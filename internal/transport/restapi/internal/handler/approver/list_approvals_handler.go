// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package approver

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/approver"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ListApprovalsHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.ApprovalListReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := approver.NewListApprovalsLogic(r.Context(), svcCtx)
		resp, err := l.ListApprovals(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
