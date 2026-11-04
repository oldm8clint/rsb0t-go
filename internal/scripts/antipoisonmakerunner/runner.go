// Package antipoisonmakerunner is the I/O half of AntipoisonMaker: reading the bank, drawing a batch,
// brewing and grinding in bursts, hunting unicorns for their horns, buying vials of water at Aemad's
// (East Ardougne) and a pestle at Jatix's (Taverley), and ordering Marrentill from the mule. The
// decisions are internal/scripts/antipoisonmakeplan's.
//
// Everything here reuses legs already proven elsewhere: the bank is fleetrunner.OpenBank (the
// webwalk, then the booth), a batch is drawn with actions.Bank.WithdrawX and brewed by the same
// burst of OPHELDU uses bowstringrunner.StringAll proves for Fletching (five a tick, confirmed by
// the product count), a fight is combatrunner.FightOne, and the mule ask is the bundled kit order.
package antipoisonmakerunner

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/mulebus"
	"rsb0t-go/internal/scripts/antipoisonmakeplan"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/tile"
)

// HerbloreStat is Herblore's index in the protocol's stat table (gamestate.SkillNames).
const HerbloreStat = 15

// PerTick is how many brew or grind uses go out together: the engine reads five user events a
// player a tick and both recipes run with no delay.
const PerTick = 5

// confirm is how long one burst has to show in the pack.
const confirm = 2500 * time.Millisecond

func logf(d *fleetrunner.Deps, format string, args ...any) {
	if d.Log != nil {
		d.Log(fmt.Sprintf(format, args...))
	}
}

func pack(d *fleetrunner.Deps) []gamestate.SlotItem {
	inv := d.Session.State().Inventory(fleetrunner.PlayerPack)
	if inv == nil {
		return nil
	}
	return inv.NonEmpty()
}

// PackCount is how many of id the pack holds.
func PackCount(d *fleetrunner.Deps, id int32) int { return int(actions.CountByID(pack(d), id)) }

// FreeSlots is the pack's empty slots.
func FreeSlots(d *fleetrunner.Deps) int { return max(0, antipoisonmakeplan.PackSlots-len(pack(d))) }

// Herblore is the account's current Herblore level; ok false before the server sent it.
func Herblore(d *fleetrunner.Deps) (int, bool) {
	s, ok := d.Session.State().Skill(HerbloreStat)
	if !ok || s.Generation == 0 {
		return 0, false
	}
	return s.Level, true
}

type waiter struct{ d *fleetrunner.Deps }

func (w waiter) Delay(t time.Duration) bool { return w.d.Delay(t) }
func (w waiter) DelayUntil(cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for {
		if cond() {
			return true
		}
		if !time.Now().Before(deadline) {
			return false
		}
		if !w.d.Delay(100 * time.Millisecond) {
			return false
		}
	}
}
func (w waiter) DelayTicks(n int) bool { return w.d.Delay(time.Duration(n) * 600 * time.Millisecond) }

// ---- the bank ----

// Counts is what the bank held after the pack was deposited into it.
type Counts struct {
	Marrentill, VialWater, VialEmpty, Dust, Horn, Unf, Antipoison, Coins int
	Pestle                                                               bool
}

func countsOf(items []gamestate.SlotItem) Counts {
	n := func(id int32) int { return int(actions.CountByID(items, id)) }
	return Counts{
		Marrentill: n(antipoisonmakeplan.MarrentillID), VialWater: n(antipoisonmakeplan.VialWaterID),
		VialEmpty: n(antipoisonmakeplan.VialEmptyID), Dust: n(antipoisonmakeplan.HornDustID),
		Horn: n(antipoisonmakeplan.HornID), Unf: n(antipoisonmakeplan.UnfID),
		Antipoison: n(antipoisonmakeplan.AntipoisonID), Coins: n(antipoisonmakeplan.CoinsID),
		Pestle: n(antipoisonmakeplan.PestleID) > 0,
	}
}

// openBank is fleetrunner.OpenBank (a var so a test can hand the runner an open bank).
var openBank = fleetrunner.OpenBank

// OpenAndRead walks to the bank at at, deposits the whole pack (notes deposit as the item) and
// reads the bank. The bank is left open for Draw; the caller closes it. Err is non-empty on failure.
func OpenAndRead(d *fleetrunner.Deps, at tile.Tile) (*actions.Bank, Counts, string) {
	bank, ok := openBank(d, at)
	if !ok {
		return nil, Counts{}, fmt.Sprintf("could not open the bank at %d,%d", at.X, at.Z)
	}
	if len(bank.Inventory()) > 0 {
		if err := bank.BatchDepositInventory(waiter{d}); err != nil {
			_ = bank.Close()
			return nil, Counts{}, "deposit: " + err.Error()
		}
	}
	return bank, countsOf(bank.Items()), ""
}

