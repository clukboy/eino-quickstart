package console

import (
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	store "eino-quickstart/internal/platform/persistence/console"
	"eino-quickstart/internal/transport/restapi/internal/svc"
)

type Handler struct{ store *store.Store }

func NewHandler(svcCtx *svc.ServiceContext) http.HandlerFunc {
	return (&Handler{store: svcCtx.Console}).ServeHTTP
}

func (h *Handler) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	path := strings.Trim(strings.TrimPrefix(r.URL.Path, "/api/v1/"), "/")
	parts := strings.Split(path, "/")
	for i, part := range parts {
		if decoded, decodeErr := url.PathUnescape(part); decodeErr == nil {
			parts[i] = decoded
		}
	}
	var payload any
	if r.Body != nil {
		if raw, readErr := io.ReadAll(r.Body); readErr == nil && len(raw) > 0 {
			_ = json.Unmarshal(raw, &payload)
		}
	}
	body, _ := payload.(map[string]any)
	if body == nil {
		body = map[string]any{}
	}
	var result any
	var status = http.StatusOK
	var err error
	switch {
	case path == "overview" && r.Method == http.MethodGet:
		result = overview()
	case path == "usage" && r.Method == http.MethodGet:
		result, status, err = h.usage(r.URL.Query())
	case path == "model-routing" && r.Method == http.MethodGet:
		result = h.single("model-routing", map[string]any{"default_chat_model": "", "default_embedding_model": "", "fallback_chain": []any{}, "updated_at": store.Now()})
	case path == "model-routing" && r.Method == http.MethodPut:
		body["updated_at"] = store.Now()
		err = h.store.Write("model-routing", body)
		result = body
	case path == "approval-policies" && r.Method == http.MethodGet:
		result = map[string]any{"data": h.rows("approval-policies")}
	case path == "approval-policies" && r.Method == http.MethodPut:
		rows := payloadRows(payload)
		err = h.store.Write("approval-policies", rows)
		result = map[string]any{"data": rows}
	case path == "password-policy" && r.Method == http.MethodGet:
		result = h.single("password-policy", defaultPasswordPolicy())
	case path == "password-policy" && r.Method == http.MethodPut:
		body["updated_at"] = store.Now()
		err = h.store.Write("password-policy", body)
		result = body
	case strings.HasPrefix(path, "eval-runs/") && r.Method == http.MethodGet:
		result, status, err = h.findOrEmpty("eval-runs", parts[1])
	case strings.HasPrefix(path, "agent-defs/"):
		result, status, err = h.agentDefs(parts, r.Method, body)
	case strings.HasPrefix(path, "tools/"):
		result, status, err = h.tools(parts, r.Method, body)
	case strings.HasPrefix(path, "model-providers/"):
		result, status, err = h.providers(parts, r.Method, body)
	case strings.HasPrefix(path, "models/"):
		result, status, err = h.models(parts, r.Method, body)
	case strings.HasPrefix(path, "org-units/"):
		result, status, err = h.orgUnits(parts, r.Method, body)
	case strings.HasPrefix(path, "eval-sets/"):
		result, status, err = h.evalSets(parts, r.Method, body, r.URL.Query())
	case strings.HasPrefix(path, "feedback/"):
		result, status, err = h.feedback(parts, r.Method, body)
	case strings.HasPrefix(path, "runs") || path == "audit-logs":
		result, status, err = h.readOnlyCollection(parts, r)
	default:
		result, status, err = h.collectionQuery(parts, r.Method, body, r.URL.Query())
	}
	if err != nil {
		writeConsoleError(w, statusOr(status, http.StatusBadRequest), err)
		return
	}
	if result == nil {
		status = http.StatusNoContent
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if result != nil {
		_ = json.NewEncoder(w).Encode(result)
	}
}
func statusOr(v, fallback int) int {
	if v == 0 {
		return fallback
	}
	return v
}

