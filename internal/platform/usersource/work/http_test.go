package work

import (
	"eino-quickstart/internal/platform/im/wechat"
	"testing"
)

var client *wechat.Client
func TestFetchByExternalID(t *testing.T) {
	userSync := 	NewUserSync(client)
	users,err := userSync.FetchByExternalID(t.Context(),"10851")
	if err != nil {
		t.Fatalf("Failed to fetch users: %v", err)
	}
	t.Logf("Fetched users: %+v", users)
}


func TestMain(m *testing.M) {
	// Initialize the client here if needed
	client = wechat.NewClient(map[string]*wechat.Config{
		"Work": {
			AppID:     "wwd342688f274e37bd",
			Secret:    "Yi0BXHu7M2P1QGgPe_4HcU4zc-ZcaHnNNiK9B4u1mKA",
			// 
			AgentID:   1000057,
			HttpDebug: true,
		},
	}, nil)
	m.Run()
}