// Package casketplan is CasketOpener's own decisions, ported from rsb0t-scripts'
// casketPlan.ts and the loop embedded in CasketOpener.ts. It is pure: no client, actions,
// fleet or bot imports and no I/O.
//
// Deliberately not ported here, and why:
//   - casketHud.ts, hudPanel.ts, bankFeed.ts, tallyFeed.ts, updateCheck.ts — the Electron
//     wall's on-screen overlay/update-checker/feed publishers; no headless equivalent.
//   - CasketOpener.ts's own hardcoded casket object id (405). This client's content pack
//     numbering is not guaranteed to match rsb0t-scripts' upstream ids, so
//     internal/scripts/casketrunner resolves "Casket" by name via cachecfg at runtime instead
//     — see casketrunner.ResolveCasketIDs.
package casketplan

// CasketName is the item name every casket variant (noted or not) shares.
const CasketName = "Casket"

// Stack is one item slot or bank row, generalised over both.
type Stack struct {
	ID    int
	Count int
}

// PackCounts totals a set of stacks by item id: a stack and a row of single slots read alike.
func PackCounts(items []Stack) map[int]int {
	out := map[int]int{}
	for _, i := range items {
		c := i.Count
		if c < 1 {
			c = 1
		}
		out[i.ID] += c
	}
	return out
}

// Gained is what one opening added: every id whose total went up, excluding any id isCasket
// reports true for (a casket only ever goes down as it's opened) — ported from gained(). A
// predicate rather than a single id because this content pack maps several distinct object
// ids (one per treasure-trail tier) to the identical display name "Casket" — found live
// (cmd/casketopenertest, 2026-09-16): ::give casket landed a different id than the one
// resolved by picking just the last name match, so every id sharing the name is now treated
// as "a casket," matching how casketrunner.CasketIDs itself resolves the whole set.
func Gained(before, after map[int]int, isCasket func(id int) bool) map[int]int {
	out := map[int]int{}
	for id, count := range after {
		delta := count - before[id]
		if !isCasket(id) && delta > 0 {
			out[id] = delta
		}
	}
	return out
}

// BankStep is what the bank stop does once the rewards have gone in.
type BankStep string

const (
	StepWithdraw BankStep = "withdraw"
	StepDeposit  BankStep = "deposit"
	StepDone     BankStep = "done"
	StepFull     BankStep = "full"
)

// BankSnapshot is what BankStepFor needs, read fresh each pass.
type BankSnapshot struct {
	// BankCaskets is unnoted caskets the bank holds.
	BankCaskets int
	// NotedHeld is noted caskets still in the pack — they cannot be opened until the bank has
	// turned them back.
	NotedHeld int
	FreeSlots int
}

// BankStepFor ports bankStep: noted caskets go in first, since the bank counts them as the
// real thing once they are there; an empty bank with none of them left in the pack is the end.
func BankStepFor(s BankSnapshot) BankStep {
	if s.NotedHeld > 0 {
		return StepDeposit
	}
	if s.BankCaskets <= 0 {
		return StepDone
	}
	if s.FreeSlots <= 0 {
		return StepFull
	}
	return StepWithdraw
}

// AddFind adds what one casket gave to the run's tally.
func AddFind(tally map[string]int, name string, count int) {
	if count > 0 {
		tally[name] += count
	}
}

// FindRow is one line of the find tally.
type FindRow struct {
	Name  string
	Count int
}

// FindRows sorts the tally: the most first, then by name — ported from findRows().
func FindRows(tally map[string]int) []FindRow {
	out := make([]FindRow, 0, len(tally))
	for name, count := range tally {
		out = append(out, FindRow{Name: name, Count: count})
	}
	// Simple insertion sort: this list is small (a handful of reward types per run) and pulling
	// in "sort" for one call site isn't worth it, but stays correct either way — swap to
	// sort.Slice if this ever needs to handle a large tally.
	for i := 1; i < len(out); i++ {
		for j := i; j > 0; j-- {
			a, b := out[j-1], out[j]
			if a.Count > b.Count || (a.Count == b.Count && a.Name <= b.Name) {
				break
			}
			out[j-1], out[j] = out[j], out[j-1]
		}
	}
	return out
}
