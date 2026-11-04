package donorplan

// Not asking a mule for what it has just said it does not have.
//
// THE FLEET OWNER'S REPORT, 2026-09-22, verbatim: "mulereciever wasnt updated to include handing
// out of all items in bank if requested from another account, accounts are still requesting items
// the mule does not have". The second half of that sentence is this file. Widening what the mule
// WILL give (orderplan.OpenStock) does nothing about an account asking for something that is
// genuinely not there, and the fleet does that in a loop: a woodcutter walked 83 tiles to
// Catherby, asked four questions about axes that are not in the bank, was told "none in the bank"
// four times, threw all four answers away and did it again two minutes later, at zero experience.
//
// WHAT IS REMEMBERED, and why it is exactly this. A mule's decline is authoritative — orderplan
// .Decide reads its real bank before it says "none in the bank" — but only some declines are
// about the ITEM. The three below are, and all three share the property that makes remembering
// them safe: they are QUANTITY-INDEPENDENT. Decide clamps an over-large order rather than
// refusing it, so none of these can be a "you asked for too many" in disguise, and asking again
// for fewer cannot produce a different answer.
//
// Everything else is deliberately NOT remembered, and the ones that would do real harm are worth
// naming. "you are not in sight of me" is about where the asker was standing and says nothing at
// all about stock (orderplan.DeclineIsPositional; a live cohort already read four positional
// refusals as four scarcity answers and settled for the worst axe as a result). "already handed
// you one recently" is about THIS donor's own per-window cap, not the item — remembering it would
// lock out 268 other accounts over one account's quota. "not on the allow list", "hand-outs are
// switched off" and "cannot read my own stock" are about the mule, not the item.
//
// This is donorplan's general form of the per-bot memory woodcutplan.AxeUnavailable already
// keeps for axes. It lives here because donorrunner.Await is the single choke point every asking
// script goes through — ProgressiveFisher, LootChestFisher, ScarabSwarm, ProgressiveWoodcutter
// and peerrunner all call it — so one memory at that point covers every script and every item,
// where a per-script one covers one.
//
// Pure, like the rest of this package: no client, actions, fleet or bot imports and no I/O, and
// every method takes the moment as an explicit time.Time rather than reading the wall clock —
// the same Go-idiom deviation orderplan.Ledger, mulebus.Roster and mulereceiverplan's own
// collections already make and document.

import (
	"strings"
	"sync"
	"time"
)

// RefusalKind classifies a mule's decline for the purpose of deciding whether asking again soon
// could possibly produce a different answer.
type RefusalKind int

const (
	// RefusalNotAboutTheItem is every decline that says nothing about what the mule holds:
	// positional, per-donor, allow list, hand-outs switched off, a mule that cannot read its
	// own bank, a mechanical failure mid-trade, or a silence. Never remembered.
	RefusalNotAboutTheItem RefusalKind = iota
	// RefusalNoStock is the mule having read its bank and found none: orderplan.Decide's "none
	// in the bank".
	RefusalNoStock
	// RefusalHeldBack is the mule holding some and keeping them: orderplan's batch-floor
	// decline ("only 46 Crystal key banked, holding until 100") and its OpenStock.Never
	// decline (orderplan.DeclineKeptBack). The item exists; policy says it stays.
	RefusalHeldBack
	// RefusalNotOffered is the mule's policy having no room for the item at all — "not an item
	// I hand out" — or its per-window ceiling for it being spent.
	RefusalNotOffered
)

// ClassifyRefusal reads a mule's decline reason.
//
// Matched by lower-cased SUBSTRING rather than equality, for one concrete reason: a reason
// travels as mulebus.DeclinedMessage.Reason and is truncated at mulebus.ReasonMax (80 bytes), so
// the tail of a long one is not guaranteed to arrive. The phrases matched are the leading,
// distinctive parts.
//
// If orderplan's wording ever drifts this returns RefusalNotAboutTheItem and every caller falls
// back to the blind asking it did before. That is a lost optimisation and not a breakage, which
// is why this is a short list of specific phrases rather than a guess at a family of them — the
// same choice woodcutplan.MuleDeclineIsEmptyStock makes, for the same reason.
func ClassifyRefusal(reason string) RefusalKind {
	r := strings.ToLower(strings.TrimSpace(reason))
	switch {
	case r == "":
		return RefusalNotAboutTheItem
	// Checked BEFORE the stock phrases. "you are not in sight of me" contains none of them
	// today, but this ordering is what makes the positional case impossible to get wrong if
	// somebody rewords one later, and reading a positional refusal as a stock answer is the
	// specific mistake this fleet has already made at scale.
	case strings.Contains(r, "not in sight of me"):
		return RefusalNotAboutTheItem
	case strings.Contains(r, "already handed you"):
		return RefusalNotAboutTheItem
	case strings.Contains(r, "cannot read my own stock"):
		return RefusalNotAboutTheItem
	case strings.Contains(r, "none in the bank"):
		return RefusalNoStock
	case strings.Contains(r, "holding until"):
		return RefusalHeldBack
	case strings.Contains(r, "i keep that one back"):
		return RefusalHeldBack
	case strings.Contains(r, "not an item i hand out"):
		return RefusalNotOffered
	case strings.Contains(r, "limit for that item is spent"):
		return RefusalNotOffered
	default:
		return RefusalNotAboutTheItem
	}
}

