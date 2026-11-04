package orderplan

// The bundled kit order: a donor asking for up to KitLinesMax different items at once, handed
// over in ONE trade of at most a pack's worth of trade-screen slots.
//
// WHY IT EXISTS (owner, 2026-09-23). There is one mule, the mule, for the whole fleet, and the
// ScarabSwarm cohort asks it for kits (food, runes, a staff, coins, rope). One order per item is
// one bank trip and one trade per item, on the one account every fisher is also queueing for; the
// owner's ruling is that a kit ask is ONE trade of at most 28 unstacked items. This file is the
// pure half: which lines the mule will serve, how many of each, and which travel as bank notes so
// that the whole kit fits one trade screen and the donor's pack.
//
// It reuses Decide line by line rather than restating any of its rules. Every line is judged
// against the SAME ledger, with nothing recorded between lines, so the per-donor order cap is
// checked exactly once for the kit -- all lines pass it or all fail it with the same reason --
// and every other rule (the catalogue, the open-stock path and its never-list, the per-order and
// per-window caps, the floor and the real stock) is Decide's own, byte for byte. Ledger.RecordKit
// then counts a delivered kit as ONE order against the donor.

import (
	"strconv"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fighterfood"
)

// KitLinesMax is the most lines one kit order carries: one trade screen's worth of slots, which
// is also the most a pack can receive, since every line needs at least one slot.
const KitLinesMax = 28

// KitLine is one line of a kit: an item named as the game names it, and how many.
type KitLine struct {
	Item string
	Qty  int
	// Noted asks for the line as bank notes: a non-stackable with a note form then crosses as notes
	// whatever its count, one trade slot for the whole line. A donor that banks what it gets asks
	// for it (MasterCrafter's storage round: the a user request, "the mule should hand over items
	// noted").
	Noted bool
}

// KitFacts is what the mule's object config says about one item, which the slot plan needs.
type KitFacts struct {
	// Stackable items take one slot at any count.
	Stackable bool
	// HasNote is whether the item has a bank-note form a non-stackable line can travel as.
	HasNote bool
}

// KitLineDecision is the mule's answer for one line.
type KitLineDecision struct {
	// Item is the canonical spelling when the line is served, the donor's own otherwise.
	Item string
	// Asked is what the donor asked for; Qty is what will be handed over (0 = not served).
	Asked int
	Qty   int
	// Noted is true when the line crosses as bank notes (one slot) rather than the item.
	Noted bool
	// Reason is why the line is not served, or why it was cut, short enough for a bus message.
	Reason string
}

// KitDecision is the whole answer: one entry per line, in the donor's order.
type KitDecision struct {
	Lines []KitLineDecision
}

// Serving is how many lines will actually be handed over.
func (k KitDecision) Serving() int {
	n := 0
	for _, l := range k.Lines {
		if l.Qty > 0 {
			n++
		}
	}
	return n
}

// Slots is how many trade-screen (and receiving pack) slots the served lines take.
func (k KitDecision) Slots(facts func(item string) (KitFacts, bool)) int {
	n := 0
	for _, l := range k.Lines {
		n += lineSlots(l, facts)
	}
	return n
}

func lineSlots(l KitLineDecision, facts func(item string) (KitFacts, bool)) int {
	if l.Qty <= 0 {
		return 0
	}
	f, _ := facts(l.Item)
	if f.Stackable || l.Noted {
		return 1
	}
	return l.Qty
}

// The kit-level decline reasons.
const (
	DeclineKitTooLong = "a kit carries at most 28 lines"
	DeclineKitEmpty   = "the kit names nothing"
	DeclineKitNoRoom  = "no room for it in this trade"
	DeclineKitNoNote  = "too many to trade and it has no bank note"
)

// NormaliseKit merges lines that name the same item (case-insensitively, whitespace-trimmed),
// drops lines with no quantity, and keeps the donor's order (the first mention decides a merged
// line's place). It reports DeclineKitTooLong when more than KitLinesMax distinct items remain.
func NormaliseKit(lines []KitLine) ([]KitLine, string) {
	var out []KitLine
	at := map[string]int{}
	for _, l := range lines {
		name := strings.TrimSpace(l.Item)
		if name == "" || l.Qty < 1 {
			continue
		}
		key := strings.ToLower(name)
		if i, ok := at[key]; ok {
			out[i].Qty += l.Qty
			out[i].Noted = out[i].Noted || l.Noted
			if out[i].Qty > QtyMax {
				out[i].Qty = QtyMax
			}
			continue
		}
		at[key] = len(out)
		out = append(out, KitLine{Item: name, Qty: l.Qty, Noted: l.Noted})
	}
	if len(out) == 0 {
		return nil, DeclineKitEmpty
	}
	if len(out) > KitLinesMax {
		return out, DeclineKitTooLong
	}
	return out, ""
}

