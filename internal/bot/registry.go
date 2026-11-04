package bot

import "sync"

var (
	registryMu sync.RWMutex
	registry   = map[string]func() Bot{}
)

// Register adds a script factory under name, so fleet/dashboard code can start it by name
// without importing the script package directly. Intended to be called from an init() func
// in a small adapter file (see barbarian.go) — panics on a duplicate name since that's always
// a programming mistake (two scripts silently fighting over one registration), not a runtime
// condition callers need to recover from.
func Register(name string, factory func() Bot) {
	registryMu.Lock()
	defer registryMu.Unlock()
	if _, exists := registry[name]; exists {
		panic("bot: script already registered: " + name)
	}
	registry[name] = factory
}

// New builds a fresh Bot instance for a registered script name.
func New(name string) (Bot, bool) {
	registryMu.RLock()
	factory, ok := registry[name]
	registryMu.RUnlock()
	if !ok {
		return nil, false
	}
	return factory(), true
}

// Names lists every registered script name, for a dashboard's script picker.
func Names() []string {
	registryMu.RLock()
	defer registryMu.RUnlock()
	out := make([]string, 0, len(registry))
	for name := range registry {
		out = append(out, name)
	}
	return out
}
