package config

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"sort"
	"strings"
)

const userPlaceholder = "{USER}"

// expandUser fills {USER} with the username upper-cased, anything but letters and digits as _.
func expandUser(template, username string) string {
	if !strings.Contains(template, userPlaceholder) {
		return template
	}
	var b strings.Builder
	for i := 0; i < len(username); i++ {
		c := username[i]
		switch {
		case c >= 'a' && c <= 'z':
			b.WriteByte(c - 'a' + 'A')
		case c >= 'A' && c <= 'Z', c >= '0' && c <= '9':
			b.WriteByte(c)
		default:
			b.WriteByte('_')
		}
	}
	return strings.ReplaceAll(template, userPlaceholder, b.String())
}

// checkUsername returns why name cannot be a login name, or "".
func checkUsername(name string) string {
	if name == "" {
		return "is required"
	}
	if len(name) > 12 {
		return fmt.Sprintf("must be at most 12 characters, got %d", len(name))
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ' ' || c == '_') {
			return fmt.Sprintf("may hold only letters, digits, spaces and _, got %q", name)
		}
	}
	if strings.TrimSpace(name) != name {
		return fmt.Sprintf("must not start or end with a space, got %q", name)
	}
	if userhash(name) == 0 {
		return fmt.Sprintf("needs at least one letter or digit, got %q", name)
	}
	return ""
}

// userhash is JString.toUserhash from the vendored client: base 37 over the first 12 characters, case
// folded, anything else as 0, trailing zero digits stripped. Two names with one hash are one
// account to the server, which is what the duplicate check needs.
func userhash(name string) uint64 {
	name = strings.TrimSpace(name)
	var l uint64
	for i := 0; i < len(name) && i < 12; i++ {
		c := name[i]
		l *= 37
		switch {
		case c >= 'A' && c <= 'Z':
			l += uint64(c) + 1 - 'A'
		case c >= 'a' && c <= 'z':
			l += uint64(c) + 1 - 'a'
		case c >= '0' && c <= '9':
			l += uint64(c) + 27 - '0'
		}
	}
	for l%37 == 0 && l != 0 {
		l /= 37
	}
	return l
}

// credentials maps a userhash to its password.
type credentials map[uint64]string

type credentialEntry struct {
	username, password string
}

// loadCredentials reads the credentials file, either username:password lines or a JSON object.
// Its messages never quote a password.
func loadCredentials(path string, r reporter) (credentials, bool) {
	if path == "" {
		return nil, true
	}
	const field = "login.credentialsFile"
	data, err := os.ReadFile(path)
	if err != nil {
		// checkFile has already reported a missing or unreadable file.
		return nil, false
	}
	data = bytes.TrimPrefix(data, utf8BOM)

	var entries []credentialEntry
	if trimmed := bytes.TrimSpace(data); len(trimmed) > 0 && trimmed[0] == '{' {
		var raw map[string]string
		if err := json.Unmarshal(trimmed, &raw); err != nil {
			r.add(field, "%s must be username:password lines or a JSON object mapping each username to its password", path)
			return nil, false
		}
		for name, pw := range raw {
			entries = append(entries, credentialEntry{name, pw})
		}
		sort.Slice(entries, func(i, j int) bool { return entries[i].username < entries[j].username })
	} else {
		// Why: the same parse as e2e/headless-parity-live.ts, so one accounts file serves both. A
		// line without a colon is skipped there too; # comments are an addition no username can hit.
		for _, line := range strings.Split(string(data), "\n") {
			line = strings.TrimSpace(line)
			if line == "" || strings.HasPrefix(line, "#") {
				continue
			}
			if i := strings.IndexByte(line, ':'); i >= 0 {
				entries = append(entries, credentialEntry{line[:i], line[i+1:]})
			}
		}
	}

	creds := make(credentials, len(entries))
	first := make(map[uint64]string, len(entries))
	ok := true
	for _, e := range entries {
		if msg := checkUsername(e.username); msg != "" {
			r.add(field, "entry %q: username %s", e.username, msg)
			ok = false
			continue
		}
		h := userhash(e.username)
		if prev, dup := first[h]; dup {
			r.add(field, "entries %q and %q name the same account", prev, e.username)
			ok = false
			continue
		}
		first[h] = e.username
		creds[h] = e.password
	}
	return creds, ok
}

// resolvePassword takes the password from passwordEnv when that variable is set and non-empty,
// otherwise from the credentials file.
func (a *Account) resolvePassword(r reporter, lookup func(string) (string, bool), creds credentials) {
	var pw, from, envName string
	if a.PasswordEnv != "" {
		envName = expandUser(a.PasswordEnv, a.Username)
		if v, ok := lookup(envName); ok && v != "" {
			pw, from = v, "environment variable "+envName
		}
	}
	if pw == "" {
		if v, ok := creds[userhash(a.Username)]; ok && v != "" {
			pw, from = v, "login.credentialsFile"
		}
	}
	switch {
	case pw == "" && envName != "":
		r.add("password", "none found: environment variable %s is unset or empty and login.credentialsFile has no entry", envName)
		return
	case pw == "":
		r.add("password", "none found: set passwordEnv or add an entry to login.credentialsFile")
		return
	case len(pw) > 20:
		// Why: the server answers 3 (invalid username or password) to anything over 20 characters.
		r.add("password", "from %s must be at most 20 characters, got %d", from, len(pw))
		return
	}
	for i := 0; i < len(pw); i++ {
		if pw[i] < 0x20 || pw[i] > 0x7e {
			// Why: the login block writes the password as a newline-terminated byte string.
			r.add("password", "from %s must be printable ASCII", from)
			return
		}
	}
	a.Password = Secret(pw)
}
