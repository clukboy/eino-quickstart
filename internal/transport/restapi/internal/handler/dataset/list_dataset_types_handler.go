// Code scaffolded by goctl. Safe to edit.
// goctl 1.9.2

package dataset

import (
	"net/http"

	"eino-quickstart/internal/transport/restapi/internal/logic/dataset"
	"eino-quickstart/internal/transport/restapi/internal/svc"
	"github.com/zeromicro/go-zero/rest/httpx"
)

// 列出数据集类型
func ListDatasetTypesHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		l := dataset.NewListDatasetTypesLogic(r.Context(), svcCtx)
		resp, err := l.ListDatasetTypes()
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
