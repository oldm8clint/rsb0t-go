package controlpanel

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sync"
	"unicode"
	"unicode/utf8"
)

// Layout limits. web/layout.js holds the same numbers; the two must agree.
const (
	LayoutVersion    = 1
	GridColumns      = 12
	GridMaxRows      = 48
	MaxPanels        = 32
	MaxLayoutBytes   = 64 << 10
	MaxSettingsBytes = 4 << 10
	MaxPanelAccounts = 256
	maxTitleRunes    = 48
)

// Layout is the page: which panels, where, and how each is set up.
type Layout struct {
	V      int     `json:"v"`
	Panels []Panel `json:"panels"`
}

// Panel is one panel instance. Type names a module in web/panels; the server checks only its shape,
// so a layout saved by a newer build that knows more types still loads.
type Panel struct {
	ID    string `json:"id"`
	Type  string `json:"type"`
	Title string `json:"title"`
	// Col and Row are the top-left cell from zero; W and H are cells across and down.
	Col int `json:"col"`
	Row int `json:"row"`
	W   int `json:"w"`
	H   int `json:"h"`
	// Accounts is who the panel shows. Empty means every account.
	Accounts []string `json:"accounts"`
	// Settings is the panel type's own record, a JSON object the browser normalises.
	Settings json.RawMessage `json:"settings"`
}

var (
	panelIDPattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,23}$`)
	panelTypePattern = regexp.MustCompile(`^[a-z][a-z0-9-]{0,23}$`)
)

// ParseLayout decodes and validates a layout. Unknown fields, trailing data and anything out of
// bounds are errors: the server refuses a bad layout rather than guessing what it meant.
func ParseLayout(data []byte) (Layout, error) {
	if len(data) > MaxLayoutBytes {
		return Layout{}, fmt.Errorf("layout is %d bytes, over the %d byte limit", len(data), MaxLayoutBytes)
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var l Layout
	if err := dec.Decode(&l); err != nil {
		return Layout{}, fmt.Errorf("layout is not valid: %w", err)
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		return Layout{}, errors.New("layout has data after its closing brace")
	}
	if err := l.Validate(); err != nil {
		return Layout{}, err
	}
	return l, nil
}

// Validate checks every bound and normalises in place: nil lists become empty, settings are compacted.
func (l *Layout) Validate() error {
	if l.V != LayoutVersion {
		return fmt.Errorf("layout version is %d, want %d", l.V, LayoutVersion)
	}
	if len(l.Panels) > MaxPanels {
		return fmt.Errorf("layout has %d panels, over the limit of %d", len(l.Panels), MaxPanels)
	}
	if l.Panels == nil {
		l.Panels = []Panel{}
	}
	seen := make(map[string]bool, len(l.Panels))
	for i := range l.Panels {
		p := &l.Panels[i]
		if !panelIDPattern.MatchString(p.ID) {
			return fmt.Errorf("panel %d: id %q must be 1 to 24 lower-case letters, digits or dashes", i, p.ID)
		}
		if seen[p.ID] {
			return fmt.Errorf("panel %q appears twice", p.ID)
		}
		seen[p.ID] = true
		if !panelTypePattern.MatchString(p.Type) {
			return fmt.Errorf("panel %q: type %q must be 1 to 24 lower-case letters, digits or dashes", p.ID, p.Type)
		}
		if !cleanText(p.Title) || utf8.RuneCountInString(p.Title) > maxTitleRunes {
			return fmt.Errorf("panel %q: title must be at most %d printable characters", p.ID, maxTitleRunes)
		}
		if p.W < 1 || p.H < 1 || p.Col < 0 || p.Row < 0 || p.Col+p.W > GridColumns || p.Row+p.H > GridMaxRows {
			return fmt.Errorf("panel %q: %dx%d at column %d, row %d is off the %d-column, %d-row grid", p.ID, p.W, p.H, p.Col, p.Row, GridColumns, GridMaxRows)
		}
		for j := 0; j < i; j++ {
			if overlaps(p, &l.Panels[j]) {
				return fmt.Errorf("panel %q overlaps panel %q", p.ID, l.Panels[j].ID)
			}
		}
		if len(p.Accounts) > MaxPanelAccounts {
			return fmt.Errorf("panel %q names %d accounts, over the limit of %d", p.ID, len(p.Accounts), MaxPanelAccounts)
		}
		if p.Accounts == nil {
			p.Accounts = []string{}
		}
		for _, name := range p.Accounts {
			if name == "" || len(name) > maxNameBytes || !cleanText(name) {
				return fmt.Errorf("panel %q: account name %q must be 1 to %d printable bytes", p.ID, name, maxNameBytes)
			}
		}
		raw := bytes.TrimSpace(p.Settings)
		switch {
		case len(raw) == 0 || bytes.Equal(raw, []byte("null")):
			p.Settings = json.RawMessage("{}")
		case raw[0] != '{':
			return fmt.Errorf("panel %q: settings must be a JSON object", p.ID)
		default:
			var compact bytes.Buffer
			if err := json.Compact(&compact, raw); err != nil {
				return fmt.Errorf("panel %q: settings: %w", p.ID, err)
			}
			if compact.Len() > MaxSettingsBytes {
				return fmt.Errorf("panel %q: settings are %d bytes, over the %d byte limit", p.ID, compact.Len(), MaxSettingsBytes)
			}
			p.Settings = compact.Bytes()
		}
	}
	return nil
}

func overlaps(a, b *Panel) bool {
	return a.Col < b.Col+b.W && b.Col < a.Col+a.W && a.Row < b.Row+b.H && b.Row < a.Row+a.H
}

func cleanText(s string) bool {
	if !utf8.ValidString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsControl(r) {
			return false
		}
	}
	return true
}

// layoutStore reads and writes the layout file. It keeps nothing in memory between requests.
type layoutStore struct {
	path string
	mu   sync.Mutex
}

func (s *layoutStore) persisted() bool { return s.path != "" }

// load returns nil with no error when nothing has been saved yet.
func (s *layoutStore) load() (*Layout, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	f, err := os.Open(s.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, MaxLayoutBytes+1))
	if err != nil {
		return nil, err
	}
	l, err := ParseLayout(data)
	if err != nil {
		return nil, err
	}
	return &l, nil
}

// save writes beside the file and renames over it, so a crash mid-write leaves the old layout.
func (s *layoutStore) save(l Layout) error {
	data, err := json.MarshalIndent(l, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	s.mu.Lock()
	defer s.mu.Unlock()
	dir := filepath.Dir(s.path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".dashboard-layout-*.tmp")
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
		err = os.Rename(name, s.path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
