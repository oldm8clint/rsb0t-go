// Bank persistence: the account owner's explicit ask — "have it save from last logout/
// script end" — so the dashboard's bank panel keeps showing an account's last-known
// bank contents after it disconnects or its bot stops, rather than the data vanishing
// the instant AccountStatus.Connected goes false (which is what happened before this
// file existed: BankItems/BankUpdatedAt were only ever computed from a live *client.
// Session in Snapshot, so a disconnected account reported nothing).
//
// Two layers, both driven from the same bankCache:
//  1. In-process memory — survives a bot stopping or an account disconnecting while the
//     fleet process keeps running (the account struct itself stays in Manager.accounts
//     either way; only its session goes nil). This layer always exists, no config needed.
//  2. An optional JSON file (Config.BankStateFile, empty disables it — same convention
//     as Config.LayoutFile in internal/controlpanel, which the account owner pointed at
//     directly as "might be a reusable pattern") — survives a full process restart too.
//     Uses the identical write-beside-then-rename technique internal/controlpanel's own
//     layoutStore.save uses, so a crash mid-write never corrupts the file.
package fleet

import (
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// bankRecord is one account's last-known bank, JSON-friendly.
type bankRecord struct {
	Items     []SlotStatus `json:"items"`
	UpdatedAt string       `json:"updatedAt"` // RFC3339
}

// bankStore holds bankRecord per username, optionally persisted to a file.
type bankStore struct {
	mu     sync.Mutex
	saveMu sync.Mutex
	dirty  bool
	path   string // empty: memory only
	data   map[string]bankRecord
}

func newBankStore(path string) *bankStore {
	s := &bankStore{path: path, data: make(map[string]bankRecord)}
	if path == "" {
		return s
	}
	f, err := os.Open(path)
	if err != nil {
		// Nothing saved yet (or unreadable) — start empty rather than failing
		// Manager construction over a missing/corrupt cache file.
		return s
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, 8<<20))
	if err != nil {
		return s
	}
	var loaded map[string]bankRecord
	if err := json.Unmarshal(data, &loaded); err == nil {
		s.data = loaded
	}
	return s
}

// get returns username's cached bank, if any.
func (s *bankStore) get(username string) (bankRecord, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	rec, ok := s.data[username]
	return rec, ok
}

// set records username's bank and, when a file is configured, persists the whole map
// (small — one small record per account, not worth a smarter incremental format).
// Best-effort: a write failure is silently dropped rather than propagated, since losing
// this cache is never worse than the pre-existing behavior (no persistence at all), and
// Snapshot (the only caller) has no good way to surface an error here anyway.
func (s *bankStore) set(username string, rec bankRecord) {
	s.saveMu.Lock()
	defer s.saveMu.Unlock()
	s.mu.Lock()
	old, exists := s.data[username]
	same := exists && old.UpdatedAt == rec.UpdatedAt && len(old.Items) == len(rec.Items)
	if same {
		for i := range old.Items {
			if old.Items[i] != rec.Items[i] {
				same = false
				break
			}
		}
	}
	if same && !s.dirty {
		s.mu.Unlock()
		return
	}
	s.data[username] = rec
	path := s.path
	var snapshot map[string]bankRecord
	if path != "" {
		snapshot = make(map[string]bankRecord, len(s.data))
		for k, v := range s.data {
			snapshot[k] = v
		}
	}
	s.mu.Unlock()
	if path == "" {
		return
	}
	err := saveBankFile(path, snapshot)
	s.mu.Lock()
	s.dirty = err != nil
	s.mu.Unlock()
}

// saveBankFile writes beside path and renames over it, so a crash mid-write leaves the
// previous file intact — the same technique internal/controlpanel's layoutStore.save
// uses for the panel layout file.
func saveBankFile(path string, snapshot map[string]bankRecord) error {
	data, err := json.MarshalIndent(snapshot, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	dir := filepath.Dir(path)
	if dir != "." {
		if err := os.MkdirAll(dir, 0o700); err != nil {
			return err
		}
	}
	tmp, err := os.CreateTemp(dir, ".bankstate-*.tmp")
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
	}
	return err
}

// rememberBank updates the bank cache for username with freshly-observed items,
// stamped with now — called from Snapshot whenever a live session actually reports
// bank contents, so the cache always reflects the most recent real observation.
func (m *Manager) rememberBank(username string, items []SlotStatus, updatedAt string) {
	if len(items) == 0 {
		return
	}
	if updatedAt == "" {
		updatedAt = time.Now().Format(time.RFC3339)
	}
	m.bank.set(username, bankRecord{Items: items, UpdatedAt: updatedAt})
}

// cachedBank returns username's last-persisted bank, if any — the fallback Snapshot
// uses when a session is nil (disconnected) or simply hasn't opened its bank yet this
// run (a fresh reconnect, before StartBot's script gets around to banking again).
func (m *Manager) cachedBank(username string) (bankRecord, bool) {
	return m.bank.get(username)
}
