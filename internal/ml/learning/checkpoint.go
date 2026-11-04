package learning

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type checkpoint struct {
	Version int                  `json:"version"`
	Alpha   float32              `json:"alpha"`
	Gamma   float32              `json:"gamma"`
	Values  map[string][]float32 `json:"values"`
}

// Save writes a QTable checkpoint atomically. Checkpoints contain learned
// values and hyperparameters only; they never contain account credentials or
// live client state.
func (q *QTable) Save(path string) error {
	if q == nil {
		return fmt.Errorf("learning: nil QTable")
	}
	cp := checkpoint{Version: 1, Alpha: q.alpha, Gamma: q.gamma, Values: make(map[string][]float32, len(q.values))}
	for k, v := range q.values {
		cp.Values[k] = append([]float32(nil), v...)
	}
	data, err := json.Marshal(cp)
	if err != nil {
		return fmt.Errorf("learning: encode checkpoint: %w", err)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return fmt.Errorf("learning: create checkpoint directory: %w", err)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".qtable-*.tmp")
	if err != nil {
		return fmt.Errorf("learning: create checkpoint temp: %w", err)
	}
	tmpName := tmp.Name()
	ok := false
	defer func() {
		_ = tmp.Close()
		if !ok {
			_ = os.Remove(tmpName)
		}
	}()
	if _, err := tmp.Write(data); err != nil {
		return fmt.Errorf("learning: write checkpoint: %w", err)
	}
	if err := tmp.Sync(); err != nil {
		return fmt.Errorf("learning: sync checkpoint: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("learning: close checkpoint: %w", err)
	}
	if err := os.Rename(tmpName, path); err != nil {
		return fmt.Errorf("learning: replace checkpoint: %w", err)
	}
	ok = true
	return nil
}

// Load restores a checkpoint produced by Save. Unknown versions are rejected
// so future format changes cannot silently corrupt an experiment.
func Load(path string, seed int64) (*QTable, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("learning: read checkpoint: %w", err)
	}
	var cp checkpoint
	if err := json.Unmarshal(data, &cp); err != nil {
		return nil, fmt.Errorf("learning: decode checkpoint: %w", err)
	}
	if cp.Version != 1 {
		return nil, fmt.Errorf("learning: unsupported checkpoint version %d", cp.Version)
	}
	q := NewQTable(seed, cp.Alpha, cp.Gamma)
	q.values = make(map[string][]float32, len(cp.Values))
	for k, v := range cp.Values {
		if len(v) != len(actions) {
			return nil, fmt.Errorf("learning: state %q has %d actions, want %d", k, len(v), len(actions))
		}
		q.values[k] = append([]float32(nil), v...)
	}
	return q, nil
}
