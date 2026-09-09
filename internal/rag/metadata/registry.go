package metadata

import (
	"fmt"
	"sync"

	"eino-quickstart/internal/rag/domain"
)

type Registry interface {
	Register(schema Schema) error
	Get(name string, version int) (Schema, bool)
	Validate(schemaName string, version int, values domain.Metadata) error
}

type MemoryRegistry struct {
	mu      sync.RWMutex
	schemas map[string]Schema
}

func NewMemoryRegistry() *MemoryRegistry {
	return &MemoryRegistry{
		schemas: make(map[string]Schema),
	}
}

func (r *MemoryRegistry) Register(schema Schema) error {
	if schema.Name == "" {
		return fmt.Errorf("schema name is required")
	}

	if schema.Version <= 0 {
		return fmt.Errorf("schema version must be greater than zero")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	key := schemaKey(schema.Name, schema.Version)

	r.schemas[key] = schema

	return nil
}

func (r *MemoryRegistry) Get(name string, version int) (Schema, bool) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	schema, ok := r.schemas[schemaKey(name, version)]
	return schema, ok
}

func (r *MemoryRegistry) Validate(schemaName string, version int, values domain.Metadata) error {

	schema, ok := r.Get(schemaName, version)
	if !ok {
		return fmt.Errorf(
			"metadata schema not found: %s:%d",
			schemaName,
			version,
		)
	}

	return Validate(schema, values)
}

func schemaKey(name string, version int) string {
	return fmt.Sprintf("%s:%d", name, version)
}
