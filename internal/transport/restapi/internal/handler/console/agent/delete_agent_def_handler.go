// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package agent

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/console/agent"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func DeleteAgentDefHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.AgentDefIDReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := agent.NewDeleteAgentDefLogic(r.Context(), svcCtx)
		resp, err := l.DeleteAgentDef(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
