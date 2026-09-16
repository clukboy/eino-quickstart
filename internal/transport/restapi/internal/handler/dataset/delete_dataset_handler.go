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

// 删除数据集
func DeleteDatasetHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var req types.DatasetIDReq
		if err := httpx.Parse(r, &req); err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
			return
		}

		l := dataset.NewDeleteDatasetLogic(r.Context(), svcCtx)
		resp, err := l.DeleteDataset(&req)
		if err != nil {
			httpx.ErrorCtx(r.Context(), w, err)
		} else {
			httpx.OkJsonCtx(r.Context(), w, resp)
		}
	}
}
