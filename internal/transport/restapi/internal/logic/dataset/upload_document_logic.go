// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"path/filepath"
	"strings"

	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/transport/restapi/internal/httpx"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// uploadMaxMemory 是 multipart 解析时留在内存里的上限，超出的部分 net/http
// 会落到临时文件。它不是单文件大小上限；单文件大小由
// knowledge.maxDocumentBytes 约束，读取时还会再做一次受限读取。
const uploadMaxMemory = 8 << 20

const (
	uploadFieldName               = "file"
	defaultUploadMaxDocumentBytes = int64(5 << 20)
	defaultUploadMaxRequestBytes  = int64(50 << 20)
	defaultUploadMaxFiles         = 10
	uploadStatusSucceeded         = "succeeded"
	uploadStatusFailed            = "failed"
)

// uploadFileError is safe to expose at the per-file level. The message never
// contains an OS path, SQL detail, or an internal error string.
type uploadFileError struct {
	code    string
	message string
}

func (e *uploadFileError) Error() string { return e.message }

type countingReadCloser struct {
	reader io.Reader
	closer io.Closer
	read   int64
}

func (r *countingReadCloser) Read(p []byte) (int, error) {
	n, err := r.reader.Read(p)
	r.read += int64(n)
	return n, err
}

func (r *countingReadCloser) Close() error { return r.closer.Close() }

type UploadDocumentLogic struct {
	logx.Logger
	ctx    context.Context
	svcCtx *svc.ServiceContext
	r      *http.Request
}

// 上传文档
func NewUploadDocumentLogic(r *http.Request, svcCtx *svc.ServiceContext) *UploadDocumentLogic {
	ctx := r.Context()
	return &UploadDocumentLogic{
		Logger: logx.WithContext(ctx),
		ctx:    ctx,
		svcCtx: svcCtx,
		r:      r,
	}
}

// UploadDocument 接收多个 file 字段，逐个落成文档。
//
// 请求级问题（multipart 损坏、没有文件、超过文件数或请求总大小）直接拒绝；
// 文件级问题不会回滚同一批的其它文件，而是写入 files[].error。这样调用方可以
// 只重传失败文件，不必把已经成功的整批再发一遍。data 仍保留扁平成功列表，兼容
// 原来的上传响应。
func (l *UploadDocumentLogic) UploadDocument(req *types.DocumentUploadReq) (*types.DocumentUploadResp, error) {
	subject, err := actorSubject(l.ctx)
	if err != nil {
		return nil, err
	}

	// Release any multipart spill files created by net/http on either path.
	defer func() {
		if l.r.MultipartForm != nil {
			_ = l.r.MultipartForm.RemoveAll()
		}
	}()

	maxRequestBytes := l.uploadMaxRequestBytes()
	if l.r.ContentLength > maxRequestBytes {
		return nil, httpx.TooLarge(fmt.Sprintf("上传请求超过 %d 字节的总上限", maxRequestBytes))
	}

	// The production server also has go-zero's MaxBytes middleware. This local
	// limit keeps direct handler tests and alternate transports on the same
	// contract, including requests that use chunked transfer encoding.
	counter := &countingReadCloser{
		reader: io.LimitReader(l.r.Body, maxRequestBytes+1),
		closer: l.r.Body,
	}
	l.r.Body = counter
	if err := l.r.ParseMultipartForm(uploadMaxMemory); err != nil {
		if counter.read > maxRequestBytes {
			return nil, httpx.TooLarge(fmt.Sprintf("上传请求超过 %d 字节的总上限", maxRequestBytes))
		}
		return nil, httpx.BadRequest("上传请求不是有效的 multipart/form-data")
	}
	if counter.read > maxRequestBytes {
		return nil, httpx.TooLarge(fmt.Sprintf("上传请求超过 %d 字节的总上限", maxRequestBytes))
	}

	fileHeaders := l.r.MultipartForm.File[uploadFieldName]
	if len(fileHeaders) == 0 {
		return nil, documentFail(&knowledge.ValidationError{
			Message: fmt.Sprintf("未找到上传文件，请确认字段名为 %q", uploadFieldName),
		})
	}
	maxFiles := l.uploadMaxFiles()
	if len(fileHeaders) > maxFiles {
		return nil, httpx.BadRequest(fmt.Sprintf("一次最多上传 %d 个文件", maxFiles))
	}

	resp := &types.DocumentUploadResp{
		Data:  make([]*types.DocumentResp, 0),
		Files: make([]*types.DocumentUploadFileResp, 0, len(fileHeaders)),
	}
	for _, header := range fileHeaders {
		item := &types.DocumentUploadFileResp{
			Filename: header.Filename,
			Status:   uploadStatusFailed,
		}

		content, readErr := readUploadedFileWithLimit(header, l.uploadMaxDocumentBytes())
		if readErr != nil {
			item.Error = uploadFileErrorDTO(readErr)
			resp.Files = append(resp.Files, item)
			resp.PartialFailure = true
			continue
		}

		created, createErr := l.svcCtx.Knowledge.Create(l.ctx, knowledge.CreateInput{
			DatasetID:    req.DatasetID,
			Title:        header.Filename,
			Content:      content,
			Visibility:   req.Visibility,
			OwnerSubject: subject,
		})
		if createErr != nil {
			item.Error = uploadFileErrorDTO(documentFail(createErr))
			resp.Files = append(resp.Files, item)
			resp.PartialFailure = true
			continue
		}

		documents, dtoErr := createResultDTOs(l.ctx, l.svcCtx.Knowledge, created)
		if dtoErr != nil {
			item.Error = uploadFileErrorDTO(documentFail(dtoErr))
			resp.Files = append(resp.Files, item)
			resp.PartialFailure = true
			continue
		}
		item.Status = uploadStatusSucceeded
		item.Documents = documents
		resp.Data = append(resp.Data, documents...)
		resp.Files = append(resp.Files, item)
	}

	return resp, nil
}

