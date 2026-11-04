package config

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

// FieldError is one problem in a settings file.
type FieldError struct {
	// Where is "file", "global", "defaults" or an account such as `accounts[2] "alice"`.
	Where string
	// Field is the dotted JSON path inside Where, empty when the problem is the whole section.
	Field string
	Msg   string
}

func (e *FieldError) Error() string {
	s := e.Where
	if e.Field != "" {
		s += ": " + e.Field
	}
	return s + ": " + e.Msg
}

// Error is every problem Load found, so a large fleet file is fixed in one pass.
type Error struct {
	Problems []*FieldError
}

func (e *Error) Error() string {
	if len(e.Problems) == 1 {
		return "config: " + e.Problems[0].Error()
	}
	var b strings.Builder
	fmt.Fprintf(&b, "config: %d problems", len(e.Problems))
	for _, p := range e.Problems {
		b.WriteString("\n  ")
		b.WriteString(p.Error())
	}
	return b.String()
}

type collector struct {
	list []*FieldError
}

func (c *collector) at(where string) reporter { return reporter{c: c, where: where} }

func (c *collector) err() error {
	if len(c.list) == 0 {
		return nil
	}
	return &Error{Problems: c.list}
}

type reporter struct {
	c     *collector
	where string
}

func (r reporter) add(field, format string, args ...interface{}) {
	r.c.list = append(r.c.list, &FieldError{Where: r.where, Field: field, Msg: fmt.Sprintf(format, args...)})
}

// Options controls Parse and LoadWith.
type Options struct {
	// BaseDir anchors relative paths in the file. LoadWith defaults it to the file's directory.
	BaseDir string
	// LookupEnv resolves passwordEnv and controlPanel.tokenEnv; nil means os.LookupEnv.
	LookupEnv func(key string) (string, bool)
	// Profile, when set, replaces server.profile, so a command-line flag can switch between the
	// live server and a local engine without editing the file.
	Profile string
}

// Load reads, validates and normalises the settings file at path.
func Load(path string) (*Config, error) {
	return LoadWith(path, Options{})
}

// LoadWith is Load with options.
func LoadWith(path string, opts Options) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("config: %w", err)
	}
	if opts.BaseDir == "" {
		opts.BaseDir = filepath.Dir(path)
	}
	cfg, err := Parse(data, opts)
	if err != nil {
		return nil, err
	}
	cfg.Path = path
	return cfg, nil
}

type fileShape struct {
	Version  int               `json:"version"`
	Global   json.RawMessage   `json:"global"`
	Defaults json.RawMessage   `json:"defaults"`
	Accounts []json.RawMessage `json:"accounts"`
}

var utf8BOM = []byte("\xef\xbb\xbf")

// Parse validates and normalises a settings document. The returned error is an *Error.
func Parse(data []byte, opts Options) (*Config, error) {
	if opts.LookupEnv == nil {
		opts.LookupEnv = os.LookupEnv
	}
	// Why: Windows editors save UTF-8 with a byte order mark, which encoding/json rejects.
	data = bytes.TrimPrefix(data, utf8BOM)

	c := &collector{}
	file := c.at("file")

	var probe interface{}
	if err := json.Unmarshal(data, &probe); err != nil {
		file.add("", "%s", describeSyntax(data, err))
		return nil, c.err()
	}
	if obj, ok := asObject(data); ok {
		if _, versioned := obj["version"]; !versioned && isLegacy(obj) {
			return parseLegacy(data, obj, opts, c)
		}
	}
	var top fileShape
	if !decodeSection(data, &top, file) {
		return nil, c.err()
	}
	if top.Version != Version {
		file.add("version", "must be %d, got %d (a file without version is read as fleet.config.json only when it has wsURL, httpBase, sharedPassword or loginDelaySeconds)", Version, top.Version)
		return nil, c.err()
	}

	g := defaultGlobal()
	gr := c.at("global")
	var creds credentials
	credsOK := false
	if decodeSection(top.Global, &g, gr) {
		if opts.Profile != "" {
			g.Server.Profile = opts.Profile
		}
		g.normalise(gr, opts.BaseDir, opts.LookupEnv)
		creds, credsOK = loadCredentials(g.Login.CredentialsFile, gr)
	}

	before := len(c.list)
	dr := c.at("defaults")
	def := defaultAccount()
	if decodeSection(top.Defaults, &def, dr) {
		if hasKey(top.Defaults, "username") {
			dr.add("username", "is set per account, not in defaults")
		}
		def.validate(dr, false)
	}
	if len(c.list) > before {
		// Why: a bad default would otherwise be reported again under every account inheriting it.
		return nil, c.err()
	}

	if len(top.Accounts) == 0 {
		file.add("accounts", "list at least one account")
	}
	cfg := &Config{}
	seen := make(map[uint64]string, len(top.Accounts))
	for i, raw := range top.Accounts {
		where := accountWhere(i, peekUsername(raw))
		ar := c.at(where)
		acc := defaultAccount()
		if !checkShape(raw, reflectAccount, "", ar) {
			continue
		}
		if !decodeSection(mergeAccount(top.Defaults, raw), &acc, ar) {
			continue
		}
		acc.validate(ar, true)
		noteDuplicate(ar, seen, acc.Username, where)
		if acc.Enabled && credsOK {
			acc.resolvePassword(ar, opts.LookupEnv, creds)
		}
		acc.finish(g.Log.Level)
		cfg.Accounts = append(cfg.Accounts, acc)
	}

	if err := c.err(); err != nil {
		return nil, err
	}
	cfg.Global = g
	return cfg, nil
}

