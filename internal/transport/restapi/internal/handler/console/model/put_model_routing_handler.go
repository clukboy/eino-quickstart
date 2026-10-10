// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package model

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/console/model"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func PutModelRoutingHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.PutModelRoutingReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := model.NewPutModelRoutingLogic(r.Context(), svcCtx)
		resp, err := l.PutModelRouting(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