func writeConsoleError(w http.ResponseWriter, status int, err error) {
	code := "bad_request"
	switch status {
	case http.StatusNotFound:
		code = "not_found"
	case http.StatusConflict:
		code = "conflict"
	case http.StatusForbidden:
		code = "forbidden"
	case http.StatusUnauthorized:
		code = "unauthorized"
	case http.StatusMethodNotAllowed:
		code = "method_not_allowed"
	case http.StatusServiceUnavailable:
		code = "service_unavailable"
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": code, "error": err.Error()})
}
func (h *Handler) usage(query url.Values) (any, int, error) {
	interval := query.Get("interval")
	if interval != "" && interval != "day" && interval != "hour" {
		return nil, http.StatusBadRequest, errors.New("interval must be day or hour")
	}
	groupBy := query.Get("group_by")
	if groupBy != "" && groupBy != "subject" && groupBy != "agent" && groupBy != "model" && groupBy != "dataset" {
		return nil, http.StatusBadRequest, errors.New("group_by is invalid")
	}
	if from, to := query.Get("from_ms"), query.Get("to_ms"); from != "" && to != "" {
		fromMS, fromErr := strconv.ParseInt(from, 10, 64)
		toMS, toErr := strconv.ParseInt(to, 10, 64)
		if fromErr != nil || toErr != nil || fromMS >= toMS {
			return nil, http.StatusBadRequest, errors.New("from_ms must be less than to_ms")
		}
	}
	return map[string]any{"totals": map[string]any{"runs": 0, "tokens_in": 0, "tokens_out": 0, "cost_micros": 0}, "trend": []any{}, "rows": []any{}}, http.StatusOK, nil
}

func overview() map[string]any {
	return map[string]any{"kpi": map[string]any{"sessions": 0, "messages": 0, "runs": 0, "tokens_in": 0, "tokens_out": 0, "cost_micros": 0, "pending_approvals": 0, "failed_runs": 0}, "trend": []any{}, "top_agents": []any{}, "top_datasets": []any{}, "recent_fails": []any{}}
}
func defaultPasswordPolicy() map[string]any {
	return map[string]any{"min_length": 8, "require_upper": false, "require_lower": false, "require_digit": false, "require_symbol": false, "max_age_days": 0, "history_count": 0, "max_failed_attempts": 0, "lockout_seconds": 0, "session_ttl_hours": 24, "updated_at": store.Now()}
}
func (h *Handler) single(key string, fallback map[string]any) any {
	if v := h.store.Read(key); v != nil {
		return v
	}
	_ = h.store.Write(key, fallback)
	return fallback
}
func (h *Handler) rows(key string) []map[string]any { return storeRows(h.store.Read(key)) }
func storeRows(v any) []map[string]any {
	var out []map[string]any
	switch a := v.(type) {
	case []map[string]any:
		out = append(out, a...)
	case []any:
		for _, x := range a {
			if m, ok := x.(map[string]any); ok {
				out = append(out, m)
			}
		}
	}
	return out
}

func payloadRows(payload any) []map[string]any {
	if obj, ok := payload.(map[string]any); ok {
		return storeRows(obj["data"])
	}
	return storeRows(payload)
}
func (h *Handler) findOrEmpty(key, id string) (any, int, error) {
	if key == "eval-runs" {
		if value := h.store.Read("eval-runs/" + id); value != nil {
			return value, http.StatusOK, nil
		}
	}
	for _, row := range h.rows(key) {
		if store.ID(row["id"]) == id || store.ID(row["run_id"]) == id {
			return row, http.StatusOK, nil
		}
	}
	return nil, http.StatusNotFound, store.ErrNotFound
}

func (h *Handler) collection(parts []string, method string, body map[string]any) (any, int, error) {
	return h.collectionQuery(parts, method, body, nil)
}

func (h *Handler) collectionQuery(parts []string, method string, body map[string]any, query url.Values) (any, int, error) {
	key := parts[0]
	if key == "" {
		return nil, 404, store.ErrNotFound
	}
	if len(parts) > 1 {
		return nil, 404, store.ErrNotFound
	}
	if method == http.MethodGet {
		return h.pageQuery(key, query), 200, nil
	}
	if method == http.MethodPost {
		body = normalize(key, body)
		_, err := h.store.Mutate(key, func(rows []map[string]any) ([]map[string]any, error) {
			body["id"] = store.NextID(rows)
			body["created_at"] = store.Now()
			body["updated_at"] = store.Now()
			return append(rows, body), nil
		})
		return redactRow(key, body), 201, err
	}
	return nil, 405, nil
}

func (h *Handler) page(key string) map[string]any {
	return h.pageQuery(key, nil)
}

func (h *Handler) pageQuery(key string, query url.Values) map[string]any {
	rows := h.rows(key)
	store.SortNewest(rows)
	p := queryInt(query, "page", 1)
	size := queryInt(query, "page_size", 100)
	if p < 1 {
		p = 1
	}
	if size < 1 {
		size = 100
	}
	if size > 500 {
		size = 500
	}
	if keyword := strings.TrimSpace(query.Get("keyword")); keyword != "" {
		filtered := rows[:0]
		needle := strings.ToLower(keyword)
		for _, row := range rows {
			if strings.Contains(strings.ToLower(store.String(row["name"])), needle) || strings.Contains(strings.ToLower(store.String(row["description"])), needle) {
				filtered = append(filtered, row)
			}
		}
		rows = filtered
	}
	start := (p - 1) * size
	if start > len(rows) {
		start = len(rows)
	}
	end := start + size
	if end > len(rows) {
		end = len(rows)
	}
	data := make([]map[string]any, 0, end-start)
	for _, row := range rows[start:end] {
		data = append(data, redactRow(key, row))
	}
	return map[string]any{"data": data, "total": len(rows), "page": p, "page_size": size}
}

func queryInt(query url.Values, key string, fallback int) int {
	if query == nil {
		return fallback
	}
	value, err := strconv.Atoi(query.Get(key))
	if err != nil || value <= 0 {
		return fallback
	}
	return value
}
func normalize(key string, b map[string]any) map[string]any {
	if b == nil {
		b = map[string]any{}
	}
	switch key {
	case "tools":
		if b["enabled"] == nil {
			b["enabled"] = false
		}
		if b["has_secret"] == nil {
			b["has_secret"] = store.String(b["secret"]) != ""
		}
		if b["timeout_ms"] == nil {
			b["timeout_ms"] = 30000
		}
	case "agent-defs":
		if b["status"] == nil {
			b["status"] = "DRAFT"
		}
		if b["current_version"] == nil {
			b["current_version"] = 0
		}
		if b["published_version"] == nil {
			b["published_version"] = 0
		}
	case "models":
		if b["enabled"] == nil {
			b["enabled"] = true
		}
	case "model-providers":
		if b["enabled"] == nil {
			b["enabled"] = true
		}
	case "eval-sets":
		if b["cases"] == nil {
			b["cases"] = []any{}
		}
	}
	return b
}

func (h *Handler) mutateByID(key, id, method string, body map[string]any, allowDelete bool) (any, int, error) {
	var found map[string]any
	var nextRows []map[string]any
	nextRows, err := h.store.Mutate(key, func(rows []map[string]any) ([]map[string]any, error) {
		idx := -1
		for i, row := range rows {
			if store.ID(row["id"]) == id || store.ID(row["key"]) == id {
				idx = i
				found = row
				break
			}
		}
		if idx < 0 {
			return rows, store.ErrNotFound
		}
		switch method {
		case http.MethodGet:
			return rows, nil
		case http.MethodDelete:
			if !allowDelete {
				return rows, store.ErrNotFound
			}
			rows = append(rows[:idx], rows[idx+1:]...)
		case http.MethodPut, http.MethodPatch:
			for k, v := range body {
				if k != "secret" || store.String(v) != "" {
					found[k] = v
				}
			}
			found["updated_at"] = store.Now()
		}
		return rows, nil
	})
	if err != nil {
		return nil, 404, err
	}
	if method == http.MethodDelete {
		return map[string]any{"status": "deleted"}, 200, nil
	}
	if found == nil {
		for _, row := range nextRows {
			if store.ID(row["id"]) == id || store.ID(row["key"]) == id {
				found = row
				break
			}
		}
	}
	return redactRow(key, found), 200, nil
}

func (h *Handler) agentDefs(parts []string, method string, body map[string]any) (any, int, error) {
	if len(parts) == 1 {
		return h.collection(parts, method, body)
	}
	id := parts[1]
	if len(parts) == 2 {
		if method == http.MethodDelete {
			_, err := h.store.Mutate("agent-defs", func(rows []map[string]any) ([]map[string]any, error) {
				for _, row := range rows {
					if store.ID(row["id"]) == id {
						row["status"] = "ARCHIVED"
						row["updated_at"] = store.Now()
						return rows, nil
					}
				}
				return rows, store.ErrNotFound
			})
			if err != nil {
				return nil, 404, err
			}
			return map[string]any{"status": "archived"}, 200, nil
		}
		return h.mutateByID("agent-defs", id, method, body, false)
	}
	if len(parts) >= 3 && parts[2] == "versions" {
		if len(parts) == 3 && method == http.MethodGet {
			return map[string]any{"data": h.versions(id)}, 200, nil
		}
		if len(parts) == 3 && method == http.MethodPost {
			body["version"] = len(h.versions(id)) + 1
			body["status"] = "DRAFT"
			body["created_at"] = store.Now()
			body["created_by"] = "system"
			_, _ = h.store.Mutate("agent-defs", func(rows []map[string]any) ([]map[string]any, error) {
				for _, r := range rows {
					if store.ID(r["id"]) == id {
						vs, _ := r["versions"].([]any)
						r["versions"] = append(vs, body)
						r["current_version"] = body["version"]
					}
				}
				return rows, nil
			})
			return body, 201, nil
		}
		if len(parts) >= 5 {
			version := parts[3]
			action := parts[4]
			_, _ = h.store.Mutate("agent-defs", func(rows []map[string]any) ([]map[string]any, error) {
				for _, r := range rows {
					if store.ID(r["id"]) != id {
						continue
					}
					if vs, ok := r["versions"].([]any); ok {
						for _, v := range vs {
							if m, ok := v.(map[string]any); ok && store.ID(m["version"]) == version {
								if action == "publish" {
									m["status"] = "PUBLISHED"
									r["published_version"] = m["version"]
								} else if action == "retire" {
									m["status"] = "RETIRED"
									r["published_version"] = 0
								}
							}
						}
					}
				}
				return rows, nil
			})
			return map[string]any{"version": version, "status": strings.ToUpper(action + "ED")}, 200, nil
		}
	}
	if len(parts) == 3 && parts[2] == "datasets" && method == http.MethodPut {
		_, _ = h.store.Mutate("agent-defs", func(rows []map[string]any) ([]map[string]any, error) {
			for _, r := range rows {
				if store.ID(r["id"]) == id {
					r["dataset_ids"] = body["dataset_ids"]
				}
			}
			return rows, nil
		})
		return map[string]any{"status": "updated"}, 200, nil
	}
	return nil, 404, store.ErrNotFound
}
func (h *Handler) versions(id string) []any {
	for _, r := range h.rows("agent-defs") {
		if store.ID(r["id"]) == id {
			if vs, ok := r["versions"].([]any); ok {
				return vs
			}
		}
	}
	return []any{}
}

func (h *Handler) tools(parts []string, method string, body map[string]any) (any, int, error) {
	if len(parts) == 1 {
		return h.collection(parts, method, body)
	}
	key := parts[1]
	if method == http.MethodDelete {
		for _, row := range h.rows("tools") {
			if store.ID(row["key"]) == key && strings.EqualFold(store.String(row["type"]), "builtin") {
				return nil, 409, errors.New("内置工具不能删除")
			}
		}
	}
	if len(parts) == 3 && parts[2] == "enabled" && method == http.MethodPatch {
		return h.mutateByID("tools", key, http.MethodPatch, body, true)
	}
	if len(parts) == 3 && parts[2] == "test" && method == http.MethodPost {
		return map[string]any{"ok": true, "latency_ms": 0}, 200, nil
	}
	return h.mutateByID("tools", key, method, body, true)
}
func (h *Handler) providers(parts []string, method string, body map[string]any) (any, int, error) {
	if len(parts) == 1 {
		return h.collection(parts, method, body)
	}
	if method == http.MethodDelete {
		key := parts[1]
		for _, model := range h.rows("models") {
			if store.ID(model["provider_key"]) == key {
				return nil, 409, errors.New("供应商仍被模型引用")
			}
		}
	}
	return h.upsertByKey("model-providers", parts[1], method, body)
}
func (h *Handler) models(parts []string, method string, body map[string]any) (any, int, error) {
	if len(parts) == 1 {
		return h.collection(parts, method, body)
	}
	if method == http.MethodDelete {
		key := parts[1]
		routing := h.single("model-routing", map[string]any{})
		if routingMap, ok := routing.(map[string]any); ok {
			if store.ID(routingMap["default_chat_model"]) == key || store.ID(routingMap["default_embedding_model"]) == key {
				return nil, 409, errors.New("模型仍被路由配置引用")
			}
			for _, item := range storeRows(routingMap["fallback_chain"]) {
				if store.ID(item["key"]) == key || store.ID(item["model"]) == key {
					return nil, 409, errors.New("模型仍被路由配置引用")
				}
			}
		}
	}
	return h.upsertByKey("models", parts[1], method, body)
}

func (h *Handler) upsertByKey(key, id, method string, body map[string]any) (any, int, error) {
	for _, row := range h.rows(key) {
		if store.ID(row["key"]) == id || store.ID(row["id"]) == id {
			return h.mutateByID(key, id, method, body, true)
		}
	}
	if method != http.MethodPut {
		return nil, 404, store.ErrNotFound
	}
	body = normalize(key, body)
	body["key"] = id
	_, err := h.store.Mutate(key, func(rows []map[string]any) ([]map[string]any, error) {
		body["id"] = store.NextID(rows)
		body["created_at"] = store.Now()
		body["updated_at"] = store.Now()
		return append(rows, body), nil
	})
	return redactRow(key, body), 201, err
}

func (h *Handler) orgUnits(parts []string, method string, body map[string]any) (any, int, error) {
	if len(parts) == 1 {
		return h.collection(parts, method, body)
	}
	id := parts[1]
	if len(parts) == 3 && parts[2] == "members" {
		if method == http.MethodGet {
			for _, row := range h.rows("org-units") {
				if store.ID(row["id"]) == id {
					members := storeRows(row["members"])
					return map[string]any{"data": members, "total": len(members)}, 200, nil
				}
			}
			return nil, 404, store.ErrNotFound
		}
		if method == http.MethodPost {
			subject := store.String(body["subject"])
			if subject == "" {
				return nil, 400, errors.New("subject is required")
			}
			_, err := h.store.Mutate("org-units", func(rows []map[string]any) ([]map[string]any, error) {
				for _, row := range rows {
					if store.ID(row["id"]) != id {
						continue
					}
					members := storeRows(row["members"])
					for _, member := range members {
						if store.String(member["subject"]) == subject {
							return rows, errors.New("member already exists")
						}
					}
					members = append(members, map[string]any{"subject": subject, "username": subject, "nickname": subject, "role": "agent", "joined_at": store.Now()})
					row["members"] = members
					row["member_count"] = len(members)
					return rows, nil
				}
				return rows, store.ErrNotFound
			})
			if err != nil {
				return nil, 409, err
			}
			return map[string]any{"status": "updated"}, 200, nil
		}
	}
	if len(parts) == 4 && parts[2] == "members" && method == http.MethodDelete {
		subject := parts[3]
		_, err := h.store.Mutate("org-units", func(rows []map[string]any) ([]map[string]any, error) {
			for _, row := range rows {
				if store.ID(row["id"]) != id {
					continue
				}
				members := storeRows(row["members"])
				filtered := members[:0]
				for _, member := range members {
					if store.ID(member["subject"]) != subject {
						filtered = append(filtered, member)
					}
				}
				row["members"] = filtered
				row["member_count"] = len(filtered)
				return rows, nil
			}
			return rows, store.ErrNotFound
		})
		if err != nil {
			return nil, 404, err
		}
		return map[string]any{"status": "updated"}, 200, nil
	}
	if method == http.MethodDelete {
		for _, row := range h.rows("org-units") {
			if store.ID(row["id"]) == id && len(storeRows(row["members"])) > 0 {
				return nil, 409, errors.New("组织单元仍有成员")
			}
			if store.ID(row["parent_id"]) == id {
				return nil, 409, errors.New("组织单元仍有下级部门")
			}
		}
	}
	return h.mutateByID("org-units", id, method, body, true)
}
func (h *Handler) evalSets(parts []string, method string, body map[string]any, query url.Values) (any, int, error) {
	if len(parts) == 1 {
		return h.collectionQuery(parts, method, body, query)
	}
	id := parts[1]
	if len(parts) == 3 && parts[2] == "cases" {
		if method == http.MethodGet {
			for _, row := range h.rows("eval-sets") {
				if store.ID(row["id"]) == id {
					cases := storeRows(row["cases"])
					return map[string]any{"data": cases, "total": len(cases), "page": 1, "page_size": len(cases)}, 200, nil
				}
			}
			return nil, 404, store.ErrNotFound
		}
		if method == http.MethodPost {
			var created map[string]any
			_, err := h.store.Mutate("eval-sets", func(rows []map[string]any) ([]map[string]any, error) {
				for _, row := range rows {
					if store.ID(row["id"]) != id {
						continue
					}
					cases := storeRows(row["cases"])
					body["id"] = store.NextID(cases)
					body["created_at"] = store.Now()
					cases = append(cases, body)
					row["cases"] = cases
					row["case_count"] = len(cases)
					created = body
					return rows, nil
				}
				return rows, store.ErrNotFound
			})
			if err != nil {
				return nil, 404, err
			}
			return created, 201, nil
		}
	}
	if len(parts) == 3 && parts[2] == "runs" {
		if method == http.MethodGet {
			for _, row := range h.rows("eval-sets") {
				if store.ID(row["id"]) == id {
					runs := storeRows(row["runs"])
					return map[string]any{"data": runs}, 200, nil
				}
			}
			return nil, 404, store.ErrNotFound
		}
		if method == http.MethodPost {
			runID := strconv.FormatInt(store.Now(), 10)
			run := map[string]any{"run_id": runID, "status": "RUNNING", "total": 0, "completed": 0, "passed": 0, "failed": 0, "started_at": store.Now(), "finished_at": 0, "error": ""}
			_, err := h.store.Mutate("eval-sets", func(rows []map[string]any) ([]map[string]any, error) {
				for _, row := range rows {
					if store.ID(row["id"]) == id {
						runs := storeRows(row["runs"])
						row["runs"] = append(runs, run)
						return rows, nil
					}
				}
				return rows, store.ErrNotFound
			})
			if err != nil {
				return nil, 404, err
			}
			_ = h.store.Write("eval-runs/"+runID, map[string]any{"run": run, "results": []any{}})
			return map[string]any{"run_id": runID}, 202, nil
		}
	}
	if len(parts) == 4 && parts[2] == "cases" && method == http.MethodDelete {
		caseID := parts[3]
		_, err := h.store.Mutate("eval-sets", func(rows []map[string]any) ([]map[string]any, error) {
			for _, row := range rows {
				if store.ID(row["id"]) != id {
					continue
				}
				cases := storeRows(row["cases"])
				filtered := cases[:0]
				for _, c := range cases {
					if store.ID(c["id"]) != caseID {
						filtered = append(filtered, c)
					}
				}
				row["cases"] = filtered
				row["case_count"] = len(filtered)
				return rows, nil
			}
			return rows, store.ErrNotFound
		})
		if err != nil {
			return nil, 404, err
		}
		return map[string]any{"status": "deleted"}, 200, nil
	}
	return h.mutateByID("eval-sets", id, method, body, true)
}
func (h *Handler) feedback(parts []string, method string, body map[string]any) (any, int, error) {
	if len(parts) == 1 {
		return h.collection(parts, method, body)
	}
	return h.mutateByID("feedback", parts[1], method, body, false)
}
func redactRow(key string, row map[string]any) map[string]any {
	if row == nil {
		return nil
	}
	if key != "tools" && key != "model-providers" && key != "models" {
		return row
	}
	out := make(map[string]any, len(row))
	hasSecret := false
	for k, v := range row {
		switch strings.ToLower(k) {
		case "secret", "api_key", "apikey", "access_token", "client_secret":
			if store.String(v) != "" {
				hasSecret = true
			}
			continue
		default:
			out[k] = v
		}
	}
	if hasSecret || store.Bool(row["has_secret"], false) {
		out["has_secret"] = true
	}
	return out
}

func (h *Handler) readOnlyCollection(parts []string, r *http.Request) (any, int, error) {
	key := parts[0]
	if key == "runs" && len(parts) > 1 {
		return h.findOrEmpty("runs", parts[1])
	}
	if r.Method != http.MethodGet {
		return nil, 405, nil
	}
	return h.page(key), 200, nil
}
