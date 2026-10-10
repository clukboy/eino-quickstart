// Code scaffolded by goctl. DO NOT EDIT.

package user

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/user"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 从已配置的外部数据源同步账号（只新增/更新，不删除；支持 dry-run）
func SyncUsersHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.SyncUsersReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}
		l := user.NewSyncUsersLogic(r.Context(), svcCtx)
		resp, err := l.SyncUsers(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
