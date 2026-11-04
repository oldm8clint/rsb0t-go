package settings

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

// MaxFileBytes caps how large one settings file this package reads may be. A script's settings
// are a handful of scalars and short lists (MuleConfig, the largest example, is under 1 KiB
// marshaled); anything past this is almost certainly a mistake, not a legitimate settings file.
const MaxFileBytes = 256 << 10

// namePattern is what an account username or script id must look like to become a path
// component: this is the same guard against path traversal and odd filesystem characters that
// internal/controlpanel's panel id/type patterns use for the same reason.
var namePattern = regexp.MustCompile(`^[A-Za-z0-9_.-]{1,64}$`)

// Store reads and writes settings files under Dir, one per account+script pair at
// Dir/<account>/<script>.json. Callers hold onto the Bag a Load returns for as long as they need
// it; the Store only remembers what it last wrote to each file, so Save can skip a write that
// would change nothing.
type Store struct {
	Dir string

	mu      sync.Mutex
	written map[string]writtenFile
}

// writtenFile is what Save last put in one file, and the file's size and time right after.
type writtenFile struct {
	data    []byte
	size    int64
	modTime time.Time
}

// NewStore is a Store rooted at dir. dir need not exist yet; Save creates it.
func NewStore(dir string) *Store {
	return &Store{Dir: dir}
}

// pathName turns an account or script name into a safe path component.
//
// A SPACE BECOMES AN UNDERSCORE, and that is not a fudge — it is the game's own identity rule.
// CLAUDE.md records that usernames are compared by userhash, so "Bob_Smith" and "bob smith"
// are THE SAME ACCOUNT to the server. Mapping one to the other therefore cannot merge two
// distinct accounts' settings, because two names that differ only by that substitution are
// not two accounts.
//
// WHY IT MATTERS, measured on the live fleet 2026-09-22: thirteen roster accounts have spaces
// in their names, and every one of them failed EVERY settings write with
// `settings: account "acct" must be 1-64 letters, digits, '.', '_' or '-'` — 373 and
// 325 failures for two accounts alone in a single run. ScarabSwarm persists its stage
// checkpoint through this Store, so those accounts could never save a checkpoint and silently
// restarted from Stage 1 on every restart, forever.
//
// The traversal guard is unchanged: the pattern below still rejects "/", "\", ".." and
// anything else odd, and it is applied AFTER this substitution, not instead of it.
func pathName(s string) string { return strings.ReplaceAll(s, " ", "_") }

// isDotName reports whether a name is "." or ".." — the two path components that mean
// "here" and "one level up" rather than a directory of their own.
func isDotName(s string) bool { return s == "." || s == ".." }

// Path is the file account+script's settings live at.
func (s *Store) Path(account, script string) (string, error) {
	account, script = pathName(account), pathName(script)
	// "." and ".." match namePattern (it allows dots), so the traversal guard it was written
	// to be had a hole in it: Path("..") returned Dir/../<script>.json, one level ABOVE the
	// store. Pre-existing, found 2026-09-22 by the test added for the spaces fix. Rejected
	// explicitly rather than by tightening the pattern, because ordinary names legitimately
	// contain dots.
	if isDotName(account) || isDotName(script) {
		return "", fmt.Errorf("settings: %q is not a usable name", account+"/"+script)
	}
	if !namePattern.MatchString(account) {
		return "", fmt.Errorf("settings: account %q must be 1-64 letters, digits, '.', '_' or '-'", account)
	}
	if !namePattern.MatchString(script) {
		return "", fmt.Errorf("settings: script %q must be 1-64 letters, digits, '.', '_' or '-'", script)
	}
	return filepath.Join(s.Dir, account, script+".json"), nil
}

// Load reads account+script's settings file into a Bag. A file that does not exist yet returns
// an empty Bag and no error — first run behaves like "nothing saved," and the caller's own
// defaults (e.g. mulereceiverplan.MuleDefaults) apply until something is saved.
func (s *Store) Load(account, script string) (*Bag, error) {
	path, err := s.Path(account, script)
	if err != nil {
		return nil, err
	}
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return NewBag(), nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxFileBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > MaxFileBytes {
		return nil, fmt.Errorf("settings: %s is over the %d byte limit", path, MaxFileBytes)
	}
	return ParseBag(data)
}

// Save writes bag to account+script's settings file, atomically: a temp file in the same
// directory is written and synced, then renamed over the target, so a crash mid-write leaves the
// previous file (or no file) intact rather than a half-written one. The directory is created if
// it does not exist yet.
//
// A Save whose bytes match what this Store last wrote to a file that has not changed since
// (same size and modification time) is skipped: ScarabSwarm saves every account's checkpoint
// every 15 seconds, and on the live fleet rewriting and syncing hundreds of unchanged files was
// a steady few percent of the control panel's CPU, plus a virus scan of each new file.
func (s *Store) Save(account, script string, bag *Bag) error {
	path, err := s.Path(account, script)
	if err != nil {
		return err
	}
	data, err := bag.MarshalIndent()
	if err != nil {
		return err
	}
	if s.unchanged(path, data) {
		return nil
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".settings-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if closeErr := tmp.Close(); err == nil {
		err = closeErr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
		s.forget(path)
		return err
	}
	s.remember(path, data)
	return nil
}

func (s *Store) unchanged(path string, data []byte) bool {
	s.mu.Lock()
	w, ok := s.written[path]
	s.mu.Unlock()
	if !ok || !bytes.Equal(w.data, data) {
		return false
	}
	fi, err := os.Stat(path)
	return err == nil && fi.Size() == w.size && fi.ModTime().Equal(w.modTime)
}

