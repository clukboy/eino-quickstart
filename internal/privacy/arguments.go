package privacy

import (
	"crypto/sha256"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/goccy/go-json"
)

const RedactedValue = "[REDACTED]"

type ArgumentPolicy struct {
	MaxBytes      int
	SensitiveKeys map[string]struct{}
}

type PreparedArgument struct {
	Hash    string
	Display string
}

func NewArgumentPolicy(maxBytes int, sensitiveKeys []string) (ArgumentPolicy, error) {
	if maxBytes <= 0 {
		return ArgumentPolicy{}, fmt.Errorf("maxBytes must be greater than 0")
	}
	keys := make(map[string]struct{}, len(sensitiveKeys))
	for _, key := range sensitiveKeys {

		keys[key] = struct{}{}
	}
	return ArgumentPolicy{
		MaxBytes:      maxBytes,
		SensitiveKeys: make(map[string]struct{}, len(sensitiveKeys)),
	}, nil
}

func (p ArgumentPolicy) Prepare(raw string) (PreparedArgument, error) {
	if len(raw) > p.MaxBytes {
		return PreparedArgument{}, fmt.Errorf("tool argument exceeds maximum bytes: %d", p.MaxBytes)
	}
	decoder := json.NewDecoder(strings.NewReader(raw))
	decoder.UseNumber()

	var value any
	if err := decoder.Decode(&value); err != nil {
		return PreparedArgument{}, fmt.Errorf("decode tool argument: %w", err)
	}

	var trailing any
	if err := decoder.Decode(&trailing); err != nil && err != io.EOF {
		return PreparedArgument{}, fmt.Errorf("tool arguments must contain one JSON value: %w", err)
	}
	canonical, err := json.Marshal(value)
	if err != nil {
		return PreparedArgument{}, fmt.Errorf("canonicalize tool argument: %w", err)
	}
	sum := sha256.Sum256(canonical)
	display, err := json.Marshal(p.redact(value))
	if err != nil {
		return PreparedArgument{}, fmt.Errorf("redact tool argument: %w", err)
	}
	return PreparedArgument{
		Hash:    fmt.Sprintf("%x", sum),
		Display: string(display),
	}, nil
}

func (p ArgumentPolicy) redact(value any) any {
	switch typed := value.(type) {
	case map[string]any:
		result := make(map[string]any, len(typed))
		for key, child := range typed {
			if p.isSensitiveKey(key) {
				result[key] = RedactedValue
				continue
			}
			result[key] = p.redact(child)
		}
		return result
	case []any:
		result := make([]any, len(typed))
		for i, child := range typed {
			result[i] = p.redact(child)
		}
		return result
	case string:
		return redactSensitiveText(typed)
	default:
		return value
	}
}

func (p ArgumentPolicy) isSensitiveKey(key string) bool {
	_, ok := p.SensitiveKeys[normalizeKey(key)]
	return ok
}
func normalizeKey(key string) string {
	key = strings.ToLower(key)
	key = strings.ReplaceAll(key, "-", "")
	key = strings.ReplaceAll(key, "_", "")
	return strings.TrimSpace(key)
}

var sensitiveTextPatterns = []*regexp.Regexp{
	regexp.MustCompile(`(?i)(authorization\s*[:=]\s*)(\S+)`),
	regexp.MustCompile(`(?i)((?:api[_-]?key|token|password|secret)\s*=\s*)(\S+)`),
	regexp.MustCompile(`(?i)(://[^:\s]+:)([^@\s]+)(@)`),
}

func redactSensitiveText(value string) string {
	result := value
	for _, pattern := range sensitiveTextPatterns {
		result = pattern.ReplaceAllString(result, `${1}`+RedactedValue)
	}
	return result
}
