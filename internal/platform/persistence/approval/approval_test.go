package approval

import (
	"testing"
	"time"

	"eino-quickstart/internal/platform/privacy"
)

func TestNewStorePreservesDependencies(t *testing.T) {
	policy, err := privacy.NewArgumentPolicy(1024, []string{"token"})
	if err != nil {
		t.Fatal(err)
	}

	store := NewStore(nil, policy, time.Minute)
	if store.argumentPolicy.MaxBytes != 1024 {
		t.Fatalf("max bytes = %d, want 1024", store.argumentPolicy.MaxBytes)
	}
	if store.approvalTTL != time.Minute {
		t.Fatalf("approval TTL = %s, want %s", store.approvalTTL, time.Minute)
	}
}
