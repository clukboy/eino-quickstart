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

// 删除文档
func DeleteDocumentHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DocumentIDReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := dataset.NewDeleteDocumentLogic(r.Context(), svcCtx)
		resp, err := l.DeleteDocument(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
