package learning

import (
	"os"
	"path/filepath"
	"testing"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

func TestCheckpointRoundTrip(t *testing.T) {
	q := NewQTable(7, .3, .9)
	s := observation.State{InventorySize: 28, InventoryUsed: 12, DistanceToFish: 1, NearbyFish: 3}
	q.Update(s, policy.Fish, 4, s, true)

	path := filepath.Join(t.TempDir(), "model.json")
	if err := q.Save(path); err != nil {
		t.Fatal(err)
	}
	loaded, err := Load(path, 99)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.States() != q.States() {
		t.Fatalf("states = %d, want %d", loaded.States(), q.States())
	}
	if got := loaded.Best(s).Action; got != policy.Fish {
		t.Fatalf("best action = %v, want %v", got, policy.Fish)
	}
}

func TestCheckpointRejectsUnknownVersion(t *testing.T) {
	path := filepath.Join(t.TempDir(), "bad.json")
	if err := os.WriteFile(path, []byte(`{"version":99}`), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Load(path, 1); err == nil {
		t.Fatal("Load accepted an unknown checkpoint version")
	}
}
