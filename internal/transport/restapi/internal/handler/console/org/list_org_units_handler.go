// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package org

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/console/org"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ListOrgUnitsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := org.NewListOrgUnitsLogic(r.Context(), svcCtx)
		resp, err := l.ListOrgUnits()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
