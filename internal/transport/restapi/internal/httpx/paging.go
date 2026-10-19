package httpx

// 列表接口的分页口径。
//
// 放在这里而不是继续在每条 list logic 里内联：口径一旦漂开，前端按服务端回传的
// page_size 渲染分页器就会算出不同的页数。现状已经有分叉（tools/runs/audit/feedback
// 是 200，agent-defs/models/model-providers/approvals 是 100），新接口一律用这一份，
// 老接口要收敛就顺手换过来。
const (
	// DefaultPageSize 是「没传 page_size」或传了非法值时实际返回的条数。
	DefaultPageSize = 20

	// MaxPageSize 是单页条数上限。超过不报错，压到上限：上限是服务端资源约束，
	// 不该让调用方猜，更不该让一次手滑把整库拉下来。
	MaxPageSize = 200
)

// NormalizePage 把请求里的 page / page_size 归一到本仓库口径：page 从 1 开始，
// page_size <= 0 或 > MaxPageSize 时按 DefaultPageSize 兜底。
//
// 返回值就是**要回写给客户端的**那对数：客户端拿 total 与 page_size 算页数，
// 两边必须用同一套数，否则会算出点不到头的「第 3/5 页」。
func NormalizePage(page, pageSize int) (int, int) {
	if page <= 0 {
		page = 1
	}

	if pageSize <= 0 || pageSize > MaxPageSize {
		pageSize = DefaultPageSize
	}

	return page, pageSize
}