// Line is one bank row to draw.
type Line struct {
	ID int32
	N  int
}

// Draw withdraws each line as items (never notes) and confirms the pack holds them. It reports
// the first line that did not arrive in full.
func Draw(d *fleetrunner.Deps, bank *actions.Bank, lines ...Line) error {
	w := waiter{d}
	if err := bank.SetNoteMode(false, w); err != nil {
		return fmt.Errorf("note mode off: %w", err)
	}
	for _, l := range lines {
		if l.N <= 0 {
			continue
		}
		before := PackCount(d, l.ID)
		if err := bank.WithdrawX(l.ID, int32(l.N), w); err != nil {
			return fmt.Errorf("withdraw %d of id %d: %w", l.N, l.ID, err)
		}
		if !w.DelayUntil(func() bool { return PackCount(d, l.ID)-before >= l.N }, 4*time.Second) {
			return fmt.Errorf("drew %d of the %d wanted of id %d", PackCount(d, l.ID)-before, l.N, l.ID)
		}
	}
	return nil
}

// ---- brewing and grinding ----

// MakeResult is one burst loop's outcome.
type MakeResult struct {
	Made int
	// Reason is "done" (an input ran out), "refused" (a burst changed nothing: the level, a free
	// world), "use-failed" or "stopped".
	Reason string
}

// burst repeats send until the inputs are gone, confirming each burst by the product count rising.
func burst(d *fleetrunner.Deps, product int32, inputs func() int, send func(n int) (int, error)) MakeResult {
	var r MakeResult
	for {
		have := inputs()
		if have <= 0 {
			r.Reason = "done"
			return r
		}
		before := PackCount(d, product)
		sent, err := send(min(PerTick, have))
		if err != nil && sent == 0 {
			logf(d, "antipoison: use failed: %v", err)
			r.Reason = "use-failed"
			return r
		}
		deadline := time.Now().Add(confirm)
		got := 0
		for time.Now().Before(deadline) {
			if !d.Delay(100 * time.Millisecond) {
				r.Reason = "stopped"
				return r
			}
			if got = PackCount(d, product) - before; got >= sent {
				break
			}
		}
		if got <= 0 {
			r.Reason = "refused"
			return r
		}
		r.Made += got
	}
}

// MakeUnf makes Unfinished potions: a Vial of water used on each Marrentill, at most limit of them.
func MakeUnf(d *fleetrunner.Deps, limit int) MakeResult {
	eq := actions.NewEquipment(d.Session, d.Components)
	base := PackCount(d, antipoisonmakeplan.UnfID)
	inputs := func() int {
		return min(PackCount(d, antipoisonmakeplan.MarrentillID), PackCount(d, antipoisonmakeplan.VialWaterID), limit-(PackCount(d, antipoisonmakeplan.UnfID)-base))
	}
	return burst(d, antipoisonmakeplan.UnfID, inputs, func(n int) (int, error) {
		return eq.UseOnHeldPairs(antipoisonmakeplan.VialWaterID, antipoisonmakeplan.MarrentillID, n)
	})
}

// MakeAntipoison finishes Antipoison(3): Unicorn horn dust used on each Unfinished potion.
func MakeAntipoison(d *fleetrunner.Deps, limit int) MakeResult {
	eq := actions.NewEquipment(d.Session, d.Components)
	base := PackCount(d, antipoisonmakeplan.AntipoisonID)
	inputs := func() int {
		return min(PackCount(d, antipoisonmakeplan.UnfID), PackCount(d, antipoisonmakeplan.HornDustID), limit-(PackCount(d, antipoisonmakeplan.AntipoisonID)-base))
	}
	return burst(d, antipoisonmakeplan.AntipoisonID, inputs, func(n int) (int, error) {
		return eq.UseOnHeldPairs(antipoisonmakeplan.HornDustID, antipoisonmakeplan.UnfID, n)
	})
}

// Grind grinds the Unicorn horns held into dust with the Pestle and mortar.
func Grind(d *fleetrunner.Deps) MakeResult {
	eq := actions.NewEquipment(d.Session, d.Components)
	if PackCount(d, antipoisonmakeplan.PestleID) == 0 {
		return MakeResult{Reason: "no-pestle"}
	}
	return burst(d, antipoisonmakeplan.HornDustID, func() int { return PackCount(d, antipoisonmakeplan.HornID) }, func(n int) (int, error) {
		return eq.UseOnHeldMany(antipoisonmakeplan.PestleID, antipoisonmakeplan.HornID, n)
	})
}

// ---- the mule ----

