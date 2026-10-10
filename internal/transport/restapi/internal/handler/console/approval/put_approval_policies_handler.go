// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package approval

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/console/approval"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"eino-quickstart/internal/transport/restapi/internal/types"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func PutApprovalPoliciesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.PutApprovalPoliciesReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := approval.NewPutApprovalPoliciesLogic(r.Context(), svcCtx)
		resp, err := l.PutApprovalPolicies(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
