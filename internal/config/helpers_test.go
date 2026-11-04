package config

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

const minimalDefaults = `{"passwordEnv":"PW_{USER}","script":"idle"}`

var testEnv = map[string]string{"PW_ALICE": "alicepw", "PW_BOB": "bobpw", "PW_BOB_SMITH": "smithpw", "PW_CAROL": "carolpw"}

// testModulus is a stand-in login modulus: the loader checks only its digit count.
var testModulus = strings.Repeat("7", 300)

func envOf(m map[string]string) func(string) (string, bool) {
	return func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}
}

func withEnv(extra map[string]string) map[string]string {
	out := make(map[string]string, len(testEnv)+len(extra))
	for k, v := range testEnv {
		out[k] = v
	}
	for k, v := range extra {
		out[k] = v
	}
	return out
}

// doc builds a version 1 settings document; an empty argument takes a minimal valid value.
func doc(global, defaults, accounts string) string {
	if global == "" {
		global = `{}`
	}
	if defaults == "" {
		defaults = minimalDefaults
	}
	if accounts == "" {
		accounts = `[{"username":"alice"}]`
	}
	return `{"version":1,"global":` + global + `,"defaults":` + defaults + `,"accounts":` + accounts + `}`
}

func parseIn(t *testing.T, dir, text string, env map[string]string) (*Config, error) {
	t.Helper()
	return Parse([]byte(text), Options{BaseDir: dir, LookupEnv: envOf(env)})
}

func mustParseIn(t *testing.T, dir, text string, env map[string]string) *Config {
	t.Helper()
	cfg, err := parseIn(t, dir, text, env)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	return cfg
}

func mustParse(t *testing.T, text string, env map[string]string) *Config {
	t.Helper()
	return mustParseIn(t, t.TempDir(), text, env)
}

func writeFile(t *testing.T, dir, name, content string) string {
	t.Helper()
	path := filepath.Join(dir, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func wantProblem(t *testing.T, err error, where, field, msg string) {
	t.Helper()
	if err == nil {
		t.Fatalf("want a problem at %s: %s containing %q, got none", where, field, msg)
	}
	var e *Error
	if !errors.As(err, &e) {
		t.Fatalf("want *config.Error, got %T: %v", err, err)
	}
	for _, p := range e.Problems {
		if p.Where == where && p.Field == field && strings.Contains(p.Msg, msg) {
			return
		}
	}
	t.Fatalf("want a problem at %s: %s containing %q, got:\n%v", where, field, msg, err)
}

func jsonEqual(t *testing.T, name string, got json.RawMessage, want string) {
	t.Helper()
	var g, w interface{}
	if err := json.Unmarshal(got, &g); err != nil {
		t.Fatalf("%s: %v", name, err)
	}
	if err := json.Unmarshal([]byte(want), &w); err != nil {
		t.Fatalf("%s: bad want: %v", name, err)
	}
	if !reflect.DeepEqual(g, w) {
		t.Errorf("%s = %s, want %s", name, got, want)
	}
}
