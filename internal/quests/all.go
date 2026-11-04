package quests

import (
	"sort"

	"rsb0t-go/internal/gamestate"
)

// Entry is one quest's name and status, as Quests.all lists them.
type Entry struct {
	Name    string // lower-cased, the key ComponentID and Status take
	Label   string // the quest tab's own spelling, e.g. "Cook's Assistant"
	ComID   int    // the questlist text component, also the journal button
	Members bool
	Status  QuestStatus
}

// All ports Quests.all over the quests this package has quest-tab component ids for (see
// Known), sorted by name. A quest missing from the table is absent here rather than Unknown;
// upstream reads every row off the live quest tab instead.
func All(state *gamestate.State) []Entry {
	names := Known()
	sort.Strings(names)
	out := make([]Entry, 0, len(names))
	for _, name := range names {
		status, _ := Status(state, name)
		row, _ := rowFor(name)
		out = append(out, Entry{Name: name, Label: row.Label, ComID: row.ComID, Members: row.Members, Status: status})
	}
	return out
}
