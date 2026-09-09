package domain

// Source describes where a document comes from.
type Source struct {
	Type  string         `json:"type"`
	URI   string         `json:"uri"`
	Name  string         `json:"name"`
	Extra map[string]any `json:"extra,omitempty"`
}