func (s *Store) remember(path string, data []byte) {
	fi, err := os.Stat(path)
	if err != nil {
		s.forget(path)
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.written == nil {
		s.written = map[string]writtenFile{}
	}
	s.written[path] = writtenFile{data: data, size: fi.Size(), modTime: fi.ModTime()}
}

func (s *Store) forget(path string) {
	s.mu.Lock()
	delete(s.written, path)
	s.mu.Unlock()
}

// Bag is one script's settings for one account: an untyped JSON object with the typed accessors
// reference/server-api-docs.md's SettingsBag documents (Bool/Num/Str/List, each with a fallback
// for an absent or wrong-typed key), plus Decode/Encode to round-trip a caller's own settings
// struct whole. It is safe for concurrent use.
type Bag struct {
	mu     sync.Mutex
	values map[string]interface{}
}

// NewBag is an empty Bag.
func NewBag() *Bag {
	return &Bag{values: map[string]interface{}{}}
}

// ParseBag decodes a JSON object into a Bag. Empty or all-whitespace input is an empty Bag, the
// same as a file that does not exist yet.
func ParseBag(data []byte) (*Bag, error) {
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return NewBag(), nil
	}
	var values map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return nil, fmt.Errorf("settings: %w", err)
	}
	if values == nil {
		values = map[string]interface{}{}
	}
	return &Bag{values: values}, nil
}

// Bool is the value at key as a bool, or fallback when key is absent or not a bool.
func (b *Bag) Bool(key string, fallback bool) bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	if v, ok := b.values[key].(bool); ok {
		return v
	}
	return fallback
}

// Num is the value at key as a float64 (JSON's only number type), or fallback when key is absent
// or not a number.
func (b *Bag) Num(key string, fallback float64) float64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	if v, ok := b.values[key].(float64); ok {
		return v
	}
	return fallback
}

// Int is Num truncated to an int, the shape every *plan package's numeric settings use.
func (b *Bag) Int(key string, fallback int) int {
	return int(b.Num(key, float64(fallback)))
}

// Str is the value at key as a string, or fallback when key is absent or not a string.
func (b *Bag) Str(key string, fallback string) string {
	b.mu.Lock()
	defer b.mu.Unlock()
	if v, ok := b.values[key].(string); ok {
		return v
	}
	return fallback
}

// List is the value at key as a string slice, or fallback when key is absent or not a JSON array
// of strings.
func (b *Bag) List(key string, fallback []string) []string {
	b.mu.Lock()
	defer b.mu.Unlock()
	// Why: []string must be accepted, not just []interface{}. Schema.resolve stores a
	// TypeList value as []string, so a list setting resolved through a schema was previously
	// unreadable here and every caller silently got its fallback — found by the GemCutter
	// port, whose "which gems to cut" setting resolved correctly and then read back empty,
	// quietly widening the run to every gem. A raw JSON-decoded map still arrives as
	// []interface{}, so both shapes are handled.
	switch raw := b.values[key].(type) {
	case []string:
		out := make([]string, len(raw))
		copy(out, raw)
		return out
	case []interface{}:
		out := make([]string, 0, len(raw))
		for _, v := range raw {
			s, ok := v.(string)
			if !ok {
				return fallback
			}
			out = append(out, s)
		}
		return out
	}
	return fallback
}

// Set stores value at key, replacing whatever was there. value must be JSON-marshalable.
func (b *Bag) Set(key string, value interface{}) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.values[key] = value
}

// Delete removes key, if present.
func (b *Bag) Delete(key string) {
	b.mu.Lock()
	defer b.mu.Unlock()
	delete(b.values, key)
}

// Raw is a copy of the Bag's underlying values, e.g. for logging or a generic settings page.
func (b *Bag) Raw() map[string]interface{} {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make(map[string]interface{}, len(b.values))
	for k, v := range b.values {
		out[k] = v
	}
	return out
}

// Decode strictly decodes the Bag into v, the shape a *plan package's own settings struct already
// models (e.g. mulereceiverplan.MuleConfig) — an unknown field is an error, matching
// config.Account.DecodeSettings's behaviour for the process-level settings this package sits on
// top of. Go's default (no json tag) field matching is case-insensitive, so this decodes cleanly
// whether the stored keys are PascalCase (a Go struct's own default marshaling) or the lowerCamel
// keys fleet.config.json's settings objects use.
func (b *Bag) Decode(v interface{}) error {
	b.mu.Lock()
	data, err := json.Marshal(b.values)
	b.mu.Unlock()
	if err != nil {
		return err
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return fmt.Errorf("settings: decode: %w", err)
	}
	return nil
}

// Encode replaces the Bag's values with v's own JSON shape — the save-side counterpart of
// Decode: a script mutates its own typed struct in memory, calls Encode, then Store.Save
// persists it.
func (b *Bag) Encode(v interface{}) error {
	data, err := json.Marshal(v)
	if err != nil {
		return err
	}
	var values map[string]interface{}
	if err := json.Unmarshal(data, &values); err != nil {
		return fmt.Errorf("settings: encode: %w", err)
	}
	if values == nil {
		values = map[string]interface{}{}
	}
	b.mu.Lock()
	b.values = values
	b.mu.Unlock()
	return nil
}

// MarshalIndent is the Bag's JSON form (indented, newline-terminated), what Store.Save writes.
func (b *Bag) MarshalIndent() ([]byte, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	data, err := json.MarshalIndent(b.values, "", "  ")
	if err != nil {
		return nil, err
	}
	return append(data, '\n'), nil
}
