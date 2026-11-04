package registry

import (
	"errors"
	"path/filepath"
	"testing"
)

func TestJSONStorePersistsLifecycleMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "registry.json")
	store, err := OpenJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	want := Account{ID: "account-0001", Provider: "liveserver", Status: Pending, SecretRef: "secret/account-0001"}
	if err := store.Create(want); err != nil {
		t.Fatal(err)
	}
	if _, err := Transition(store, want.ID, Authenticating); err != nil {
		t.Fatal(err)
	}
	if _, err := Transition(store, want.ID, Ready); err != nil {
		t.Fatal(err)
	}

	reloaded, err := OpenJSONStore(path)
	if err != nil {
		t.Fatal(err)
	}
	got, err := reloaded.Get(want.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.Status != Ready || got.SecretRef != want.SecretRef || got.Provider != want.Provider {
		t.Fatalf("unexpected persisted account: %+v", got)
	}
}

func TestTransitionRejectsInvalidStateChange(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Create(Account{ID: "account-0001", Provider: "liveserver", Status: Pending}); err != nil {
		t.Fatal(err)
	}
	_, err := Transition(store, "account-0001", Running)
	if !errors.Is(err, ErrInvalidTransition) {
		t.Fatalf("expected invalid transition, got %v", err)
	}
}

func TestInitStyleCreationIsIdempotent(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Create(Account{ID: "account-0001", Provider: "liveserver", Status: Pending}); err != nil {
		t.Fatal(err)
	}
	if err := store.Create(Account{ID: "account-0001", Provider: "liveserver", Status: Pending}); err == nil {
		t.Fatal("expected duplicate account error")
	}
}