func accountWhere(i int, username string) string {
	where := fmt.Sprintf("accounts[%d]", i)
	if username != "" {
		where += fmt.Sprintf(" %q", username)
	}
	return where
}

// noteDuplicate reports an account whose username the server would treat as an earlier one.
func noteDuplicate(r reporter, seen map[uint64]string, username, where string) {
	if checkUsername(username) != "" {
		return
	}
	h := userhash(username)
	if prev, dup := seen[h]; dup {
		r.add("username", "names the same account as %s", prev)
		return
	}
	seen[h] = where
}

func (a *Account) finish(level string) {
	if s := bytes.TrimSpace(a.Settings); len(s) == 0 || string(s) == "null" {
		a.Settings = json.RawMessage("{}")
	}
	if a.LogLevel == "" {
		a.LogLevel = level
	}
}

// legacyFile is the fleet.config.json shape cmd/fleet/main.go reads (FleetConfig there).
type legacyFile struct {
	WSURL             string          `json:"wsURL"`
	HTTPBase          string          `json:"httpBase"`
	SharedPassword    string          `json:"sharedPassword"`
	LoginDelaySeconds float64         `json:"loginDelaySeconds"`
	Accounts          []legacyAccount `json:"accounts"`
}

type legacyAccount struct {
	Username string          `json:"username"`
	Script   string          `json:"script"`
	Settings json.RawMessage `json:"settings"`
}

var (
	legacyMarkers     = []string{"wsURL", "httpBase", "sharedPassword", "loginDelaySeconds"}
	legacyKeys        = map[string]bool{"wsURL": true, "httpBase": true, "sharedPassword": true, "loginDelaySeconds": true, "accounts": true}
	legacyAccountKeys = map[string]bool{"username": true, "script": true, "settings": true}
)

func isLegacy(obj map[string]json.RawMessage) bool {
	for _, k := range legacyMarkers {
		if _, ok := obj[k]; ok {
			return true
		}
	}
	return false
}

