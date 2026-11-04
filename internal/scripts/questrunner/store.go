package questrunner

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/questplan"
)

// The side store's file half. AIO-PLAN A2: the store lives beside the ScarabSwarm checkpoint
// (data/scarabswarm-checkpoints/<acct>/), decodes leniently, and a file that will not decode is
// kept as <name>.bad-<ts>, never overwritten silently.

// StoreFileName is the quest store's file name beside the checkpoint. It is NOT
// ScarabSwarmStage5.json, which is the Stage 5 ladder framework's own store (AIO-PLAN A6, a
// later lane): the quest engine's counters live in their own file so the two can land in either
// order without sharing a schema.
const StoreFileName = "ScarabSwarmQuests.json"

// StorePath is the quest store's path for an account under dir (the checkpoint directory).
func StorePath(dir, account string) (string, error) {
	acct := strings.ReplaceAll(strings.TrimSpace(account), " ", "_")
	if acct == "" || acct == "." || acct == ".." || strings.ContainsAny(acct, `/\:`) {
		return "", fmt.Errorf("questrunner: %q is not a usable account name for a store path", account)
	}
	return filepath.Join(dir, acct, StoreFileName), nil
}

// LoadStore reads the store at path. A missing file is an empty store. A file that does not
// decode is renamed to <path>.bad-<unix> and an empty store is returned WITH the error, so the
// caller logs it; the bad file is never overwritten.
func LoadStore(path string) (*questplan.Store, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return questplan.NewStore(), nil
	}
	if err != nil {
		return nil, err
	}
	s, derr := questplan.DecodeStore(data)
	if derr == nil {
		return s, nil
	}
	bad := fmt.Sprintf("%s.bad-%d", path, time.Now().Unix())
	if rerr := os.Rename(path, bad); rerr != nil {
		return nil, fmt.Errorf("%v; and keeping it aside as %s failed: %w", derr, bad, rerr)
	}
	return questplan.NewStore(), fmt.Errorf("%v; the unreadable file was kept as %s", derr, bad)
}

// SaveStore writes the store atomically (temp file, sync, rename).
func SaveStore(path string, s *questplan.Store) error {
	data, err := s.Encode()
	if err != nil {
		return err
	}
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".quests-*.tmp")
	if err != nil {
		return err
	}
	name := tmp.Name()
	_, err = tmp.Write(data)
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err == nil {
		err = os.Rename(name, path)
	}
	if err != nil {
		_ = os.Remove(name)
	}
	return err
}
