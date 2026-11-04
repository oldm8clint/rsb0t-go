package controlpanelapp

import (
	"os"
	"path/filepath"
	"testing"
)

func TestCredentialsFallBackToTheSharedPassword(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "credentials.json"), []byte(`{"Alpha":"pw-a","beta":""}`), 0o600); err != nil {
		t.Fatal(err)
	}
	c, err := loadCredentials(filepath.Join(dir, "fleet.config.json"), "credentials.json")
	if err != nil {
		t.Fatal(err)
	}
	if got := c.passwordFor("alpha", "shared"); got != "pw-a" {
		t.Fatalf("alpha: %q, want its own entry matched case-insensitively", got)
	}
	if got := c.passwordFor("beta", "shared"); got != "shared" {
		t.Fatalf("beta has an empty entry: %q, want the shared password", got)
	}
	if got := c.passwordFor("gamma", "shared"); got != "shared" {
		t.Fatalf("gamma: %q", got)
	}
	accts := []accountConfig{{Username: "alpha"}, {Username: "gamma"}}
	if m := missingPassword(accts, c, ""); m != "gamma" {
		t.Fatalf("missing: %q, want gamma", m)
	}
	if m := missingPassword(accts, c, "x"); m != "" {
		t.Fatalf("missing with a shared password: %q", m)
	}
}

func TestNoCredentialsFileIsNoCredentials(t *testing.T) {
	c, err := loadCredentials("fleet.config.json", "")
	if err != nil || c != nil {
		t.Fatalf("%v %v", c, err)
	}
	if got := c.passwordFor("a", "s"); got != "s" {
		t.Fatalf("nil credentials: %q", got)
	}
	if _, err := loadCredentials(filepath.Join(t.TempDir(), "r.json"), "nope.json"); err == nil {
		t.Fatal("a named file that is missing must be an error")
	}
}
