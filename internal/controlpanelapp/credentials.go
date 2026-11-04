package controlpanelapp

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// credentials is the optional per-account password file a roster names with "credentialsFile": a JSON
// object of login name to password, kept beside the roster and outside any repository. An account with
// no entry falls back to the shared password (the -password-env variable, then sharedPassword), so a
// roster that names no file behaves exactly as it always did.
type credentials map[string]string

// loadCredentials reads path, resolved against the roster's own directory when relative. An empty path
// is no file. The file is read, never written, here; cmd/rsb0t's `account add` is what writes it.
func loadCredentials(rosterPath, path string) (credentials, error) {
	if path == "" {
		return nil, nil
	}
	if !filepath.IsAbs(path) {
		path = filepath.Join(filepath.Dir(rosterPath), path)
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("credentialsFile: %w", err)
	}
	var c credentials
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("credentialsFile %s: want an object of login name to password: %w", path, err)
	}
	return c, nil
}

// passwordFor is the password an account logs in with: its own entry, else shared. A login name is
// matched case-insensitively, as the server does.
func (c credentials) passwordFor(username, shared string) string {
	for k, v := range c {
		if strings.EqualFold(k, username) && v != "" {
			return v
		}
	}
	return shared
}

// missingPassword is the first account that would log in with no password, "" when none would.
func missingPassword(accounts []accountConfig, c credentials, shared string) string {
	for _, a := range accounts {
		if c.passwordFor(a.Username, shared) == "" {
			return a.Username
		}
	}
	return ""
}
