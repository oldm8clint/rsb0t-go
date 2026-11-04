package main

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"rsb0t-go/internal/controlpanel"
)

const (
	rosterFile      = "fleet.config.json"
	credentialsFile = "credentials.json"
)

var defaultListen = controlpanel.DefaultListen

// starterRoster is what `rsb0t init` writes: no server yet (the setup page and `rsb0t server` ask for
// one; rsb0t names no server of its own), a 3 second login pace, and no accounts. Passwords are never in
// it: they live in credentials.json, which the roster names.
const starterRoster = `{
  "server": "",
  "wsURL": "",
  "httpBase": "",
  "loginDelaySeconds": 3,
  "credentialsFile": "credentials.json",
  "worldList": [],
  "accounts": []
}
`

// roster is the roster file as a JSON object whose keys rsb0t does not manage are kept as they were.
type roster struct {
	path   string
	fields map[string]json.RawMessage
	order  []string
}

// ensureRoster creates the data folder and the starter roster when they are missing, and loads it.
func ensureRoster(home string) (*roster, error) {
	if err := os.MkdirAll(home, 0o755); err != nil {
		return nil, err
	}
	p := filepath.Join(home, rosterFile)
	if _, err := os.Stat(p); errors.Is(err, os.ErrNotExist) {
		if err := os.WriteFile(p, []byte(starterRoster), 0o644); err != nil {
			return nil, err
		}
	}
	return loadRoster(p)
}

func loadRoster(p string) (*roster, error) {
	b, err := os.ReadFile(p)
	if err != nil {
		return nil, err
	}
	r := &roster{path: p, fields: map[string]json.RawMessage{}}
	if err := json.Unmarshal(b, &r.fields); err != nil {
		return nil, fmt.Errorf("%s: %w", p, err)
	}
	// Keep the file's key order, so editing by hand and by command give one stable diff.
	dec := json.NewDecoder(bytes.NewReader(b))
	if _, err := dec.Token(); err == nil {
		for dec.More() {
			k, err := dec.Token()
			if err != nil {
				break
			}
			r.order = append(r.order, k.(string))
			var skip json.RawMessage
			if err := dec.Decode(&skip); err != nil {
				break
			}
		}
	}
	return r, nil
}

// accountEntry is one roster account as a raw JSON object, so keys rsb0t does not know are kept.
type accountEntry map[string]json.RawMessage

func (r *roster) accounts() []accountEntry {
	var raw []accountEntry
	_ = json.Unmarshal(r.fields["accounts"], &raw)
	return raw
}

func (a accountEntry) name() string {
	var s string
	_ = json.Unmarshal(a["username"], &s)
	return s
}

func (a accountEntry) script() string {
	var s string
	_ = json.Unmarshal(a["script"], &s)
	return s
}

func (r *roster) setAccounts(list []accountEntry) {
	if list == nil {
		list = []accountEntry{}
	}
	b, _ := json.Marshal(list)
	r.fields["accounts"] = b
	r.ensureKey("accounts")
}

func (r *roster) ensureKey(k string) {
	for _, o := range r.order {
		if o == k {
			return
		}
	}
	r.order = append(r.order, k)
}

// find is the index of the account named name (case-insensitively, as the server reads names), or -1.
func (r *roster) find(name string) int {
	for i, a := range r.accounts() {
		if strings.EqualFold(a.name(), name) {
			return i
		}
	}
	return -1
}

// save writes the roster back, indented, keys in their original order.
func (r *roster) save() error {
	var buf bytes.Buffer
	buf.WriteString("{\n")
	for i, k := range r.order {
		v, ok := r.fields[k]
		if !ok {
			continue
		}
		var ind bytes.Buffer
		if err := json.Indent(&ind, v, "  ", "  "); err != nil {
			return err
		}
		kb, _ := json.Marshal(k)
		fmt.Fprintf(&buf, "  %s: %s", kb, ind.String())
		if i < len(r.order)-1 {
			buf.WriteString(",")
		}
		buf.WriteString("\n")
	}
	buf.WriteString("}\n")
	tmp := r.path + ".tmp"
	if err := os.WriteFile(tmp, buf.Bytes(), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, r.path)
}

// checkName is why name cannot be a login name, "" when it can: at most 12 letters, digits, spaces and
// underscores, not starting or ending with a space (the rule internal/config applies).
func checkName(name string) string {
	switch {
	case name == "":
		return "is empty"
	case len(name) > 12:
		return fmt.Sprintf("is %d characters; a login name has at most 12", len(name))
	case strings.TrimSpace(name) != name:
		return "starts or ends with a space"
	}
	for i := 0; i < len(name); i++ {
		c := name[i]
		if !(c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == ' ' || c == '_') {
			return fmt.Sprintf("has %q in it; a login name is letters, digits, spaces and _", c)
		}
	}
	return ""
}

// credentials is credentials.json: login name to password, written readable by the owner only.
type credentials map[string]string

func loadCredentials(home string) (credentials, error) {
	b, err := os.ReadFile(filepath.Join(home, credentialsFile))
	if errors.Is(err, os.ErrNotExist) {
		return credentials{}, nil
	}
	if err != nil {
		return nil, err
	}
	c := credentials{}
	if err := json.Unmarshal(b, &c); err != nil {
		return nil, fmt.Errorf("%s: %w", credentialsFile, err)
	}
	return c, nil
}

func (c credentials) save(home string) error {
	b, _ := json.MarshalIndent(c, "", "  ")
	p := filepath.Join(home, credentialsFile)
	tmp := p + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, p)
}

func (c credentials) delete(name string) {
	for k := range c {
		if strings.EqualFold(k, name) {
			delete(c, k)
		}
	}
}

func (c credentials) passwordFor(name string) string {
	for k, v := range c {
		if strings.EqualFold(k, name) {
			return v
		}
	}
	return ""
}
