package quests

import (
	"errors"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/gamestate"
)

// JournalScroll is questjournal_scroll (content pack/interface.pack 8134), the modal the
// quest_journal proc fills: the quest name in com 8144 and up to 50 lines in qj1..qj50. Journal
// reads whichever main modal opens, as upstream does, so a quest with its own journal layout
// works too.
const JournalScroll = 8134

// journalOpenTimeout is upstream Quests.journal's wait for the modal.
const journalOpenTimeout = 5 * time.Second

// ErrUnknownQuest is returned by Journal for a name the quest table does not have.
var ErrUnknownQuest = errors.New("quests: no quest tab row with that name")

// Journal ports Quests.journal: click the quest's row on the quest tab (IF_BUTTON on its
// questlist component), wait up to 5 s for a different main modal to open, and return that
// modal's text lines (actions.MainModalTexts). Upstream's caveat applies: it opens a modal, so
// prefer an item or chat message as proof of progress when one exists. A nil Waiter polls.
func Journal(s actions.Session, components archive.Components, name string, w actions.Waiter) ([]string, error) {
	comID, ok := ComponentID(name)
	if !ok {
		return nil, ErrUnknownQuest
	}
	st := s.State()
	before, _, _ := st.Modals()
	if err := actions.IfButton(s, comID); err != nil {
		return nil, err
	}
	opened := func() bool {
		main, _, _ := st.Modals()
		return main != gamestate.NoModal && main != before
	}
	if !waitUntil(w, opened, journalOpenTimeout) {
		return nil, actions.ErrNotConfirmed
	}
	return actions.MainModalTexts(s, components), nil
}

func waitUntil(w actions.Waiter, cond func() bool, timeout time.Duration) bool {
	if w != nil {
		return w.DelayUntil(cond, timeout)
	}
	deadline := time.Now().Add(timeout)
	for !cond() {
		if !time.Now().Before(deadline) {
			return false
		}
		time.Sleep(50 * time.Millisecond)
	}
	return true
}
