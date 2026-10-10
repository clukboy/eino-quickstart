package indexversion

import "testing"

func TestShadowBuildSwitchAndRollbackAreGated(t *testing.T) {
	m, err := New(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	v := Version{ID: "v2", Embedding: "embed-v2", Dimensions: 1024, Mapping: "map-a", Expected: 2}
	if err := m.Create(v); err != nil {
		t.Fatal(err)
	}
	if err := m.Switch("v2"); err == nil {
		t.Fatal("building version became active")
	}
	if err := m.Progress("v2", 1); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete("v2"); err == nil {
		t.Fatal("incomplete version became ready")
	}
	if err := m.Progress("v2", 2); err != nil {
		t.Fatal(err)
	}
	if err := m.Complete("v2"); err != nil {
		t.Fatal(err)
	}
	if err := m.Switch("v2"); err != nil {
		t.Fatal(err)
	}
	active, versions, err := m.State()
	if err != nil {
		t.Fatal(err)
	}
	if active != "v2" || len(versions) != 1 || versions[0].Status != StatusReady {
		t.Fatalf("state = %q %#v", active, versions)
	}
	if err := m.Fail("v2", "test failure"); err == nil {
		t.Fatal("active ready version was allowed to fail")
	}
}

func TestFailedVersionCannotBeActivated(t *testing.T) {
	m, err := New(t.TempDir() + "/state.json")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Create(Version{ID: "bad", Embedding: "e", Dimensions: 1, Mapping: "m", Expected: 0}); err != nil {
		t.Fatal(err)
	}
	if err := m.Fail("bad", "embedding unavailable"); err != nil {
		t.Fatal(err)
	}
	if err := m.Switch("bad"); err == nil {
		t.Fatal("failed version became active")
	}
}
