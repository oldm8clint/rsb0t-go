// Package wcfletchrunner is the I/O half of the ProgressiveWCFletch port: reading the Fletching
// level, collecting a knife, and cutting a pack of logs into the product
// internal/scripts/wcfletchplan chose.
//
// # It has no Deps of its own, on purpose
//
// Every sibling runner declares its own Deps and its own WalkNear/WaitForGuardian/bank helpers,
// and docs/script_porting_framework.md's "why duplicated, not shared" section is right that this
// is usually correct — each copy differs in its own EmergencyFlee reference point. This package
// is the one case where it would be wrong. ProgressiveWCFletch walks the identical routes to the
// identical tiers, banks at the identical Seers booth and chops the identical trees as
// ProgressiveWoodcutter; the ONLY thing it does differently is fletch. A second copy of
// woodcutrunner's walking and banking would be a copy that drifts, and the fleet owner's whole
// reason for wanting two scripts is to compare them — two scripts that walk differently are not
// a comparison. So this package takes *woodcutrunner.Deps directly and calls woodcutrunner for
// everything that is not fletching.
//
// # What is genuinely new here
//
// One function: Fletch. It is the shape bankfletchrunner.Fletch already proved live, generalised
// from "arrow shafts only" to any cut_logs product, with two differences that matter and are
// explained at the function itself: the product is chosen by the caller's plan rather than by
// what the menu happens to show, and the call does not return until the make BATCH has finished
// rather than when its first item lands.
//
// bankfletchrunner.Fletch is deliberately called rather than copied where it would do — it does
// not, because it is hard-wired to bankfletchplan.ArrowShaft/MakeMatch and that package is held
// by another session. Everything else it taught (wait for a menu OFFERING the product rather
// than for "a make menu is open"; close a half-driven interface so the next attempt cannot
// mistake it for a usable one; the receipt is the pack, never the returned reason) is carried
// over here verbatim in intent.
//
// Deliberately NOT built here: stringing the bows, and attaching heads to the shafts. Both need
// an item a woodcutter never has (a bow string, an arrowtip), and the (u) bows exist precisely so
// a different account does the stringing — see wcfletchplan's own package doc.
package wcfletchrunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/tile"
	"rsb0t-go/internal/scripts/wcfletchplan"
	"rsb0t-go/internal/scripts/woodcutplan"
	"rsb0t-go/internal/scripts/woodcutrunner"
)

// Deps is woodcutrunner's, unchanged — see this package's doc comment for why there is no second
// one.
type Deps = woodcutrunner.Deps

// FletchingStat is the Fletching skill's protocol index.
//
// 9, read off internal/gamestate.SkillNames' own table ("Attack, Defence, Strength, Hitpoints,
// Ranged, Prayer, Magic, Cooking, Woodcutting, Fletching, ..."), the same table
// woodcutrunner.WoodcuttingStat = 8 comes from. TestFletchingStatIsFletching pins it against
// gamestate.SkillName rather than leaving it as a number nobody checked.
const FletchingStat = 9