func (l *UploadDocumentLogic) uploadMaxRequestBytes() int64 {
	if l.svcCtx != nil && l.svcCtx.UploadMaxRequestBodyBytes > 0 {
		return l.svcCtx.UploadMaxRequestBodyBytes
	}
	return defaultUploadMaxRequestBytes
}

func (l *UploadDocumentLogic) uploadMaxDocumentBytes() int64 {
	if l.svcCtx != nil && l.svcCtx.UploadMaxDocumentBytes > 0 {
		return l.svcCtx.UploadMaxDocumentBytes
	}
	return defaultUploadMaxDocumentBytes
}

func (l *UploadDocumentLogic) uploadMaxFiles() int {
	if l.svcCtx != nil && l.svcCtx.UploadMaxFiles > 0 {
		return l.svcCtx.UploadMaxFiles
	}
	return defaultUploadMaxFiles
}

func readUploadedFile(header *multipart.FileHeader) (string, error) {
	return readUploadedFileWithLimit(header, defaultUploadMaxDocumentBytes)
}

func readUploadedFileWithLimit(header *multipart.FileHeader, maxBytes int64) (string, error) {
	if header == nil {
		return "", &uploadFileError{code: httpx.CodeBadRequest, message: "上传文件无效"}
	}
	filename := strings.TrimSpace(header.Filename)
	if filename == "" {
		return "", &uploadFileError{code: "empty_filename", message: "文件名不能为空"}
	}
	if !supportedUploadFilename(filename) {
		return "", &uploadFileError{
			code:    httpx.CodeUnsupportedFormat,
			message: "仅支持 Markdown 或纯文本文件（.md、.markdown、.txt、.text）",
		}
	}

	file, err := header.Open()
	if err != nil {
		return "", fmt.Errorf("open upload file: %w", err)
	}
	defer func() { _ = file.Close() }()

	data, err := io.ReadAll(io.LimitReader(file, maxBytes+1))
	if err != nil {
		return "", fmt.Errorf("read upload file: %w", err)
	}
	if int64(len(data)) > maxBytes {
		return "", &uploadFileError{
			code:    httpx.CodeTooLarge,
			message: fmt.Sprintf("文件 %q 超过 %d 字节的单文件上限", filename, maxBytes),
		}
	}
	if len(data) == 0 {
		return "", &uploadFileError{code: "empty_file", message: fmt.Sprintf("文件 %q 不能为空", filename)}
	}
	return string(data), nil
}

func supportedUploadFilename(filename string) bool {
	switch strings.ToLower(filepath.Ext(filename)) {
	case ".md", ".markdown", ".txt", ".text":
		return true
	default:
		return false
	}
}

func uploadFileErrorDTO(err error) *types.DocumentUploadError {
	var fileErr *uploadFileError
	if errors.As(err, &fileErr) {
		return &types.DocumentUploadError{Code: fileErr.code, Message: fileErr.message}
	}
	var appErr *httpx.Error
	if errors.As(err, &appErr) {
		return &types.DocumentUploadError{Code: appErr.Code, Message: appErr.Message}
	}
	return &types.DocumentUploadError{Code: httpx.CodeInternal, Message: "文件处理失败"}
}
