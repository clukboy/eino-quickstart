package rag

import (
	"strconv"

	"github.com/cloudwego/eino/schema"
)

func MetaString(d *schema.Document, key string) string {
	if d == nil || d.MetaData == nil {
		return ""
	}
	if v, ok := d.MetaData[key].(string); ok {
		return v
	}
	return ""
}

func MetaInt(d *schema.Document, key string) int {
	if d == nil || d.MetaData == nil {
		return 0
	}
	switch v := d.MetaData[key].(type) {
	case int:
		return v
	case string:
		n, _ := strconv.Atoi(v)
		return n
	}
	return 0
}

// EnsureMeta [优化] 集中初始化 MetaData，防止 nil map panic
func EnsureMeta(d *schema.Document) {
	if d.MetaData == nil {
		d.MetaData = make(map[string]any)
	}
}

// CopyMeta 浅拷贝元数据，chunk 继承父文档元数据时使用
func CopyMeta(src map[string]any) map[string]any {
	dst := make(map[string]any, len(src))
	for k, v := range src {
		dst[k] = v
	}
	return dst
}