func logf(d *Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

// ---- live reads ----

// FletchingLevel is the account's live Fletching level, 0 when the server has not reported it.
func FletchingLevel(d *Deps) int {
	skill, ok := d.Session.State().Skill(FletchingStat)
	if !ok {
		return 0
	}
	return skill.Level
}

// FletchingXP is the account's live Fletching experience.
func FletchingXP(d *Deps) int {
	skill, ok := d.Session.State().Skill(FletchingStat)
	if !ok {
		return 0
	}
	return skill.XP
}

// CountByID totals the pack's items with obj id.
//
// BY ID, and that is the whole reason this exists rather than a name count: the unstrung and the
// strung bow of every species share a display name in content 289 (see wcfletchplan/ladder.go's
// header), so a name count of "Oak shortbow" would include a strung one and report stock the
// mule is not going to receive.
func CountByID(d *Deps, id int) int {
	n := 0
	for _, row := range woodcutrunner.PackSlotRows(d) {
		if row.ID != id {
			continue
		}
		c := row.Count
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// CountByName totals the pack's items named name. Used only for the LOGS, whose names are
// unambiguous.
func CountByName(d *Deps, name string) int {
	n := 0
	for _, row := range woodcutrunner.PackSlotRows(d) {
		if !strings.EqualFold(strings.TrimSpace(row.Name), strings.TrimSpace(name)) {
			continue
		}
		c := row.Count
		if c < 1 {
			c = 1
		}
		n += c
	}
	return n
}

// itemID is the obj id of the first pack item named name.
func itemID(d *Deps, name string) (int32, bool) {
	for _, row := range woodcutrunner.PackSlotRows(d) {
		if strings.EqualFold(strings.TrimSpace(row.Name), strings.TrimSpace(name)) {
			return int32(row.ID), true
		}
	}
	return 0, false
}

// HasKnife reports whether the pack holds a Knife.
func HasKnife(d *Deps) bool { return woodcutrunner.HasKnife(d) }

// KnifeInReach reports whether a knife ground spawn is close enough to be worth the detour from
// where the account is standing.
func KnifeInReach(d *Deps) bool {
	x, z, level, ok := d.Session.State().Tile()
	if !ok {
		return false
	}
	_, _, in := woodcutplan.NearestKnife(tile.Tile{X: x, Z: z, Level: level})
	return in
}

// TakeKnife walks to the nearest knife ground spawn and picks it up — woodcutrunner's own, called
// unchanged. Already holding one is a success with no packets sent.
func TakeKnife(d *Deps) woodcutrunner.KnifeResult { return woodcutrunner.TakeKnife(d) }

// ---- fletching ----

// FletchResult is one Fletch call's outcome.
type FletchResult struct {
	// Made is true only once the product's count in this account's own pack has actually risen.
	// The receipt is the pack, never the returned reason — bankfletchrunner's own hard-won rule.
	Made bool
	// Products is how many of the product arrived, Logs how many went in.
	Products int
	Logs     int
	// Reason is why it ended: "made", "no-knife", "no-logs", "reserve" (only the logs kept back for the chop
	// manipulation's knife were left, so nothing was cut), "use-failed", "no-menu", "nothing-made" or "stopped".
	Reason string
}

const (
	// menuWait is how long the make menu has to appear OFFERING the chosen product.
	menuWait = 3 * time.Second
	// firstItemWait is how long the first product has to land after Make-X is answered.
	firstItemWait = 15 * time.Second
	// batchPoll is how often the batch is sampled while it runs.
	batchPoll = 600 * time.Millisecond
	// batchStall is how long the log count may stay still before the batch is called finished.
	batchStall = 3 * time.Second
	// batchCap bounds a whole batch. content's [queue,fletch_logs] re-queues itself with
	// `%action_delay = add(map_clock, 2)`, so one log costs 2 ticks: 28 logs is ~34s at the
	// production 600ms tick. Sixty seconds leaves room for a lagging round trip without letting
	// a wedged batch hold a Loop call open indefinitely.
	batchCap = 60 * time.Second
)

// Fletch cuts every one of the pack's logsName into p, via the knife-on-logs make menu, except the reserve logs it leaves
// in the pack: the one log ProgressiveWCFletch keeps for the chop manipulation's knife (wcfletchplan.CarrierReserve).
//
// TWO THINGS IT DOES THAT A NAIVE VERSION WOULD NOT, both of them load-bearing:
//
//  1. IT ASKS THE MENU ONLY FOR THE PRODUCT THE PLAN ALREADY CHOSE, and the plan applied the
//     level gate. content/scripts/skill_fletching/scripts/cut_logs.rs2 calls ~skill_multi2 with
//     BOTH bows unconditionally and tests `if (stat(fletching) < $level)` afterwards, answering a
//     product the account may not make with a ~doubleobjbox message and no experience. So "the
//     longbow is on the menu" is not permission to make one, and a runner that read the menu as
//     the source of truth would batch a refusal 28 times and log a success.
//
//  2. IT WAITS FOR THE WHOLE BATCH, not for the first item. content processes one log every two
//     ticks and re-queues itself, so the first product lands about a second in while the other
//     twenty-seven are still coming. Returning there would report Products=1 for a batch of 28
//     and, worse, hand the caller back a pack it is about to re-read mid-batch — which is how a
//     caller ends up clicking a second knife-on-logs into a make interface that is still running.
//     The settle below watches the LOG count fall and calls it finished when it stops moving.
func Fletch(d *Deps, logsName string, p wcfletchplan.Product, reserve int) FletchResult {
	if reserve < 0 {
		reserve = 0
	}
	knifeID, ok := itemID(d, wcfletchplan.Knife)
	if !ok {
		return FletchResult{Reason: "no-knife"}
	}
	logsID, ok := itemID(d, logsName)
	if !ok {
		return FletchResult{Reason: "no-logs"}
	}
	logsBefore := CountByName(d, logsName)
	if logsBefore <= reserve {
		return FletchResult{Reason: "reserve"}
	}
	cut := logsBefore - reserve
	productBefore := CountByID(d, p.ItemID)

	// A bank (or any modal) closed a moment ago is closed by the engine in its player turn, AFTER the tick's input has
	// been read: a knife-on-logs read in the same tick opens the make menu and has it closed again. That is the "no-menu"
	// every first fletch after a Seers bank visit logged, and every BassOysterCaskets fletching round (bank, close, use):
	// the second try, seconds later, always worked (gamestate/modalclose.go). Two ticks after the close it is safe.
	if !actions.SettleAfterClose(d.Session, woodcutrunner.Waiter(d)) {
		return FletchResult{Reason: "stopped"}
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	if err := eq.UseOnHeld(knifeID, logsID); err != nil {
		// Deliberately its own reason rather than "no-logs": the logs ARE in the pack and the use
		// itself was refused, so the caller's next pass would re-derive the identical state and
		// retry immediately. A live run span exactly this branch 26.9 million times in one session
		// (actions.ErrNotOnScreen, the engine not having assigned the account's sidebar tabs yet,
		// so Equipment could not resolve the backpack component at all — CLAUDE.md's OpLocU gotcha,
		// gate 5). A distinct reason lets the caller back off.
		logf(d, "wcfletch: knife on %s was refused: %v", strings.ToLower(logsName), err)
		return FletchResult{Reason: "use-failed", Logs: 0}
	}

	dlg := actions.NewChatDialog(d.Session, d.Components)
	offers := func() bool { return dlg.Offers(d.Config, p.MakeMatch) }
	if !waitFor(d, menuWait, offers) {
		if dlg.IsMakeMenu() {
			logf(d, "wcfletch: a make menu was open but offered %s, not a %s — closing it", dlg.DescribeProducts(d.Config), p.MakeMatch)
		}
		// Leave nothing open for the next attempt to mistake for a usable menu.
		_ = actions.CloseModal(d.Session)
		return FletchResult{Reason: "no-menu"}
	}
	if err := dlg.MakeX(d.Config, p.MakeMatch, int32(cut), woodcutrunner.Waiter(d)); err != nil {
		logf(d, "wcfletch: MakeX(%s x%d): %v — the menu offered %s", p.MakeMatch, cut, err, dlg.DescribeProducts(d.Config))
		_ = actions.CloseModal(d.Session)
		return FletchResult{Reason: "no-menu"}
	}

	// What this batch actually makes, for the Loot panel's Fletching card (see fletchcount.go).
	// Opened AFTER MakeX so the baseline is the pack as the batch starts, exactly as
	// bankfletchrunner does it; reporting only, and nil-safe when no Loot sink is wired.
	window := beginFletch(d, int(logsID), logsName)

	// The first item, then the rest of the batch.
	if !waitFor(d, firstItemWait, func() bool {
		return CountByID(d, p.ItemID) > productBefore || CountByName(d, logsName) < logsBefore
	}) {
		out := FletchResult{Reason: "nothing-made"}
		logf(d, "wcfletch: nothing came of cutting %d %s into %s", cut, strings.ToLower(logsName), strings.ToLower(p.ItemName))
		// Nothing was made, so this reports nothing (loot.Window.report drops a window with no
		// gain and no events). It is flushed rather than abandoned so the next batch's baseline
		// is not this stale one.
		window.Flush()
		return out
	}
	stopped := !settle(d, logsName, logsBefore, reserve)

	logsAfter, productAfter := CountByName(d, logsName), CountByID(d, p.ItemID)
	out := FletchResult{
		Products: productAfter - productBefore,
		Logs:     logsBefore - logsAfter,
		Reason:   "made",
	}
	// Events are LOGS CONSUMED, not bows made — loot.ActionFletch's contract, because the panel
	// prices a fletching card's cost as events x the material's price. Flushed here, after
	// settle, which is the whole reason a bow lands inside THIS window instead of the next chop
	// one. Both calls are nil-safe.
	if out.Logs > 0 {
		window.Got(out.Logs)
	}
	window.Flush()
	if stopped {
		out.Reason = "stopped"
	}
	out.Made = out.Products > 0
	if out.Made {
		logf(d, "wcfletch: cut %d %s into %d %s (Fletching %d)",
			out.Logs, strings.ToLower(logsName), out.Products, strings.ToLower(p.ItemName), FletchingLevel(d))
	} else if !stopped {
		out.Reason = "nothing-made"
	}
	return out
}

// settle waits for a running make batch to finish, watching the LOG count rather than the product
// count.
//
// The log count is the right thing to watch and the product count is not: arrow shafts arrive
// fifteen at a time into one stack, so a product reading can jump and then sit still for a whole
// tick while the batch is plainly still running, and an unstackable bow landing in a new slot is
// only visible once the inventory update arrives. The logs fall by exactly one per craft and
// reach zero when the batch is done, which content guarantees — [queue,fletch_logs] deletes one
// log per pass and stops when `inv_total(inv, $log) = 0`.
//
// Returns false when the caller's own Delay asked to stop.
func settle(d *Deps, logsName string, from, reserve int) bool {
	deadline := time.Now().Add(batchCap)
	last, lastMoved := from, time.Now()
	for time.Now().Before(deadline) {
		if !d.Delay(batchPoll) {
			return false
		}
		now := CountByName(d, logsName)
		if now <= reserve {
			return true
		}
		if now != last {
			last, lastMoved = now, time.Now()
			continue
		}
		if time.Since(lastMoved) >= batchStall {
			// The batch stopped with logs still in the pack. Ordinary and not an error: a level-up
			// dialogue interrupts it, and so does a random event. The caller re-derives and comes
			// back.
			return true
		}
	}
	logf(d, "wcfletch: the make batch was still running after %s with %d %s left — carrying on", batchCap, last, strings.ToLower(logsName))
	return true
}

func waitFor(d *Deps, timeout time.Duration, cond func() bool) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
}

// ---- counting the stock ----

// PackStock counts the fletched units (unstrung bows and arrow shafts) in the pack, by id.
func PackStock(d *Deps) int {
	return wcfletchplan.HandoverStock(woodcutrunner.PackSlotRows(d))
}

// BankStock counts the fletched units in an already-open bank, by id.
func BankStock(d *Deps, bank *actions.Bank) int {
	return wcfletchplan.HandoverStock(woodcutrunner.BankItemRows(d, bank))
}

// RungLogsHeld is how many of a rung's own logs the pack holds.
func RungLogsHeld(d *Deps, rung wcfletchplan.Rung) int { return CountByID(d, rung.LogsID) }

// PackLogs counts the pack's usable logs by display name: every un-noted log of every species (a note cannot take a
// knife), the carrier candidates wcfletchplan.CarrierReserve and CarrierFromBank choose from.
func PackLogs(d *Deps) map[string]int {
	out := map[string]int{}
	inv := d.Session.State().Inventory(woodcutrunner.PlayerPack)
	if inv == nil {
		return out
	}
	for _, it := range inv.NonEmpty() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !woodcutplan.IsLogs(obj.Name) {
			continue
		}
		out[strings.TrimSpace(obj.Name)]++
	}
	return out
}

// BankLogs counts the logs an open bank holds by display name (the bank keeps un-noted items).
func BankLogs(d *Deps, bank *actions.Bank) map[string]int {
	out := map[string]int{}
	for _, it := range bank.Items() {
		obj, ok := d.Config.Obj(int(it.ID))
		if !ok || obj.Noted() || !woodcutplan.IsLogs(obj.Name) {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		out[strings.TrimSpace(obj.Name)] += c
	}
	return out
}

// FletchSnapshot builds wcfletchplan.FletchSnapshot from live state.
//
// Every reading goes through woodcutrunner's own PlayerPack 3214 view, never a tab-resolved one:
// an account whose sidebar tabs the server has not assigned reads as holding nothing, and a
// fletching decision taken on that would send a fully-equipped account off to fetch a knife it is
// already carrying. The caller's own snapshot refuses to decide anything while
// woodcutrunner.ContainersReadable is false, and this is the second line of that same defence.
func FletchSnapshot(d *Deps, enabled bool, rung wcfletchplan.Rung, product wcfletchplan.Product, canFletch, idle, banking bool, reserve int) wcfletchplan.FletchSnapshot {
	kind := wcfletchplan.KindNone
	stacking := false
	if canFletch {
		kind = product.Kind
		// Stacking is taken from the product's own PerLog rather than from its kind, so it stays
		// true of whatever content says yields more than one per log — it is `data=shafts,15`
		// that makes the first rung special, not the word "shaft".
		stacking = product.PerLog > 1
	}
	return wcfletchplan.FletchSnapshot{
		Enabled:      enabled,
		HasKnife:     HasKnife(d),
		KnifeInReach: KnifeInReach(d),
		Product:      kind,
		Stacking:     stacking,
		Logs:         max(RungLogsHeld(d, rung)-reserve, 0),
		PackFull:     woodcutrunner.PackFull(d),
		Idle:         idle,
		Banking:      banking,
	}
}
