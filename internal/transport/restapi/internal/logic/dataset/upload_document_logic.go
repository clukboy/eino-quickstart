// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"

	"eino-quickstart/ent"
	"eino-quickstart/ent/document"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"

	"github.com/zeromicro/go-zero/core/logx"
)

// uploadMaxMemory 是 multipart 解析时留在内存里的上限，超出的部分 net/http
// 会落到临时文件。请求体本身已经被 go-zero 的 MaxBytes 中间件按
// runtime.maxRequestBodyBytes 截断，这里只是不让一个大文件常驻内存。
const uploadMaxMemory = 8 << 20

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

const maxMemory = 32 << 20 // 32MB，控制内存中缓存的最大字节数

func (l *UploadDocumentLogic) UploadDocument(req *types.DocumentUploadReq) (resp *types.DocumentListResp, err error) {
	if err := l.r.ParseMultipartForm(maxMemory); err != nil {
		return nil, fmt.Errorf("解析上传文件失败: %w", err)

	}

	// ✅ 获取所有名为 "files" 的文件头（支持多文件）
	fileHeaders := l.r.MultipartForm.File["file"]
	if len(fileHeaders) == 0 {
		return nil, fmt.Errorf("未找到上传文件，请确认字段名为 'file'")
	}
	files := make([]*ent.DocumentCreate, 0, len(fileHeaders))

	saveDir := "./uploads"
	os.MkdirAll(saveDir, os.ModePerm)

	for _, fh := range fileHeaders {
		file, err := fh.Open()
		if err != nil {
			return nil, fmt.Errorf("打开文件 %s 失败: %w", fh.Filename, err)
		}

		savePath := filepath.Join(saveDir, fh.Filename)
		dst, err := os.Create(savePath)
		if err != nil {
			file.Close()
			return nil, fmt.Errorf("创建目标文件失败: %w", err)
		}

		_, err = io.Copy(dst, file)
		file.Close()
		dst.Close()
		if err != nil {
			return nil, fmt.Errorf("保存文件 %s 失败: %w", fh.Filename, err)
		}

		files = append(files, l.svcCtx.EntClient.Document.Create().
			SetDatasetID(req.DatasetID).
			SetVisibility(document.Visibility(req.Visibility)).
			SetTitle(fh.Filename).
			SetSource(savePath).
			SetStatus(document.StatusIndexing),
		)

		

	}
	documents, err := l.svcCtx.EntClient.Document.CreateBulk(files...).Save(l.ctx)
	if err != nil {
		return nil, documentFail(err)
	}
	list, err := documentDTOs(l.ctx, documents)
	if err != nil {
		return nil, documentFail(err)
	}
	return &types.DocumentListResp{Data: list}, nil
}
