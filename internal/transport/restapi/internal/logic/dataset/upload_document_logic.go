// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"

	"eino-quickstart/ent"
	"eino-quickstart/internal/application/knowledge"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// uploadMaxMemory 是 multipart 解析时留在内存里的上限，超出的部分 net/http
// 会落到临时文件。请求体本身已经被 go-zero 的 MaxBytes 中间件按
// runtime.maxRequestBodyBytes 截断，这里只是不让一个大文件常驻内存。
const uploadMaxMemory = 8 << 20

// uploadFieldName 是 multipart 里的文件字段名。
const uploadFieldName = "file"

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

// UploadDocument 接收上传的文件，逐个落成文档。
//
// 走的和 CreateDocument 完全同一条路：正文写进 knowledge.root 的托管目录、
// 建文档行、投递索引任务。**这里不解析、也不切块** —— 一份上传文件里有几个
// 产品，要等 worker 按内容拆开之后才知道，请求内算不出来；本接口只保证
// 「文件已收下并且排队了」。
//
// 返回的每条 DocumentResp 都是 status=indexing、chunk_count=0：要轮询
// GET /dataset/:id/documents/:docId 才能看到索引进度。
func (l *UploadDocumentLogic) UploadDocument(req *types.DocumentUploadReq) (resp *types.DocumentListResp, err error) {
	subject, err := actorSubject(l.ctx)
	if err != nil {
		return nil, err
	}

	if err := l.r.ParseMultipartForm(uploadMaxMemory); err != nil {
		return nil, fmt.Errorf("解析上传文件失败: %w", err)
	}

	fileHeaders := l.r.MultipartForm.File[uploadFieldName]
	if len(fileHeaders) == 0 {
		return nil, documentFail(&knowledge.ValidationError{
			Message: fmt.Sprintf("未找到上传文件，请确认字段名为 %q", uploadFieldName),
		})
	}

	docs := make([]*ent.Document, 0, len(fileHeaders))
	for _, header := range fileHeaders {
		content, err := readUploadedFile(header)
		if err != nil {
			return nil, err
		}

		doc, err := l.svcCtx.Knowledge.Create(l.ctx, knowledge.CreateInput{
			DatasetID:    req.DatasetID,
			Title:        header.Filename,
			Content:      content,
			Visibility:   req.Visibility,
			OwnerSubject: subject,
		})
		if err != nil {
			return nil, documentFail(err)
		}
		docs = append(docs, doc)
	}

	list, err := documentDTOs(l.ctx, l.svcCtx.Knowledge, docs)
	if err != nil {
		return nil, documentFail(err)
	}
	return &types.DocumentListResp{Data: list}, nil
}

func readUploadedFile(header *multipart.FileHeader) (string, error) {
	file, err := header.Open()
	if err != nil {
		return "", fmt.Errorf("打开文件 %s 失败: %w", header.Filename, err)
	}
	defer func() {
		_ = file.Close()
	}()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", fmt.Errorf("读取文件 %s 失败: %w", header.Filename, err)
	}
	return string(data), nil
}
