// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/dataset"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 创建文档
func CreateDocumentHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.CreateDocumentReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := dataset.NewCreateDocumentLogic(r.Context(), svcCtx)
		resp, err := l.CreateDocument(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
