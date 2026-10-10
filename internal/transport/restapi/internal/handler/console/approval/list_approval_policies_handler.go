// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package approval

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/console/approval"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func ListApprovalPoliciesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := approval.NewListApprovalPoliciesLogic(r.Context(), svcCtx)
		resp, err := l.ListApprovalPolicies()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
