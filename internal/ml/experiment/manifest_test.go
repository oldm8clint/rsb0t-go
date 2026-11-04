package experiment

import (
	"os"
	"path/filepath"
	"testing"
)

func TestLoadUsesDefaultsForPartialManifest(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "manifest.json")
	if err := os.WriteFile(path, []byte(`{"name":"test","task":"fishing"}`), 0600); err != nil {
		t.Fatal(err)
	}
	m, err := Load(path)
	if err != nil {
		t.Fatalf("Load: %v", err)
	}
	if m.Name != "test" || m.Task != "fishing" {
		t.Fatalf("unexpected identity: %+v", m)
	}
	if m.Episodes <= 0 || m.Steps <= 0 {
		t.Fatalf("defaults should remain valid: %+v", m)
	}
}

func TestLoadRejectsInvalidLearningParameters(t *testing.T) {
	cases := []string{
		`{"name":"test","task":"fishing","alpha":0}`,
		`{"name":"test","task":"fishing","alpha":1.1}`,
		`{"name":"test","task":"fishing","gamma":-0.1}`,
		`{"name":"test","task":"fishing","gamma":1.1}`,
		`{"name":"test","task":"fishing","epsilon":-0.1}`,
		`{"name":"test","task":"fishing","epsilon":1.1}`,
	}
	for _, input := range cases {
		dir := t.TempDir()
		path := filepath.Join(dir, "manifest.json")
		if err := os.WriteFile(path, []byte(input), 0600); err != nil {
			t.Fatal(err)
		}
		if _, err := Load(path); err == nil {
			t.Fatalf("expected invalid parameter rejection for %s", input)
		}
	}
}