// AboutTheItem reports whether a refusal of this kind is a statement about what the mule holds
// or will give, rather than about the asker, the mule or the moment.
func (k RefusalKind) AboutTheItem() bool { return k != RefusalNotAboutTheItem }

// RefusalCooldown is how long one of those answers is treated as still true.
//
// orderplan.DefaultWindow (30 minutes), deliberately, and not a number of its own: that is the
// period the mule's own caps are measured over, so a re-ask inside it cannot produce an answer
// the previous one did not already have. It is also exactly what LootChestFisher independently
// arrived at for its own stock probe (internal/bot/lootchestfisher.go's probeRetry) after the
// same reasoning.
//
// Written out rather than importing orderplan, because donorplan is imported BY orderplan's
// callers and deliberately keeps no dependencies; the unit test asserts the two stay equal.
const RefusalCooldown = 30 * time.Minute

type refusal struct {
	until  time.Time
	kind   RefusalKind
	reason string
	at     time.Time
}

// RefusalMemory is what each mule has lately said it cannot or will not give, keyed by mule AND
// item.
//
// Keyed by both, not by item alone: two mules have two banks, and an answer from one says
// nothing about the other. Shared across accounts on purpose, though — a mule's stock is a
// fleet-wide fact, so one account learning "no Mithril axe" should spare the other 268 the walk,
// which is the same premise internal/peerbank is built on.
//
// Safe for concurrent use: in a fleet process every session goroutine reaches the same instance.
// The zero value is usable and behaves as an empty memory.
type RefusalMemory struct {
	mu sync.Mutex
	by map[string]refusal
}

// NewRefusalMemory returns an empty memory. The zero value works too; this exists so a caller
// can be explicit.
func NewRefusalMemory() *RefusalMemory { return &RefusalMemory{} }

func refusalKey(mule, item string) string {
	m := strings.ReplaceAll(strings.ToLower(strings.TrimSpace(mule)), "_", " ")
	return m + "\x00" + strings.ToLower(strings.TrimSpace(item))
}

// Record notes that mule refused item for a reason that is about the item. A reason that is not
// — a positional refusal, a per-donor cap, a silence — is ignored rather than stored, so a caller
// may hand every decline to this without classifying them itself.
//
// Returns whether anything was remembered, for a log line.
func (m *RefusalMemory) Record(now time.Time, mule, item, reason string, cooldown time.Duration) bool {
	if m == nil || mule == "" || item == "" {
		return false
	}
	kind := ClassifyRefusal(reason)
	if !kind.AboutTheItem() {
		return false
	}
	if cooldown <= 0 {
		cooldown = RefusalCooldown
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.by == nil {
		m.by = map[string]refusal{}
	}
	m.by[refusalKey(mule, item)] = refusal{until: now.Add(cooldown), kind: kind, reason: reason, at: now}
	return true
}

// Refused reports whether mule has said no to item recently, and gives back the mule's OWN words
// and how long ago it said them.
//
// The reason is returned verbatim rather than reworded, and that is load-bearing: callers parse
// it. lootchestfisherplan.ParseBankedCount reads the real banked count out of the batch-floor
// decline, and woodcutplan.MuleDeclineIsEmptyStock matches the stock one. A remembered answer has
// to be the same string the mule sent, or replaying it would break every reader of it.
func (m *RefusalMemory) Refused(now time.Time, mule, item string) (reason string, age time.Duration, refused bool) {
	if m == nil {
		return "", 0, false
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	r, ok := m.by[refusalKey(mule, item)]
	if !ok || !now.Before(r.until) {
		return "", 0, false
	}
	return r.reason, now.Sub(r.at), true
}

// Clear forgets any standing refusal of item by mule. Call it on evidence to the contrary — a
// mule that has just handed one over plainly has them, whatever it said half an hour ago.
func (m *RefusalMemory) Clear(mule, item string) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	delete(m.by, refusalKey(mule, item))
}

// Forget drops every remembered refusal by one mule, for a caller that knows its bank has
// changed wholesale (a restock, a session coming back).
func (m *RefusalMemory) Forget(mule string) {
	if m == nil || mule == "" {
		return
	}
	prefix := refusalKey(mule, "")
	m.mu.Lock()
	defer m.mu.Unlock()
	for k := range m.by {
		if strings.HasPrefix(k, prefix) {
			delete(m.by, k)
		}
	}
}

// Sweep drops lapsed entries, so a long-running process does not accumulate one per item per
// mule forever. Refused already ignores a lapsed entry, so this is housekeeping and never a
// correctness requirement.
func (m *RefusalMemory) Sweep(now time.Time) {
	if m == nil {
		return
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	for k, r := range m.by {
		if !now.Before(r.until) {
			delete(m.by, k)
		}
	}
}

// Count is how many standing refusals are remembered, for a status line and for a test.
func (m *RefusalMemory) Count(now time.Time) int {
	if m == nil {
		return 0
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	n := 0
	for _, r := range m.by {
		if now.Before(r.until) {
			n++
		}
	}
	return n
}
