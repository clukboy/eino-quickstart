package registry

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/components/tool"
)

type Registry struct {
	mu    sync.RWMutex
	tools map[string]tool.BaseTool
}

func New() *Registry {
	return &Registry{tools: make(map[string]tool.BaseTool)}
}

func (r *Registry) Get(name string) (tool.BaseTool, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	item, ok := r.tools[name]
	return item, ok
}

func (r *Registry) Register(t tool.BaseTool) error {
	info, err := t.Info(context.Background())
	if err != nil {
		return err
	}
	if info == nil || info.Name == "" {
		return fmt.Errorf("tool info/name is empty")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if _, exists := r.tools[info.Name]; exists {
		return fmt.Errorf("tool already registered: %s", info.Name)
	}
	r.tools[info.Name] = t
	return nil
}

func (r *Registry) Require(names ...string) ([]tool.BaseTool, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()

	items := make([]tool.BaseTool, 0, len(names))

	for _, name := range names {
		item, ok := r.tools[name]
		if !ok {
			return nil, fmt.Errorf("required tool is not registered: %s", name)
		}
		items = append(items, item)
	}

	return items, nil
}

func (r *Registry) List() []tool.BaseTool {
	r.mu.RLock()
	defer r.mu.RUnlock()
	tools := make([]tool.BaseTool, 0, len(r.tools))
	for _, t := range r.tools {
		tools = append(tools, t)
	}
	return tools
}