// OrderMarrentill asks mule for qty clean Marrentill in one bundled trade, standing at its post
// (the caller walked there). The herbs may arrive as notes, left in the pack for the next bank
// visit, which deposits a note as the item. It returns how many arrived and the mule's reason when
// none did.
func OrderMarrentill(d *fleetrunner.Deps, bus *mulebus.Bus, me, mule string, qty int, post tile.Tile, world int, timeout time.Duration) (got int, reason string, stopped bool) {
	res, info := fleetrunner.RequestKitBundle(d, bus, me, mule,
		[]fleetplan.ItemCount{{Item: antipoisonmakeplan.MarrentillAsk, Count: qty}},
		fleetrunner.KitBundleOptions{Timeout: timeout, Bank: post, KeepNotes: true, PerOrder: 1000, FallbackTimeout: 90 * time.Second, World: world})
	got = res.Got[antipoisonmakeplan.MarrentillAsk] + info.NotedHeld[antipoisonmakeplan.MarrentillAsk]
	if got == 0 {
		reason = res.Declined[antipoisonmakeplan.MarrentillAsk]
		if reason == "" {
			reason = info.Reason
		}
	}
	return got, reason, info.Stopped
}

// ---- unicorns ----

// HuntResult is one hunt trip's outcome.
type HuntResult struct {
	Kills, Horns int
	// Reason is "enough", "full", "hp", "no-unicorns", "stopped" or "unreachable".
	Reason string
}

const (
	unicornName    = "Unicorn"
	hornName       = "Unicorn horn"
	fightTimeout   = 60 * time.Second
	noTargetGiveUp = 6 * time.Minute
	beat           = 2 * time.Minute
)

// Hunt walks to the unicorn field and kills unicorns, taking each horn, until the pack holds want
// horns, has no room, or the account's hitpoints fall to hpFloor. It gives up after noTargetGiveUp
// without a kill, and logs at least every two minutes.
func Hunt(d *fleetrunner.Deps, want, hpFloor int) HuntResult {
	var r HuntResult
	cd := fleetrunner.Combat(d)
	centre, leash := antipoisonmakeplan.UnicornCentre, antipoisonmakeplan.UnicornLeash
	skip := map[int]time.Time{}
	lastKill, lastBeat := time.Now(), time.Now()
	for {
		if r.Horns = PackCount(d, antipoisonmakeplan.HornID); r.Horns >= want {
			r.Reason = "enough"
			return r
		}
		if FreeSlots(d) == 0 {
			r.Reason = "full"
			return r
		}
		if !fleetrunner.WaitForGuardian(d) {
			r.Reason = "stopped"
			return r
		}
		if hp, ok := d.Session.State().HP(); ok && hp > 0 && hp <= hpFloor {
			r.Reason = "hp"
			return r
		}
		if time.Since(lastKill) > noTargetGiveUp {
			r.Reason = "no-unicorns"
			return r
		}
		if time.Since(lastBeat) > beat {
			lastBeat = time.Now()
			logf(d, "antipoison: hunting unicorns: %d kill(s), %d horn(s) held of %d", r.Kills, r.Horns, want)
		}
		if here := fleetrunner.CurrentTile(d); here == nil || tile.Chebyshev(tile.Tile{X: here.X, Z: here.Z, Level: here.Level}, centre) > leash {
			if !fleetrunner.WalkTo(d, centre, 4, 5*time.Minute) {
				r.Reason = "unreachable"
				return r
			}
			lastKill = time.Now() // the walk is not a fruitless wait
			continue
		}
		// A horn on the ground first: it is what the hunt is for.
		if takeHorn(d, cd, centre, leash) {
			lastKill = time.Now()
			continue
		}
		npc, ok := nearestTarget(d, cd, centre, leash, skip)
		if !ok {
			if !d.Delay(1500 * time.Millisecond) {
				r.Reason = "stopped"
				return r
			}
			continue
		}
		res := combatrunner.FightOne(cd, npc, fightTimeout)
		if res.Unreachable || res.DoorFailed || res.Refused != "" {
			skip[npc.Index] = time.Now().Add(2 * time.Minute)
		}
		if res.Attacked {
			lastKill = time.Now()
		}
		if !res.NotOurs && (res.Verdict == combatplan.Killed || res.Verdict == combatplan.LikelyKilled) {
			r.Kills++
		}
		// One line a fight: the first live run hunted 8 minutes for 0 kills with nothing in the log to say why.
		logf(d, "antipoison: unicorn fight at %d,%d: verdict %v, attacked %v, not ours %v, unreachable %v, refused %q",
			npc.X, npc.Z, res.Verdict, res.Attacked, res.NotOurs, res.Unreachable, res.Refused)
		takeHorn(d, cd, centre, leash)
	}
}

