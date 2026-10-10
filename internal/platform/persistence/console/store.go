package console

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Store is the small management-plane repository used by the console APIs.
// It deliberately keeps the wire-shaped JSON so the API contract can evolve
// independently from the execution-plane Ent schema.
type Store struct {
	mu   sync.RWMutex
	path string
	data map[string]any
}

func NewStore(path string) *Store {
	s := &Store{path: path, data: map[string]any{}}
	if path == "" {
		return s
	}
	if b, err := os.ReadFile(path); err == nil {
		_ = json.Unmarshal(b, &s.data)
	}
	return s
}

func (s *Store) snapshot() map[string]any {
	b, _ := json.Marshal(s.data)
	var out map[string]any
	_ = json.Unmarshal(b, &out)
	return out
}

func (s *Store) saveLocked() error {
	if s.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(s.path), 0o755); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (s *Store) Read(key string) any {
	s.mu.RLock()
	defer s.mu.RUnlock()
	return s.snapshot()[key]
}

func (s *Store) Write(key string, value any) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.data[key] = value
	return s.saveLocked()
}

func (s *Store) Mutate(key string, fn func([]map[string]any) ([]map[string]any, error)) ([]map[string]any, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rows := toRows(s.data[key])
	next, err := fn(rows)
	if err != nil {
		return nil, err
	}
	s.data[key] = next
	if err := s.saveLocked(); err != nil {
		return nil, err
	}
	return next, nil
}

func toRows(v any) []map[string]any {
	switch arr := v.(type) {
	case []map[string]any:
		return append([]map[string]any(nil), arr...)
	case []any:
		out := make([]map[string]any, 0, len(arr))
		for _, item := range arr {
			if row, ok := item.(map[string]any); ok {
				out = append(out, row)
			}
		}
		return out
	default:
		return []map[string]any{}
	}
}

func Now() int64 { return time.Now().UnixMilli() }
func String(v any) string {
	if v == nil {
		return ""
	}
	return strings.TrimSpace(toString(v))
}
func toString(v any) string {
	switch x := v.(type) {
	case string:
		return x
	case json.Number:
		return x.String()
	case float64:
		return strconv.FormatFloat(x, 'f', -1, 64)
	case float32:
		return strconv.FormatFloat(float64(x), 'f', -1, 32)
	case int:
		return strconv.Itoa(x)
	case int8:
		return strconv.FormatInt(int64(x), 10)
	case int16:
		return strconv.FormatInt(int64(x), 10)
	case int32:
		return strconv.FormatInt(int64(x), 10)
	case int64:
		return strconv.FormatInt(x, 10)
	case uint:
		return strconv.FormatUint(uint64(x), 10)
	case uint8:
		return strconv.FormatUint(uint64(x), 10)
	case uint16:
		return strconv.FormatUint(uint64(x), 10)
	case uint32:
		return strconv.FormatUint(uint64(x), 10)
	case uint64:
		return strconv.FormatUint(x, 10)
	default:
		return ""
	}
}
func Bool(v any, fallback bool) bool {
	if v == nil {
		return fallback
	}
	b, ok := v.(bool)
	if ok {
		return b
	}
	return strings.EqualFold(String(v), "true")
}
func Int(v any, fallback int) int {
	if v == nil {
		return fallback
	}
	switch x := v.(type) {
	case float64:
		return int(x)
	case json.Number:
		n, _ := strconv.Atoi(x.String())
		return n
	case string:
		n, _ := strconv.Atoi(x)
		return n
	}
	return fallback
}
func ID(v any) string { return String(v) }

func NextID(rows []map[string]any) int64 {
	var max int64
	for _, row := range rows {
		n, _ := strconv.ParseInt(String(row["id"]), 10, 64)
		if n > max {
			max = n
		}
	}
	return max + 1
}
func SortNewest(rows []map[string]any) {
	sort.SliceStable(rows, func(i, j int) bool { return Int64(rows[i]["updated_at"]) > Int64(rows[j]["updated_at"]) })
}
func Int64(v any) int64 {
	if v == nil {
		return 0
	}
	switch x := v.(type) {
	case float64:
		return int64(x)
	case json.Number:
		n, _ := strconv.ParseInt(x.String(), 10, 64)
		return n
	case string:
		n, _ := strconv.ParseInt(x, 10, 64)
		return n
	}
	return 0
}

var ErrNotFound = errors.New("console resource not found")
