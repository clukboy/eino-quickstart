package domain

type Metadata map[string]any

func (m Metadata) Get(key string) (any, bool) {
	v, ok := m[key]
	return v, ok
}

func (m Metadata) Set(key string, value any) {
	if m == nil {
		return
	}

	m[key] = value
}

func (m Metadata) String(key string) string {
	v, ok := m[key]
	if !ok {
		return ""
	}

	s, _ := v.(string)
	return s
}

func (m Metadata) Clone() Metadata {
	if m == nil {
		return nil
	}

	result := make(Metadata, len(m))

	for k, v := range m {
		result[k] = v
	}

	return result
}
