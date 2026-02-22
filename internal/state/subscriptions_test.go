package state_test

import (
	"os"
	"testing"

	"github.com/dfbb/im2code/internal/state"
)

func TestSubscriptions(t *testing.T) {
	f, _ := os.CreateTemp("", "subs-*.json")
	f.Close()
	defer os.Remove(f.Name())

	store, err := state.NewSubscriptions(f.Name())
	if err != nil {
		t.Fatalf("NewSubscriptions error: %v", err)
	}

	store.Set("telegram:123", "dev")

	got, ok := store.Get("telegram:123")
	if !ok || got != "dev" {
		t.Errorf("Get() = %q, %v; want %q, true", got, ok, "dev")
	}

	store.Delete("telegram:123")
	_, ok = store.Get("telegram:123")
	if ok {
		t.Error("expected key to be deleted")
	}

	// reload from disk
	store2, _ := state.NewSubscriptions(f.Name())
	_, ok = store2.Get("telegram:123")
	if ok {
		t.Error("expected deleted key to not be in reloaded store")
	}
}

func TestSubscriptions_Persist(t *testing.T) {
	f, _ := os.CreateTemp("", "subs-*.json")
	f.Close()
	defer os.Remove(f.Name())

	store, _ := state.NewSubscriptions(f.Name())
	store.Set("slack:C001", "prod")

	store2, _ := state.NewSubscriptions(f.Name())
	got, ok := store2.Get("slack:C001")
	if !ok || got != "prod" {
		t.Errorf("persisted Get() = %q, %v; want %q, true", got, ok, "prod")
	}
}

func TestSubscriptions_All(t *testing.T) {
	f, _ := os.CreateTemp("", "subs-*.json")
	f.Close()
	defer os.Remove(f.Name())

	store, _ := state.NewSubscriptions(f.Name())
	store.Set("telegram:1", "dev")
	store.Set("slack:2", "prod")

	all := store.All()
	if len(all) != 2 {
		t.Errorf("All() returned %d entries, want 2", len(all))
	}
}
