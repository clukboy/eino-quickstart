package dataset

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"strconv"
	"testing"

	"eino-quickstart/ent/enttest"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/platform/auth"
	"eino-quickstart/internal/platform/queue/tasks"
	apphttp "eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"

	"entgo.io/ent/dialect"
	_ "github.com/mattn/go-sqlite3"
	"github.com/zeromicro/go-zero/rest/pathvar"
)

type contractQueue struct {
	unavailable bool
	calls       int
}

func (q *contractQueue) EnqueueIndex(context.Context, uint64, uint64, tasks.IndexMode) error {
	if q.unavailable {
		return errors.New("controlled outage")
	}
	q.calls++
	return nil
}

func TestDocumentHTTPContract(t *testing.T) {
	// Invoke the real handlers with a supplied test identity. This suite verifies
	// JSON/path/multipart bindings and domain error mapping, not authentication.
	apphttp.Register()
	client := enttest.Open(t, dialect.SQLite, "file:http-contract?mode=memory&cache=shared&_fk=1")
	t.Cleanup(func() { _ = client.Close() })
	q := &contractQueue{}
	service, err := knowledge.NewService(knowledge.ServiceDeps{Client: client, Queue: q})
	if err != nil {
		t.Fatal(err)
	}
	ds, err := client.Dataset.Create().SetName("http-contract").SetType("document").Save(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	svcCtx := &svc.ServiceContext{EntClient: client, Knowledge: service}
	call := func(handler http.HandlerFunc, method, contentType string, body []byte, docID uint64) *httptest.ResponseRecorder {
		r := httptest.NewRequest(method, "/", bytes.NewReader(body))
		r.Header.Set("Content-Type", contentType)
		r = r.WithContext(auth.WithIdentity(r.Context(), auth.Identity{Subject: "acceptance"}))
		r = pathvar.WithVars(r, map[string]string{"id": strconv.FormatUint(ds.ID, 10), "docId": strconv.FormatUint(docID, 10)})
		w := httptest.NewRecorder()
		handler(w, r)
		return w
	}
	checkError := func(w *httptest.ResponseRecorder, status int, code string) {
		t.Helper()
		var body apphttp.ErrorResponse
		if err := json.Unmarshal(w.Body.Bytes(), &body); err != nil {
			t.Fatal(err)
		}
		if w.Code != status || body.Code != code || body.Error == "" {
			t.Fatalf("error contract: %d %s", w.Code, w.Body.String())
		}
	}
	created := call(CreateDocumentHandler(svcCtx), "POST", "application/json", []byte(`{"title":"验收","content":"验收蓝鲸词A"}`), 0)
	if created.Code != 200 {
		t.Fatalf("create: %d %s", created.Code, created.Body.String())
	}
	var list struct {
		Data []struct {
			ID         uint64 `json:"id"`
			Status     string `json:"status"`
			ChunkCount int    `json:"chunk_count"`
			Indexed    int    `json:"indexed_chunk_count"`
			Operation  string `json:"operation"`
		} `json:"data"`
	}
	if err := json.Unmarshal(created.Body.Bytes(), &list); err != nil || len(list.Data) != 1 {
		t.Fatalf("list contract: %s %v", created.Body.String(), err)
	}
	doc := list.Data[0]
	if doc.ID == 0 || doc.Status != "indexing" || doc.ChunkCount != 0 || doc.Indexed != 0 || doc.Operation != "created" {
		t.Fatalf("asynchronous contract: %+v", doc)
	}
	read := call(GetDocumentContentHandler(svcCtx), "GET", "", nil, doc.ID)
	var content map[string]any
	if err := json.Unmarshal(read.Body.Bytes(), &content); err != nil || read.Code != 200 || content["content"] != "验收蓝鲸词A" {
		t.Fatalf("body contract: %d %s %v", read.Code, read.Body.String(), err)
	}
	checkError(call(CreateDocumentHandler(svcCtx), "POST", "application/json", []byte(`{"title":"empty","content":" "}`), 0), 400, "bad_request")
	checkError(call(CreateDocumentHandler(svcCtx), "POST", "application/json", []byte(`{"title":"broken","content":12}`), 0), 400, "bad_request")
	checkError(call(GetDocumentContentHandler(svcCtx), "GET", "", nil, 99999), 404, "not_found")
	checkError(call(SearchDatasetHandler(svcCtx), "POST", "application/json", []byte(`{"query":" "}`), 0), 400, "bad_request")
	updated := call(UpdateDocumentHandler(svcCtx), "PUT", "application/json", []byte(`{"content":"验收蓝鲸词B"}`), doc.ID)
	if updated.Code != 200 {
		t.Fatalf("content-only update: %d %s", updated.Code, updated.Body.String())
	}
	read = call(GetDocumentContentHandler(svcCtx), "GET", "", nil, doc.ID)
	if err := json.Unmarshal(read.Body.Bytes(), &content); err != nil || read.Code != 200 || content["content"] != "验收蓝鲸词B" {
		t.Fatalf("updated body contract: %d %s %v", read.Code, read.Body.String(), err)
	}
	// A missing top_k is valid; this fixture deliberately has no retrieval service.
	checkError(call(SearchDatasetHandler(svcCtx), "POST", "application/json", []byte(`{"query":"验收蓝鲸词B"}`), 0), 503, "service_unavailable")
	// Actual multipart parsing, canonical file field, and multiple uploaded files.
	var upload bytes.Buffer
	writer := multipart.NewWriter(&upload)
	for n := 0; n < 2; n++ {
		file, err := writer.CreateFormFile("file", fmt.Sprintf("sample-%d.md", n))
		if err != nil {
			t.Fatal(err)
		}
		_, _ = file.Write([]byte(fmt.Sprintf("验收上传%d", n)))
	}
	_ = writer.Close()
	uploaded := call(UploadDocumentHandler(svcCtx), "POST", writer.FormDataContentType(), upload.Bytes(), 0)
	if err := json.Unmarshal(uploaded.Body.Bytes(), &list); err != nil || uploaded.Code != 200 || len(list.Data) != 2 {
		t.Fatalf("multipart contract: %d %s %v", uploaded.Code, uploaded.Body.String(), err)
	}
	q.unavailable = true
	checkError(call(CreateDocumentHandler(svcCtx), "POST", "application/json", []byte(`{"title":"queue-outage","content":"正文"}`), 0), 503, "service_unavailable")
	deleted := call(DeleteDocumentHandler(svcCtx), "DELETE", "", nil, doc.ID)
	if deleted.Code != 200 {
		t.Fatalf("delete: %d %s", deleted.Code, deleted.Body.String())
	}
	checkError(call(GetDocumentHandler(svcCtx), "GET", "", nil, doc.ID), 404, "not_found")
	checkError(call(GetDocumentContentHandler(svcCtx), "GET", "", nil, doc.ID), 404, "not_found")
	if q.calls != 4 {
		t.Fatalf("invalid requests published work: calls=%d", q.calls)
	}
}
