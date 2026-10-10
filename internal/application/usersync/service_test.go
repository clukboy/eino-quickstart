package usersync

import (
	"context"
	"testing"

	"eino-quickstart/internal/platform/persistence/account"
	"eino-quickstart/internal/platform/usersource"
)

type fakeSource struct{ users []*usersource.User }


var _ usersource.Source = (*fakeSource)(nil)
func (f *fakeSource) Fetch(context.Context) ([]*usersource.User, error) { return f.users, nil }


// FetchByExternalID implements [usersource.Source].
func (f *fakeSource) FetchByExternalID(ctx context.Context, externalID string) (*usersource.User, error) {
	return nil,nil
}


type fakeStore struct {
	byExternal map[string]*account.Record
	byUsername map[string]*account.Credentials
	nextID     uint64
}

func newFakeStore() *fakeStore {
	return &fakeStore{byExternal: map[string]*account.Record{}, byUsername: map[string]*account.Credentials{}, nextID: 1}
}
func (s *fakeStore) ByExternalID(_ context.Context, id string) (*account.Record, error) {
	if r := s.byExternal[id]; r != nil {
		c := *r
		return &c, nil
	}
	return nil, account.ErrNotFound
}
func (s *fakeStore) ByUsername(_ context.Context, username string) (*account.Credentials, error) {
	if r := s.byUsername[username]; r != nil {
		c := *r
		return &c, nil
	}
	return nil, account.ErrNotFound
}
func (s *fakeStore) UpdateExternalUser(_ context.Context, id uint64,externalID, username, nickname string) error {
	for key, r := range s.byExternal {
		if r.ID == id {
			delete(s.byUsername, r.Username)
			r.Username, r.Nickname = username, nickname
			s.byUsername[username] = &account.Credentials{Record: *r}
			s.byExternal[key] = r
			return nil
		}
	}
	return account.ErrNotFound
}
func (s *fakeStore) Create(_ context.Context, p account.CreateParams) (*account.Record, error) {
	if _, ok := s.byUsername[p.Username]; ok {
		return nil, account.ErrUsernameTaken
	}
	r := &account.Record{ID: s.nextID, Username: p.Username, Nickname: p.Nickname, ExternalID: p.ExternalID, Role: "agent", Status: "ACTIVE", MustChangePassword: true}
	s.nextID++
	s.byExternal[*p.ExternalID] = r
	s.byUsername[p.Username] = &account.Credentials{Record: *r}
	return r, nil
}

func TestSyncCreatesUpdatesAndIsIdempotentWithoutDeletes(t *testing.T) {
	store := newFakeStore()
	source := &fakeSource{users: []*usersource.User{{ExternalID: "u-1", Username: "alice", Nickname: "Alice"}, {ExternalID: "u-2", Username: "bob", Nickname: "Bob"}}}
	service, err := NewService(store, source, 4)
	if err != nil {
		t.Fatal(err)
	}
	first, err := service.Sync(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if first.Created != 2 || len(first.CreatedUsers) != 2 || first.Failed != 0 {
		t.Fatalf("first report = %+v", first)
	}
	for _, created := range first.CreatedUsers {
		if created.InitialPassword == "" {
			t.Fatal("created user has no one-time password")
		}
	}
	source.users[0].Nickname = "Alice Updated"
	second, err := service.Sync(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if second.Updated != 1 || second.Skipped != 1 || second.Created != 0 {
		t.Fatalf("second report = %+v", second)
	}
	third, err := service.Sync(context.Background(), false)
	if err != nil {
		t.Fatal(err)
	}
	if third.Created != 0 || third.Updated != 0 || third.Skipped != 2 {
		t.Fatalf("third report = %+v", third)
	}
	if len(store.byExternal) != 2 {
		t.Fatalf("sync deleted a local user: %+v", store.byExternal)
	}
}

func TestSyncDryRunDoesNotWrite(t *testing.T) {
	store := newFakeStore()
	service, err := NewService(store, &fakeSource{users: []*usersource.User{{ExternalID: "u-1", Username: "alice", Nickname: "Alice"}}}, 4)
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.Sync(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if !report.DryRun || report.Created != 1 || len(report.CreatedUsers) != 0 || len(store.byExternal) != 0 {
		t.Fatalf("dry-run report = %+v, store=%+v", report, store.byExternal)
	}
}

func TestSyncRejectsDuplicateExternalIDs(t *testing.T) {
	service, err := NewService(newFakeStore(), &fakeSource{users: []*usersource.User{{ExternalID: "u-1", Username: "alice", Nickname: "Alice"}, {ExternalID: "u-1", Username: "alice2", Nickname: "Alice 2"}}}, 4)
	if err != nil {
		t.Fatal(err)
	}
	report, err := service.Sync(context.Background(), true)
	if err != nil {
		t.Fatal(err)
	}
	if report.Failed != 1 || len(report.Errors) != 1 {
		t.Fatalf("report = %+v", report)
	}
}
