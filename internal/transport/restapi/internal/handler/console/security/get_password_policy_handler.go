// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package security

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/console/security"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

func GetPasswordPolicyHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := security.NewGetPasswordPolicyLogic(r.Context(), svcCtx)
		resp, err := l.GetPasswordPolicy()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
