package main

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestInitWritesAStarterRosterWithNoAccountAndNoPassword(t *testing.T) {
	home := filepath.Join(t.TempDir(), "data")
	r, err := ensureRoster(home)
	if err != nil {
		t.Fatal(err)
	}
	if n := len(r.accounts()); n != 0 {
		t.Fatalf("a fresh roster has %d accounts", n)
	}
	b, _ := os.ReadFile(filepath.Join(home, rosterFile))
	if strings.Contains(strings.ToLower(string(b)), "password\":") && !strings.Contains(string(b), "credentialsFile") {
		t.Fatalf("the starter roster holds a password field:\n%s", b)
	}
	if strings.Contains(strings.ToLower(string(b)), "sharedpassword") {
		t.Fatalf("the starter roster names a shared password:\n%s", b)
	}
}

func TestAddListScriptRemoveRoundTrip(t *testing.T) {
	home := t.TempDir()
	t.Setenv("RSB0T_TEST_PW", "correct horse")
	if code := run([]string{"-data", home, "account", "add", "Alice_1", "-password-env", "RSB0T_TEST_PW"}); code != 0 {
		t.Fatalf("add exited %d", code)
	}
	r, err := loadRoster(filepath.Join(home, rosterFile))
	if err != nil {
		t.Fatal(err)
	}
	if i := r.find("alice_1"); i != 0 {
		t.Fatalf("find is case-insensitive: %d", i)
	}
	creds, _ := loadCredentials(home)
	if creds.passwordFor("ALICE_1") != "correct horse" {
		t.Fatal("the password was not stored")
	}
	// The password is in credentials.json only.
	b, _ := os.ReadFile(filepath.Join(home, rosterFile))
	if strings.Contains(string(b), "correct horse") {
		t.Fatalf("the roster holds the password:\n%s", b)
	}
	if runtime.GOOS != "windows" {
		fi, _ := os.Stat(filepath.Join(home, credentialsFile))
		if fi.Mode().Perm() != 0o600 {
			t.Fatalf("credentials.json is %v, want 0600", fi.Mode().Perm())
		}
	}
	if code := run([]string{"-data", home, "account", "add", "alice_1", "-password-env", "RSB0T_TEST_PW"}); code == 0 {
		t.Fatal("a duplicate name must be refused")
	}
	if code := run([]string{"-data", home, "account", "add", "far too long a name", "-password-env", "RSB0T_TEST_PW"}); code == 0 {
		t.Fatal("a name over 12 characters must be refused")
	}
	if code := run([]string{"-data", home, "account", "script", "Alice_1", "NoSuchScript"}); code == 0 {
		t.Fatal("an unknown script must be refused")
	}
	if code := run([]string{"-data", home, "account", "remove", "alice_1"}); code != 0 {
		t.Fatalf("remove exited %d", code)
	}
	r, _ = loadRoster(filepath.Join(home, rosterFile))
	if len(r.accounts()) != 0 {
		t.Fatal("the account is still in the roster")
	}
	creds, _ = loadCredentials(home)
	if len(creds) != 0 {
		t.Fatalf("the password is still stored: %v", creds)
	}
}

func TestSavePreservesKeysThisProgramDoesNotManage(t *testing.T) {
	home := t.TempDir()
	p := filepath.Join(home, rosterFile)
	if err := os.WriteFile(p, []byte(`{"wsURL":"ws://localhost/","modulus":"123","accounts":[{"username":"a","script":"","world":0,"settings":{"k":1},"extra":true}]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	r, err := loadRoster(p)
	if err != nil {
		t.Fatal(err)
	}
	if err := r.save(); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(p)
	for _, want := range []string{`"modulus": "123"`, `"extra": true`, `"k": 1`} {
		if !strings.Contains(string(b), want) {
			t.Fatalf("lost %s:\n%s", want, b)
		}
	}
}

func TestCheckName(t *testing.T) {
	for name, ok := range map[string]bool{"abc": true, "Zezima 1": true, "a_b": true, "": false, "thirteen_char": false, " lead": false, "bad-name": false} {
		if (checkName(name) == "") != ok {
			t.Errorf("checkName(%q) = %q, want ok=%v", name, checkName(name), ok)
		}
	}
}

func TestAccountAddRefusesAPipedPasswordWithoutTheFlag(t *testing.T) {
	r, w, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	w.WriteString("hunter2\n")
	w.Close()
	old := os.Stdin
	os.Stdin = r
	defer func() { os.Stdin = old; r.Close() }()
	home := t.TempDir()
	if code := run([]string{"-data", home, "account", "add", "Piped_1"}); code == 0 {
		t.Fatal("a piped password was accepted without -password-stdin")
	}
	creds, _ := loadCredentials(home)
	if creds.passwordFor("Piped_1") != "" {
		t.Fatal("the piped password was stored")
	}
}
