package questevidence

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"rsb0t-go/internal/scripts/questplan"
)

// StoreFileName is the quest side store's file name beside an account's checkpoint
// (questrunner.StoreFileName; a test holds the two equal, so this package does not link the engine).
const StoreFileName = "ScarabSwarmQuests.json"

// ReadStores reads every account's quest side store under dir (<dir>/<account>/ScarabSwarmQuests.json).
// An account with no store is skipped; one that will not decode is returned in bad and left alone: this
// reads, it never renames a file aside the way the engine's loader does.
func ReadStores(dir string) (stores []StoreFile, bad []string, err error) {
	ents, err := os.ReadDir(dir)
	if err != nil {
		return nil, nil, err
	}
	for _, e := range ents {
		if !e.IsDir() {
			continue
		}
		path := filepath.Join(dir, e.Name(), StoreFileName)
		data, rerr := os.ReadFile(path)
		if rerr != nil {
			if !os.IsNotExist(rerr) {
				bad = append(bad, fmt.Sprintf("%s: %v", path, rerr))
			}
			continue
		}
		s, derr := questplan.DecodeStore(data)
		if derr != nil {
			bad = append(bad, fmt.Sprintf("%s: %v", path, derr))
			continue
		}
		stores = append(stores, StoreFile{Account: e.Name(), Store: s})
	}
	sort.Slice(stores, func(i, j int) bool { return stores[i].Account < stores[j].Account })
	return stores, bad, nil
}
