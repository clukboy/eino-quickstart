// Package indexversion provides the small, durable control plane used to stage
// and switch search-index versions. It deliberately does not write ES/Milvus:
// builders own those systems, while this package prevents a partial build from
// becoming the active version.
package indexversion

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
)

type Status string

const (
	StatusBuilding Status = "building"
	StatusReady    Status = "ready"
	StatusFailed   Status = "failed"
)

type Version struct {
	ID          string    `json:"id"`
	Embedding   string    `json:"embedding"`
	Dimensions  int       `json:"dimensions"`
	Mapping     string    `json:"mapping"`
	Expected    int       `json:"expected"`
	Indexed     int       `json:"indexed"`
	Status      Status    `json:"status"`
	Error       string    `json:"error,omitempty"`
	CreatedAt   time.Time `json:"created_at"`
	CompletedAt time.Time `json:"completed_at,omitempty"`
}

type state struct {
	Active   string             `json:"active"`
	Versions map[string]Version `json:"versions"`
}

type Manager struct {
	path string
	mu   sync.Mutex
}

func New(path string) (*Manager, error) {
	if strings.TrimSpace(path) == "" {
		return nil, errors.New("indexversion: state path is required")
	}
	return &Manager{path: filepath.Clean(path)}, nil
}

func (m *Manager) State() (string, []Version, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.load()
	if err != nil {
		return "", nil, err
	}
	versions := make([]Version, 0, len(s.Versions))
	for _, version := range s.Versions {
		versions = append(versions, version)
	}
	sort.Slice(versions, func(i, j int) bool { return versions[i].ID < versions[j].ID })
	return s.Active, versions, nil
}

func (m *Manager) Create(v Version) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if strings.TrimSpace(v.ID) == "" || strings.TrimSpace(v.Embedding) == "" || strings.TrimSpace(v.Mapping) == "" {
		return errors.New("indexversion: id, embedding and mapping are required")
	}
	if v.Dimensions <= 0 || v.Expected < 0 {
		return errors.New("indexversion: dimensions must be positive and expected must be non-negative")
	}
	s, err := m.load()
	if err != nil {
		return err
	}
	if _, exists := s.Versions[v.ID]; exists {
		return fmt.Errorf("indexversion: version %q already exists", v.ID)
	}
	v.Status = StatusBuilding
	v.CreatedAt = time.Now().UTC()
	s.Versions[v.ID] = v
	return m.save(s)
}

func (m *Manager) Progress(id string, indexed int) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.load()
	if err != nil {
		return err
	}
	v, ok := s.Versions[id]
	if !ok {
		return fmt.Errorf("indexversion: version %q not found", id)
	}
	if v.Status != StatusBuilding {
		return fmt.Errorf("indexversion: version %q is %s", id, v.Status)
	}
	if indexed < 0 || indexed > v.Expected {
		return fmt.Errorf("indexversion: indexed=%d outside 0..%d", indexed, v.Expected)
	}
	v.Indexed = indexed
	s.Versions[id] = v
	return m.save(s)
}

func (m *Manager) Complete(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.load()
	if err != nil {
		return err
	}
	v, ok := s.Versions[id]
	if !ok {
		return fmt.Errorf("indexversion: version %q not found", id)
	}
	if v.Status != StatusBuilding {
		return fmt.Errorf("indexversion: version %q is %s", id, v.Status)
	}
	if v.Indexed != v.Expected {
		return fmt.Errorf("indexversion: version %q incomplete: %d/%d", id, v.Indexed, v.Expected)
	}
	v.Status, v.CompletedAt = StatusReady, time.Now().UTC()
	s.Versions[id] = v
	return m.save(s)
}

func (m *Manager) Fail(id string, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.load()
	if err != nil {
		return err
	}
	v, ok := s.Versions[id]
	if !ok {
		return fmt.Errorf("indexversion: version %q not found", id)
	}
	if s.Active == id {
		return fmt.Errorf("indexversion: active version %q cannot be marked failed", id)
	}
	v.Status, v.Error = StatusFailed, strings.TrimSpace(reason)
	s.Versions[id] = v
	return m.save(s)
}

func (m *Manager) Switch(id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	s, err := m.load()
	if err != nil {
		return err
	}
	v, ok := s.Versions[id]
	if !ok {
		return fmt.Errorf("indexversion: version %q not found", id)
	}
	if v.Status != StatusReady {
		return fmt.Errorf("indexversion: version %q is not ready", id)
	}
	s.Active = id
	return m.save(s)
}

func (m *Manager) Rollback(id string) error { return m.Switch(id) }

func (m *Manager) load() (state, error) {
	data, err := os.ReadFile(m.path)
	if errors.Is(err, os.ErrNotExist) {
		return state{Versions: map[string]Version{}}, nil
	}
	if err != nil {
		return state{}, err
	}
	var s state
	if err := json.Unmarshal(data, &s); err != nil {
		return state{}, fmt.Errorf("indexversion: read state: %w", err)
	}
	if s.Versions == nil {
		s.Versions = map[string]Version{}
	}
	return s, nil
}

func (m *Manager) save(s state) error {
	if err := os.MkdirAll(filepath.Dir(m.path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	tmp := m.path + ".tmp"
	if err := os.WriteFile(tmp, append(data, '\n'), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, m.path)
}