func nearestTarget(d *fleetrunner.Deps, cd *combatrunner.Deps, centre tile.Tile, leash int, skip map[int]time.Time) (gamestate.NPC, bool) {
	now := time.Now()
	var best gamestate.NPC
	bestDist, found := 0, false
	here := fleetrunner.CurrentTile(d)
	for _, n := range combatrunner.MonstersNear(cd, unicornName, centre, leash) {
		if until, ok := skip[n.Index]; ok && now.Before(until) {
			continue
		}
		dist := 0
		if here != nil {
			dist = tile.Chebyshev(*here, tile.Tile{X: n.X, Z: n.Z, Level: n.Level})
		}
		if !found || dist < bestDist {
			best, bestDist, found = n, dist, true
		}
	}
	return best, found
}

// takeHorn picks up the nearest Unicorn horn on the ground within the leash; true when one went
// into the pack.
func takeHorn(d *fleetrunner.Deps, cd *combatrunner.Deps, centre tile.Tile, leash int) bool {
	var best *gamestate.GroundItem
	items := combatrunner.GroundItemsNamed(cd, hornName)
	for i := range items {
		it := items[i]
		if tile.Chebyshev(tile.Tile{X: it.X, Z: it.Z, Level: it.Level}, centre) > leash {
			continue
		}
		best = &items[i]
		break
	}
	if best == nil || FreeSlots(d) == 0 {
		return false
	}
	before := PackCount(d, antipoisonmakeplan.HornID)
	if err := combatrunner.ApproachAndTake(cd, *best, 1, leash, 20*time.Second); err != nil {
		logf(d, "antipoison: taking a horn: %v", err)
		return false
	}
	return PackCount(d, antipoisonmakeplan.HornID) > before
}

// ---- the shop and the sink ----

// ShopResult is one shop run's outcome.
type ShopResult struct {
	Vials, Filled int
	Pestle        bool
	Err           string
}

func npcNamed(d *fleetrunner.Deps, name string, near tile.Tile, radius int) (idx, typ int, ok bool) {
	best := radius + 1
	idx = -1
	for _, n := range d.Session.State().NPCs() {
		t, found := d.Config.Npc(n.Type)
		if !found || !strings.EqualFold(t.Name, name) {
			continue
		}
		if dist := tile.Chebyshev(near, tile.Tile{X: n.X, Z: n.Z, Level: n.Level}); dist <= radius && dist < best {
			idx, typ, best, ok = n.Index, n.Type, dist, true
		}
	}
	return idx, typ, ok
}

// BuyAt walks to stand, trades with the first of keepers found within 15 tiles, and buys line.N of
// line.ID, reporting how many landed in the pack.
func BuyAt(d *fleetrunner.Deps, keepers []string, stand tile.Tile, walk time.Duration, line Line) (int, error) {
	if line.N <= 0 {
		return 0, nil
	}
	if !fleetrunner.WalkTo(d, stand, 4, walk) {
		return 0, fmt.Errorf("could not reach %s's shop from %s", keepers[0], fleetrunner.HereIs(d))
	}
	idx, typ, ok := -1, 0, false
	keeper := keepers[0]
	for _, k := range keepers {
		if idx, typ, ok = npcNamed(d, k, stand, 15); ok {
			keeper = k
			break
		}
	}
	if !ok {
		return 0, fmt.Errorf("could not find %s near the shop", strings.Join(keepers, " or "))
	}
	npcType, _ := d.Config.Npc(typ)
	op := cachecfg.OpIndex(npcType.Ops, "Trade")
	if op == 0 {
		op = 3
	}
	shop := actions.NewShop(d.Session)
	if err := actions.OpNpc(d.Session, op, idx); err != nil {
		return 0, fmt.Errorf("trading with %s: %w", keeper, err)
	}
	if !fleetrunner.WaitFor(d, keeper+"'s shop to open", 6*time.Second, shop.IsOpen) {
		return 0, fmt.Errorf("%s's shop did not open", keeper)
	}
	defer func() { _ = shop.Close() }()
	stocked := false
	for _, it := range shop.Stock() {
		if it.ID == line.ID {
			stocked = true
			break
		}
	}
	if !stocked {
		return 0, fmt.Errorf("id %d is not in %s's stock", line.ID, keeper)
	}
	before := PackCount(d, line.ID)
	if err := shop.Buy(line.ID, int32(line.N)); err != nil {
		return 0, err
	}
	fleetrunner.WaitFor(d, "the purchase to land", 6*time.Second, func() bool { return PackCount(d, line.ID) >= before+line.N })
	return PackCount(d, line.ID) - before, nil
}