// KitFoodAsNotes reports whether a kit line of qty of item crosses as bank notes whatever its size:
// FOOD, two or more of it. The owner's rule of 2 October 2026 ("kits and food asks must go as bank
// notes, one trade slot per food type; the receiving account banks the notes and withdraws unnoted
// food as it needs it"): a 500-food kit is one slot, a 16-food ask is one slot, and the donor's kit
// bundle un-notes what arrives at the mule's own bank. Before, only a line above NotesAbove (28) was
// noted, so every small food ask crossed unnoted and took a pack slot a bite.
func KitFoodAsNotes(item string, qty int) bool {
	if qty < 2 {
		return false
	}
	_, food := fighterfood.BaseFood(item)
	return food
}

// DecideKit decides a whole kit order from donor `from`.
//
// stock is Decide's (what the mule can draw of a canonical name; nil declines). facts says, per
// canonical name, whether the item stacks and has a note; an item it does not know is treated as
// a non-stackable with no note, the conservative reading. room is the donor's own free pack slots
// as it reported them; 0 or less means "not reported" and the trade screen's KitLinesMax is used.
//
// The slot plan, once each line's quantity is decided:
//  1. A non-stackable line above NotesAbove travels as notes, as a single order would.
//  2. While the served lines need more slots than room, the unnoted non-stackable line with the
//     most units that has a note form is switched to notes (it then takes one slot). Largest
//     first, so as few lines as possible reach the donor as notes it must un-note at a bank.
//  3. If that is still too many, lines are dropped from the END of the kit -- the donor lists
//     what it needs most first -- with DeclineKitNoRoom. A multi-unit line with no note form is
//     cut to the slots left before it is dropped.
func DecideKit(now time.Time, from string, lines []KitLine, pol Policy, led *Ledger, stock func(item string) int, facts func(item string) (KitFacts, bool), room int) KitDecision {
	if facts == nil {
		facts = func(string) (KitFacts, bool) { return KitFacts{}, false }
	}
	norm, bad := NormaliseKit(lines)
	if bad != "" {
		out := KitDecision{}
		for _, l := range norm {
			out.Lines = append(out.Lines, KitLineDecision{Item: l.Item, Asked: l.Qty, Reason: bad})
		}
		if len(out.Lines) == 0 {
			out.Lines = []KitLineDecision{{Reason: bad}}
		}
		return out
	}
	out := KitDecision{Lines: make([]KitLineDecision, 0, len(norm))}
	for _, l := range norm {
		d := Decide(now, from, Format(l.Item, l.Qty), pol, led, stock)
		line := KitLineDecision{Item: l.Item, Asked: l.Qty}
		if d.Verdict != Fulfil || d.Qty < 1 {
			line.Reason = d.Reason
			if line.Reason == "" {
				line.Reason = "not an item I hand out"
			}
		} else {
			line.Item, line.Qty = d.Item, d.Qty
			if d.Clamped {
				line.Reason = d.Reason
			}
			if f, _ := facts(d.Item); !f.Stackable {
				switch {
				case TravelsAsNotes(d.Qty, false):
					if !f.HasNote {
						line.Qty, line.Reason = 0, DeclineKitNoNote
					} else {
						line.Noted = true
					}
				case KitFoodAsNotes(d.Item, d.Qty) && f.HasNote:
					line.Noted = true
				case l.Noted && f.HasNote:
					line.Noted = true
				}
			}
		}
		out.Lines = append(out.Lines, line)
	}

	limit := KitLinesMax
	if room > 0 && room < limit {
		limit = room
	}
	for out.Slots(facts) > limit {
		best := -1
		for i, l := range out.Lines {
			if l.Qty < 2 || l.Noted {
				continue
			}
			if f, _ := facts(l.Item); f.Stackable || !f.HasNote {
				continue
			}
			if best < 0 || l.Qty > out.Lines[best].Qty {
				best = i
			}
		}
		if best < 0 {
			break
		}
		out.Lines[best].Noted = true
	}
	for i := len(out.Lines) - 1; i >= 0 && out.Slots(facts) > limit; i-- {
		l := &out.Lines[i]
		if l.Qty <= 0 {
			continue
		}
		over := out.Slots(facts) - limit
		if slots := lineSlots(*l, facts); slots > 1 && slots-over >= 1 {
			l.Qty = slots - over
			l.Reason = "capped to " + strconv.Itoa(l.Qty) + ": " + DeclineKitNoRoom
			continue
		}
		l.Qty, l.Noted, l.Reason = 0, false, DeclineKitNoRoom
	}
	return out
}