// parseLegacy reads the fleet.config.json format so an existing roster keeps working.
// Why: cmd/fleet decoded it leniently, so unknown keys and null values are warnings or ignored
// here rather than errors; everything else is validated like the version 1 format.
func parseLegacy(data []byte, top map[string]json.RawMessage, opts Options, c *collector) (*Config, error) {
	file := c.at("file")
	var lf legacyFile
	if err := json.Unmarshal(data, &lf); err != nil {
		var te *json.UnmarshalTypeError
		if errors.As(err, &te) {
			file.add(te.Field, "want %s, got %s", describeType(te.Type), te.Value)
		} else {
			file.add("", "%s", err.Error())
		}
		return nil, c.err()
	}

	cfg := &Config{Legacy: true}
	cfg.Warnings = append(cfg.Warnings, "fleet.config.json format: sharedPassword is kept in plaintext in the file; the version 1 format reads passwords from passwordEnv or login.credentialsFile")
	for _, k := range sortedKeys(top) {
		if !legacyKeys[k] {
			cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("unknown key %q ignored", k))
		}
	}
	var rawAccounts []json.RawMessage
	_ = json.Unmarshal(top["accounts"], &rawAccounts)
	for i, raw := range rawAccounts {
		obj, _ := asObject(raw)
		for _, k := range sortedKeys(obj) {
			if !legacyAccountKeys[k] {
				cfg.Warnings = append(cfg.Warnings, fmt.Sprintf("accounts[%d]: unknown key %q ignored", i, k))
			}
		}
	}

	g := defaultGlobal()
	live := g.Server.Profiles[liveProfile]
	if lf.WSURL != "" {
		live.URL = lf.WSURL
	}
	if lf.HTTPBase != "" {
		live.HTTPBase = lf.HTTPBase
	}
	g.Server.Profiles[liveProfile] = live
	if d := lf.LoginDelaySeconds; math.IsNaN(d) || d < 0 || d > 600 {
		file.add("loginDelaySeconds", "must be between 0 and 600, got %g", d)
	} else {
		g.Login.LoginDelay = Duration(d * float64(time.Second))
	}
	if opts.Profile != "" {
		g.Server.Profile = opts.Profile
	}
	g.normalise(c.at("global"), opts.BaseDir, opts.LookupEnv)

	pw := lf.SharedPassword
	passwordOK := false
	switch {
	case pw == "":
		file.add("sharedPassword", "is required in the fleet.config.json format")
	case len(pw) > 20:
		file.add("sharedPassword", "must be at most 20 characters, got %d", len(pw))
	case !printableASCII(pw):
		file.add("sharedPassword", "must be printable ASCII")
	default:
		passwordOK = true
	}

	if len(lf.Accounts) == 0 {
		file.add("accounts", "list at least one account")
	}
	seen := make(map[uint64]string, len(lf.Accounts))
	for i, la := range lf.Accounts {
		where := accountWhere(i, la.Username)
		ar := c.at(where)
		acc := defaultAccount()
		acc.Username = la.Username
		acc.Script = la.Script
		if s := bytes.TrimSpace(la.Settings); string(s) != "null" {
			acc.Settings = la.Settings
		}
		acc.validate(ar, true)
		noteDuplicate(ar, seen, acc.Username, where)
		if passwordOK {
			acc.Password = Secret(pw)
		}
		acc.finish(g.Log.Level)
		cfg.Accounts = append(cfg.Accounts, acc)
	}

	if err := c.err(); err != nil {
		return nil, err
	}
	cfg.Global = g
	return cfg, nil
}

func printableASCII(s string) bool {
	for i := 0; i < len(s); i++ {
		if s[i] < 0x20 || s[i] > 0x7e {
			return false
		}
	}
	return true
}

func describeSyntax(data []byte, err error) string {
	var se *json.SyntaxError
	if errors.As(err, &se) {
		line, col := position(data, se.Offset)
		return fmt.Sprintf("invalid JSON at line %d, column %d: %s", line, col, se.Error())
	}
	return err.Error()
}

// position turns a SyntaxError offset, which counts the bytes read including the bad one, into a
// 1-based line and column.
func position(data []byte, offset int64) (int, int) {
	line, col := 1, 1
	for i := int64(0); i < offset-1 && i < int64(len(data)); i++ {
		if data[i] == '\n' {
			line++
			col = 1
		} else {
			col++
		}
	}
	return line, col
}

func resolvePath(base, p string) string {
	if p == "" || filepath.IsAbs(p) {
		return p
	}
	return filepath.Join(base, p)
}

// checkFile resolves p against base and reports it unless it names a readable regular file.
func checkFile(r reporter, field, base, p string) string {
	if p == "" {
		return ""
	}
	full := resolvePath(base, p)
	st, err := os.Stat(full)
	if err != nil {
		r.add(field, "%s", err.Error())
		return full
	}
	if st.IsDir() {
		r.add(field, "%s is a directory, want a file", full)
	}
	return full
}
