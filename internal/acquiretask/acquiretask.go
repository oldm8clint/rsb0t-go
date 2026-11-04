// Package acquiretask is a small, pure planning primitive for turning "I need Count of
// item Item" into an ordered list of concrete steps: use what's already held, then
// withdraw from an open bank, then buy from an open shop, and if a shortfall still
// remains after both, say so honestly instead of guessing at a "go gather it" strategy
// this project doesn't have.
//
// This is a loose port of the third-party server-api-docs reference's AcquireTask
// (see reference/server-api-docs.md's own AcquireTask section): `new AcquireTask(bot,
// needs)` with `ItemNeed = {name, count, source}`, `source ∈ {shop, ground, gather,
// make}` — of which the upstream itself only actually implements `shop` and `ground`
// ("gather"/"make" throw "not implemented yet" there too, an upstream limitation, not
// something this port invented). This port swaps `ground` for `bank`: this repo has no
// GroundItems decode yet (see COLLAB.md's grounditems-worktree claim — OBJ_ADD/OBJ_DEL
// are still discarded during zone decode, not parsed at all), while a bank withdrawal is
// the far more common restock path every live-proved script here already uses. `gather`
// stays an honest "still short after bank+shop" result rather than inventing dispatch
// logic no script here needs yet — the same "gather/make not implemented" scope the
// upstream itself admits to. Also unlike the upstream, a caller here doesn't declare a
// need's source up front — Plan decides it, preferring the free bank withdrawal over a
// paid shop purchase, and only asking for as much as the shortfall actually is.
//
// Pure: no client/actions/gamestate/fleet/bot imports, no I/O, matching every sibling
// *plan package's style (see internal/scripts/mulereceiverplan, barbarianplan,
// fisherplan, crafterplan). It lives outside internal/scripts because several sessions
// are mid-flight in that tree tonight; the live execution layer that reads real
// held/bank/shop counts and dispatches into internal/actions.Bank/Shop is
// internal/actions/acquiretask.go, kept thin on purpose.
package acquiretask

import "fmt"

// Source names one way a Step is resolved.
type Source string

const (
	// SourceBank means withdraw the step's Count from an already-open bank.
	SourceBank Source = "bank"
	// SourceShop means buy the step's Count from an already-open shop.
	SourceShop Source = "shop"
	// SourceGather means neither bank nor shop (as given to Plan) can supply the
	// remainder — the caller must fall back to some acquisition method this package
	// doesn't know about (gathering, crafting, a trade, ...), exactly the upstream's
	// own unimplemented "gather"/"make" sources.
	SourceGather Source = "gather"
)

// Need is one line of "I need Count of Item" — Item is a plain display/lookup name
// (resolved to an obj id by the caller, e.g. via internal/cachecfg.ObjIDs), matching
// every other *plan package's string-named convention rather than baking in an id type
// this pure package has no business knowing about.
type Need struct {
	Item  string
	Count int32
}

// Step is one instruction in a Plan: acquire Count of the planned Need's item from
// Source. A step for "already held, nothing to do" is never emitted — Plan only
// returns work that's actually needed; HasAll (or an empty Plan result) is how a caller
// checks for "nothing to do at all".
type Step struct {
	Source Source
	Count  int32
}

// String renders a Step for logging, e.g. "withdraw 4 from bank".
func (s Step) String() string {
	switch s.Source {
	case SourceBank:
		return fmt.Sprintf("withdraw %d from bank", s.Count)
	case SourceShop:
		return fmt.Sprintf("buy %d from shop", s.Count)
	case SourceGather:
		return fmt.Sprintf("%d still short — must be gathered", s.Count)
	default:
		return fmt.Sprintf("%d from %s", s.Count, s.Source)
	}
}

// Shortfall reports how many more are needed beyond what's held, floored at zero (never
// negative just because more is held than needed).
func Shortfall(held, need int32) int32 {
	if d := need - held; d > 0 {
		return d
	}
	return 0
}

// Plan decides how to close the gap between held and need.Count. It prefers the bank
// over the shop — a withdrawal costs nothing, a purchase costs gold — and never asks for
// more than the shortfall actually is, even if a source has more available than that.
//
// bankStock and shopStock are the caller's own read of what's currently available from
// each source (0 if that source isn't open, doesn't exist here, or simply has none of
// the item — Plan does not distinguish those cases, and does not care why a source has
// nothing to offer). This keeps Plan pure: it never asks whether a bank or shop is
// "open", it's just told two numbers.
//
// Returns steps in the order they should be executed. A nil/empty slice means need is
// already fully held. A trailing SourceGather step means bank+shop together could not
// cover the full shortfall — the caller must fall back to some other acquisition method
// (or fail the task outright), matching the upstream AcquireTask's own honest
// gather/make "not implemented" stance rather than pretending this package could resolve
// it.
func Plan(held int32, need Need, bankStock, shopStock int32) []Step {
	remaining := Shortfall(held, need.Count)
	if remaining <= 0 {
		return nil
	}

	var steps []Step
	if bankStock > 0 {
		take := min32(bankStock, remaining)
		steps = append(steps, Step{Source: SourceBank, Count: take})
		remaining -= take
	}
	if remaining > 0 && shopStock > 0 {
		take := min32(shopStock, remaining)
		steps = append(steps, Step{Source: SourceShop, Count: take})
		remaining -= take
	}
	if remaining > 0 {
		steps = append(steps, Step{Source: SourceGather, Count: remaining})
	}
	return steps
}

func min32(a, b int32) int32 {
	if a < b {
		return a
	}
	return b
}

// Held reports whether a single held count already satisfies need.
func Held(held, need int32) bool { return held >= need }

// HasAll reports whether every need in needs is already satisfied by the matching held
// count in heldByItem (0 if a needed item is missing from the map entirely) — the port
// of the upstream AcquireTask.hasAll(needs) helper.
func HasAll(needs []Need, heldByItem map[string]int32) bool {
	for _, n := range needs {
		if heldByItem[n.Item] < n.Count {
			return false
		}
	}
	return true
}