// RecordKit notes that a kit crossed to `to`: one grant per line (so every item's window cap sees
// its units), all counted as ONE order against the donor's per-window order cap. kit identifies
// the kit; an empty kit is recorded like separate orders.
func (l *Ledger) RecordKit(now time.Time, to, kit string, lines []ItemCount) {
	l.sweep(now)
	for _, it := range lines {
		if it.Count <= 0 {
			continue
		}
		l.grants = append(l.grants, grant{at: now, to: normalise(to), item: strings.ToLower(strings.TrimSpace(it.Name)), qty: it.Count, kit: kit})
	}
}

// ---- the donor's side of a kit trade screen ----

// KitReceiveView is what the donor reads while the mule hands it a kit: DecideReceive's view with
// the whole kit as the wanted order.
type KitReceiveView struct {
	Screen  Screen
	Partner string
	Mule    string
	// Want is the kit the mule said it will hand over (its plan), by canonical name, at the
	// quantities it decided; before a plan is heard it is what the donor asked for.
	Want []KitLine
	// Planned is true once Want is the mule's own plan rather than the ask.
	Planned  bool
	Theirs   []ItemCount
	Mine     []ItemCount
	Accepted bool
	// Give is what the donor hands the mule in the SAME trade (the kit ask's hand-over riding the kit, owner
	// 2 Nov 2026), as it placed it on its own side of the screen; empty for a kit that gives nothing. With it,
	// Mine may hold these and nothing else, and the offer screen is accepted only once all of it shows.
	Give []ItemCount
}

// DecideReceiveKit decides the donor's side of a kit hand-out screen. Like DecideReceive it walks
// away from anything that is not the trade it asked for: another partner, being asked to give,
// an item not in the kit, or more of one than wanted. Unlike a single order, the mule places a
// kit one line at a time, and the server clears both acceptances whenever the offer changes, so
// with a plan the donor accepts the offer screen only once EVERY planned line is on it in full.
// The confirm screen is accepted whenever it holds a valid subset: the mule reaches it only by
// accepting, and whatever it shows is what crosses.
func DecideReceiveKit(v KitReceiveView) (ReceiveAction, string) {
	if v.Screen == ScreenNone {
		return ReceiveClosed, ""
	}
	if !NamesMatch(v.Partner, v.Mule) {
		return ReceiveDecline, "not the mule I asked"
	}
	give := countsByName(v.Give)
	mine := countsByName(v.Mine)
	if anyPositive(v.Mine) {
		if len(give) == 0 {
			return ReceiveDecline, "I am being asked to give something"
		}
		for key, n := range mine {
			if n > give[key] {
				return ReceiveDecline, "my side of the screen is not my hand-over"
			}
		}
	}
	want := map[string]int{}
	for _, l := range v.Want {
		want[strings.ToLower(strings.TrimSpace(l.Item))] += l.Qty
	}
	got := map[string]int{}
	for _, r := range v.Theirs {
		if r.Count <= 0 {
			continue
		}
		key := strings.ToLower(strings.TrimSpace(r.Name))
		if _, ok := want[key]; !ok {
			return ReceiveDecline, "offered something I did not ask for"
		}
		got[key] += r.Count
	}
	for key, n := range got {
		if n > want[key] {
			return ReceiveDecline, "offered more than I asked for"
		}
	}
	if len(got) == 0 {
		return ReceiveWait, "waiting for the mule to place its offer"
	}
	if v.Accepted {
		return ReceiveWait, "already accepted this screen"
	}
	if v.Screen == ScreenConfirm {
		return ReceiveConfirm, ""
	}
	if v.Planned {
		for key, n := range want {
			if n > 0 && got[key] < n {
				return ReceiveWait, "waiting for the rest of the kit"
			}
		}
	}
	for key, n := range give {
		if mine[key] < n {
			return ReceiveWait, "waiting for my hand-over to show"
		}
	}
	return ReceiveAccept, ""
}

// countsByName totals rows by lower-cased trimmed name, positive counts only.
func countsByName(rows []ItemCount) map[string]int {
	out := map[string]int{}
	for _, r := range rows {
		if r.Count > 0 {
			out[strings.ToLower(strings.TrimSpace(r.Name))] += r.Count
		}
	}
	return out
}
