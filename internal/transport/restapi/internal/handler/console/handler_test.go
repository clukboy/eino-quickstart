package console

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	store "eino-quickstart/internal/platform/persistence/console"
	"eino-quickstart/internal/transport/restapi/internal/svc"
)

func newTestHandler() http.Handler {
	ctx := &svc.ServiceContext{Console: store.NewStore("")}
	return NewHandler(ctx)
}

func request(t *testing.T, h http.Handler, method, path string, value any) *httptest.ResponseRecorder {
	t.Helper()
	var body bytes.Buffer
	if value != nil {
		_ = json.NewEncoder(&body).Encode(value)
	}
	req := httptest.NewRequest(method, path, &body)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var value map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &value); err != nil {
		t.Fatalf("decode response: %v; body=%s", err, rec.Body.String())
	}
	return value
}

func TestConsoleHandlerPersistsCRUDAndRedactsSecrets(t *testing.T) {
	h := newTestHandler()
	create := request(t, h, http.MethodPost, "/api/v1/tools", map[string]any{"key": "search", "name": "Search", "secret": "do-not-return"})
	if create.Code != http.StatusCreated {
		t.Fatalf("create status=%d body=%s", create.Code, create.Body.String())
	}
	if bytes.Contains(create.Body.Bytes(), []byte("do-not-return")) {
		t.Fatal("secret leaked from create response")
	}

	list := request(t, h, http.MethodGet, "/api/v1/tools", nil)
	if list.Code != http.StatusOK || bytes.Contains(list.Body.Bytes(), []byte("do-not-return")) {
		t.Fatalf("list status=%d body=%s", list.Code, list.Body.String())
	}
	if !bytes.Contains(list.Body.Bytes(), []byte(`"has_secret":true`)) {
		t.Fatalf("has_secret missing: %s", list.Body.String())
	}

	update := request(t, h, http.MethodPut, "/api/v1/tools/search", map[string]any{"name": "Updated"})
	if update.Code != http.StatusOK {
		t.Fatalf("update status=%d body=%s", update.Code, update.Body.String())
	}
	if bytes.Contains(update.Body.Bytes(), []byte("do-not-return")) {
		t.Fatal("secret leaked from update response")
	}
}

func TestConsoleHandlerSupportsWholeTablePolicyReplacement(t *testing.T) {
	h := newTestHandler()
	payload := map[string]any{"data": []any{map[string]any{"tool_key": "*", "mode": "auto", "enabled": true}}}
	rec := request(t, h, http.MethodPut, "/api/v1/approval-policies", payload)
	if rec.Code != http.StatusOK {
		t.Fatalf("put status=%d body=%s", rec.Code, rec.Body.String())
	}
	got := decode(t, request(t, h, http.MethodGet, "/api/v1/approval-policies", nil))
	data, ok := got["data"].([]any)
	if !ok || len(data) != 1 {
		t.Fatalf("unexpected policy response: %#v", got)
	}
}

func TestConsoleHandlerStoresEvalCasesAndRuns(t *testing.T) {
	h := newTestHandler()
	set := request(t, h, http.MethodPost, "/api/v1/eval-sets", map[string]any{"name": "smoke"})
	if set.Code != http.StatusCreated {
		t.Fatalf("set status=%d body=%s", set.Code, set.Body.String())
	}
	var setResp map[string]any
	_ = json.Unmarshal(set.Body.Bytes(), &setResp)
	id := int(setResp["id"].(float64))
	caseRec := request(t, h, http.MethodPost, "/api/v1/eval-sets/1/cases", map[string]any{"question": "q"})
	if caseRec.Code != http.StatusCreated {
		t.Fatalf("case status=%d body=%s", caseRec.Code, caseRec.Body.String())
	}
	if id != 1 {
		t.Fatalf("unexpected id=%d", id)
	}
	run := request(t, h, http.MethodPost, "/api/v1/eval-sets/1/runs", nil)
	if run.Code != http.StatusAccepted {
		t.Fatalf("run status=%d body=%s", run.Code, run.Body.String())
	}
	var runResp map[string]any
	_ = json.Unmarshal(run.Body.Bytes(), &runResp)
	runID := runResp["run_id"].(string)
	detail := request(t, h, http.MethodGet, "/api/v1/eval-runs/"+runID, nil)
	if detail.Code != http.StatusOK || !bytes.Contains(detail.Body.Bytes(), []byte(`"results"`)) {
		t.Fatalf("detail status=%d body=%s", detail.Code, detail.Body.String())
	}
}
