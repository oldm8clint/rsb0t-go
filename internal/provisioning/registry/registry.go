package registry

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sort"
	"sync"
	"time"
)

type Status string

const (
	Pending        Status = "pending"
	Authenticating Status = "authenticating"
	Ready          Status = "ready"
	Assigned       Status = "assigned"
	Running        Status = "running"
	Stopped        Status = "stopped"
	Disabled       Status = "disabled"
)

type Account struct {
	ID                 string    `json:"id"`
	Provider           string    `json:"provider"`
	DiscordIdentityRef string    `json:"discord_identity_ref"`
	Status             Status    `json:"status"`
	Script             string    `json:"script,omitempty"`
	NodeID             string    `json:"node_id,omitempty"`
	SecretRef          string    `json:"secret_ref"`
	CreatedAt          time.Time `json:"created_at"`
	UpdatedAt          time.Time `json:"updated_at"`
}

var ErrNotFound = errors.New("account not found")
var ErrInvalidTransition = errors.New("invalid account state transition")

type Store interface {
	Create(Account) error
	Get(string) (Account, error)
	List() []Account
	Update(Account) error
}

type MemoryStore struct {
	mu       sync.RWMutex
	accounts map[string]Account
}

func NewMemoryStore() *MemoryStore { return &MemoryStore{accounts: make(map[string]Account)} }
func (s *MemoryStore) Create(a Account) error {
	if a.ID == "" {
		return fmt.Errorf("account id is required")
	}
	if a.Status == "" {
		a.Status = Pending
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[a.ID]; ok {
		return fmt.Errorf("account %q already exists", a.ID)
	}
	s.accounts[a.ID] = a
	return nil
}
func (s *MemoryStore) Get(id string) (Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[id]
	if !ok {
		return Account{}, ErrNotFound
	}
	return a, nil
}
func (s *MemoryStore) List() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *MemoryStore) Update(a Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[a.ID]; !ok {
		return ErrNotFound
	}
	a.UpdatedAt = time.Now().UTC()
	s.accounts[a.ID] = a
	return nil
}

// JSONStore persists only lifecycle metadata. Secrets remain external via SecretRef.
type JSONStore struct {
	mu       sync.RWMutex
	path     string
	accounts map[string]Account
}

func OpenJSONStore(path string) (*JSONStore, error) {
	s := &JSONStore{path: path, accounts: make(map[string]Account)}
	data, err := os.ReadFile(path)
	if errors.Is(err, os.ErrNotExist) {
		return s, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) != 0 {
		if err := json.Unmarshal(data, &s.accounts); err != nil {
			return nil, fmt.Errorf("decode registry: %w", err)
		}
	}
	return s, nil
}
func (s *JSONStore) persistLocked() error {
	data, err := json.MarshalIndent(s.accounts, "", "  ")
	if err != nil {
		return err
	}
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, data, 0600); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}
func (s *JSONStore) Create(a Account) error {
	if a.ID == "" {
		return fmt.Errorf("account id is required")
	}
	if a.Status == "" {
		a.Status = Pending
	}
	now := time.Now().UTC()
	if a.CreatedAt.IsZero() {
		a.CreatedAt = now
	}
	a.UpdatedAt = now
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[a.ID]; ok {
		return fmt.Errorf("account %q already exists", a.ID)
	}
	s.accounts[a.ID] = a
	return s.persistLocked()
}
func (s *JSONStore) Get(id string) (Account, error) {
	s.mu.RLock()
	defer s.mu.RUnlock()
	a, ok := s.accounts[id]
	if !ok {
		return Account{}, ErrNotFound
	}
	return a, nil
}
func (s *JSONStore) List() []Account {
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]Account, 0, len(s.accounts))
	for _, a := range s.accounts {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
func (s *JSONStore) Update(a Account) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.accounts[a.ID]; !ok {
		return ErrNotFound
	}
	a.UpdatedAt = time.Now().UTC()
	s.accounts[a.ID] = a
	return s.persistLocked()
}

func CanTransition(from, to Status) bool {
	switch from {
	case Pending:
		return to == Authenticating || to == Disabled
	case Authenticating:
		return to == Ready || to == Disabled || to == Pending
	case Ready:
		return to == Assigned || to == Disabled
	case Assigned:
		return to == Running || to == Ready || to == Disabled
	case Running:
		return to == Stopped || to == Ready || to == Disabled
	case Stopped:
		return to == Ready || to == Disabled
	case Disabled:
		return false
	default:
		return false
	}
}
func Transition(s Store, id string, to Status) (Account, error) {
	a, err := s.Get(id)
	if err != nil {
		return Account{}, err
	}
	if !CanTransition(a.Status, to) {
		return Account{}, fmt.Errorf("%w: %s -> %s", ErrInvalidTransition, a.Status, to)
	}
	a.Status = to
	if err := s.Update(a); err != nil {
		return Account{}, err
	}
	return a, nil
}
