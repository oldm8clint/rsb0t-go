package mastercraftplan

// The crystal chest round (a user request). When the mule publishes MORE than 100 Crystal keys,
// the main account asks for all of them and spends them at the Taverley crystal chest:
//
//  1. at the mule's post: bank the pack, ask the mule for every Crystal key it holds (and, when
//     a teleport is castable and the bank is short, its runes; and about a thousand Strange fruit,
//     as many as it has), bank the keys;
//  2. take 6 keys a trip, with the runes for 6 Falador Teleports and 1 teleport home (Camelot, or
//     Varrock below Magic 45; none of an element a worn staff pays for), the ships' fare and 3
//     Strange fruit (chestkit.go);
//  3. go to the chest, running, eating a Strange fruit whenever run energy is low, open it with a
//     key, and take every reward: an open is only made while the pack has room for the worst reward
//     the drop table can give (lootchestfisherplan.CanOpenWithKeyInHand), so nothing is dropped on
//     the floor, and anything that still lands there is picked up;
//  4. Falador Teleport (or the walk) to the Falador west bank, bank the loot;
//  5. draw the next 6 keys there, walk back to the chest, again;
//  6. when the keys run out, the teleport home (Camelot's, or Varrock's from the Falador side: see
//     ChestHomeSpell) and the walk to Catherby, and hand every piece of loot to the mule.
//
// A round that starts with keys of its own in the bank skips the first step and goes straight to the
// trips (ChestResumes): the mule's post is for the hand-over at the end, and for the ask when the
// keys run out.
//
// It runs after every other round, just before MasterCrafter would fall back to ScarabSwarm or wait
// for the mule to restock (DecideWithChest wraps Decide rather than changing it).
//
// THE TELEPORTS NEED MAGIC 37 (Falador) AND 45 (Camelot), 25 for Varrock, scarabplan.Teleports.
// the main account was Magic 17 on 4 Oct 2026, so then every leg was walked: the long legs between Catherby and
// Falador on ScarabSwarm's wolf-free route (the ships, then the spirit trees), with no keys in the
// pack (the keys are banked at the post and drawn at Falador; the bank is one bank). Each leg
// switches to its teleport by itself once Magic allows it. At Magic 39 and 40 (5 and 6 Oct) that
// is the Falador Teleport to the chest and back, and Varrock Teleport to start the way home: Camelot
// Teleport at 45 would land 55 tiles from the post and take the walk home from about 8 minutes to one.
//
// What is reused, not rewritten: LootChestFisher's chest tiles, its drop table, its free-slot rule
// and its 6-key trip (lootchestfisherplan); its chest open and its Falador bank (the bot uses
// lootchestfisherrunner); the mule's key gate (lootchestfisherplan.Request: all of them, up to the
// mule's own per-order cap); the spells' runes (scarabplan.Teleports).

import (
	"sort"
	"strconv"
	"time"

	"rsb0t-go/internal/scripts/fleetplan"
	"rsb0t-go/internal/scripts/lootchestfisherplan"
)

// ModeChest is a crystal chest round.
const ModeChest Mode = "chest"

// KeyCrystalChest switches the chest round on (default on).
const KeyCrystalChest = "craftCrystalChest"

func init() { knownKeys[KeyCrystalChest] = true }

const (
	// ChestKeyItem is the key's name as the mule publishes and orders it.
	ChestKeyItem = "Crystal key"
	// ChestKeyThreshold is the mule's own gate (orderplan.KeysFloor, the owner's "more than 100"):
	// it hands out only the keys above it.
	ChestKeyThreshold = lootchestfisherplan.MuleKeyThreshold
	// ChestStartKeys is the DEFAULT of craftChestStartKeys (1,000 since 5 Nov 2026; settings.Sched) - the keys the mule's bank must hold before a chest round starts (owner, 6 Oct
	// 2026: "crystalchest should require 400 keys in the mule's bank to trigger"; more than 100
	// before).
	ChestStartKeys = 400
	// ChestKeysPerTrip is LootChestFisher's measured six keys a trip.
	ChestKeysPerTrip = lootchestfisherplan.KeysPerTrip
	// ChestFaladorCasts and ChestHomeCasts are the teleports' runes carried (owner: "the runes for 6
	// Falador Teleports and 1 Camelot Teleport"): one cast of the way home's teleport, Camelot's
	// or, below Magic 45, Varrock's (ChestHomeSpell). ChestCamelotCasts is the older name for it.
	ChestFaladorCasts = 6
	ChestHomeCasts    = 1
	ChestCamelotCasts = ChestHomeCasts
	// FaladorTeleport, CamelotTeleport and VarrockTeleport are the spells' names (scarabplan.Teleports).
	FaladorTeleport = "Falador Teleport"
	CamelotTeleport = "Camelot Teleport"
	VarrockTeleport = "Varrock Teleport"
	// ChestFares is the coins a trip keeps in the pack for the ships of the wolf-free route
	// between Catherby and Falador, both ways (scarabplan.ShipFares is one way).
	ChestFares = 2 * fleetplan.ShipFares
)

// ChestLedger is what the chest round remembers across rounds and restarts (Ledger.Chest).
type ChestLedger struct {
	// Owed is the chest's loot not yet handed to the mule, by bank object id (a note counted as
	// the item it stands for), so a failed hand-over is made good by the next chest round. Only
	// what the chest gave is owed: the account's own coins and runes in the same bank stay its own.
	Owed map[string]int `json:"owed,omitempty"`
	// PauseUntil holds chest rounds off after one the mule could not fund.
	PauseUntil time.Time `json:"pauseUntil,omitempty"`
	// KeysBank is the Crystal keys this account's bank held when a visit last read it. A restart
	// knows nothing of the bank until a booth opens, and the round has to resume from the keys it
	// left there (ChestOwnKeys); the next reading corrects a wrong one.
	KeysBank int `json:"keysBank,omitempty"`
	// Lifetime counters.
	Rounds  int `json:"rounds,omitempty"`
	Opened  int `json:"opened,omitempty"`
	Handed  int `json:"handed,omitempty"`
	KeysGot int `json:"keysGot,omitempty"`
}

// RemoveOwed takes n of the bank object id off what is owed (an item the round cast High Level Alchemy on is
// gone, and owing it would ask for it forever), never below zero.
func (c *ChestLedger) RemoveOwed(id, n int) {
	k := strconv.Itoa(id)
	if n <= 0 || c.Owed[k] <= 0 {
		return
	}
	if c.Owed[k] -= n; c.Owed[k] <= 0 {
		delete(c.Owed, k)
	}
	if len(c.Owed) == 0 {
		c.Owed = nil
	}
}

// AddOwed records n of the bank object id as owed to the mule.
func (c *ChestLedger) AddOwed(id, n int) {
	if n <= 0 || id <= 0 {
		return
	}
	if c.Owed == nil {
		c.Owed = map[string]int{}
	}
	c.Owed[strconv.Itoa(id)] += n
}

// OwedIDs is the owed loot by object id, ids ascending.
func (c *ChestLedger) OwedIDs() (ids []int, owed map[int]int) {
	owed = map[int]int{}
	for k, n := range c.Owed {
		id, err := strconv.Atoi(k)
		if err != nil || n <= 0 {
			continue
		}
		owed[id] += n
	}
	for id := range owed {
		ids = append(ids, id)
	}
	sort.Ints(ids)
	return ids, owed
}

// OwedTotal is every owed unit.
func (c *ChestLedger) OwedTotal() int {
	n := 0
	for _, v := range c.Owed {
		if v > 0 {
			n += v
		}
	}
	return n
}

// Settle takes what crossed to the mule (by object id) off the owed loot, and caps the rest at
// what the bank holds (banked nil: not read, nothing capped): loot that is no longer there (runes a
// teleport spent, gems a gem round cut) cannot be handed over, and owing it would ask for it
// forever.
func (c *ChestLedger) Settle(handed map[int]int, banked func(id int) int) {
	_, owed := c.OwedIDs()
	out := map[string]int{}
	for id, n := range owed {
		n -= max(0, handed[id])
		if banked != nil {
			n = min(n, max(0, banked(id)))
		}
		if n > 0 {
			out[strconv.Itoa(id)] = n
		}
	}
	if len(out) == 0 {
		out = nil
	}
	c.Owed = out
}

// ChestInput is what the chest decision reads besides the Snapshot.
type ChestInput struct {
	// OwnKeys is the Crystal keys this account holds of its own, in its bank (as last seen: the
	// ledger remembers it across a restart) and its pack (ChestOwnKeys): keys a round did not spend
	// (its time ran out, a leg failed, the script was stopped) are spent by the next one, whatever
	// the mule holds.
	OwnKeys int
	// OnFaladorSide is whether the account stands on the Falador side of the run (the bank, the road
	// and the chest: lootchestfisherrunner.OnFaladorSide), where a round it was in the middle of
	// left it.
	OnFaladorSide bool
}

// ChestReady reports whether the mule's published key count starts a chest round: ChestStartKeys or
// more.
func ChestReady(muleKeys int) bool { return muleKeys >= ChestStartKeys }

// DecideWithChest is Decide with the chest round after every other round: when Decide would stay
// on (or go back to) ScarabSwarm with the mule here and nothing held off, a chest round runs if
// the mule holds ChestStartKeys keys, this account's bank still holds keys, or loot from an earlier
// chest round is still owed. Gems, pearls, bows and a pending hand-over all come first.
//
// One exception: a round that was under way carries on first. An account on the Falador side with
// keys of its own was in the middle of the trips when the script stopped (a user request: "it
// needs to remember what it was doing last time the script was running and continue that ... before
// just heading to the mule upon startup"), and a new round would walk it to the mule's post and back;
// only a pending hand-over, which is the mule's, still comes before it.
func DecideWithChest(set Settings, s Snapshot, in ChestInput) Decision {
	d := DecideRotating(set, s)
	if !set.CrystalChest || d.GaveUp || !s.MuleHeard || !s.MuleSameWorld {
		return d
	}
	led := s.Ledger
	if led == nil {
		led = &Ledger{}
	}
	c, why := decideChest(set, s, led, in)
	if c.Mode == ModeChest && in.OwnKeys > 0 && in.OnFaladorSide && d.Mode != ModeDeliver {
		c.Why += " (under way: the account is on the Falador side)"
		return c
	}
	// Chest loot still owed to the mule, or keys of its own left in the bank, come before a NEW Runecrafting
	// round (owner's design: a levelling round does not pre-empt what is owed to the mule); one under way
	// (d.Resume) carries on first.
	if (d.Mode == ModeRunecraft || d.Mode == ModeNature) && !d.Resume && c.Mode == ModeChest && c.Resume {
		c.Why += " (owed or left over: it comes before a new " + string(d.Mode) + " round)"
		return c
	}
	// THE CHEST IS A SCHEDULED ROUND (a user request: "triggers when the mule holds 1k+ crystal keys; loot them all"). It ran after
	// every other round, so the back-to-back trawler blocks and the rounds due at every check kept it off for days. Now it comes
	// ahead of every NEW round (not a round under way, a hand-over, a campaign's next round or the trawler, which ends in hours).
	if c.Mode == ModeChest && !c.Resume && d.Mode != ModeSwarm && d.Mode != ModeTrawler && !d.Resume && !d.Continue {
		c.Why += " (the keys are due: it comes before a new " + string(d.Mode) + " round)"
		return c
	}
	if d.Mode != ModeSwarm {
		return d
	}
	if c.Mode == ModeChest {
		return c
	}
	d.Why += "; " + why
	return d
}

func decideChest(set Settings, s Snapshot, led *Ledger, in ChestInput) (Decision, string) {
	if until, ok := led.BackedOff(ModeDeliver, s.Now); ok {
		return Decision{}, "chest rounds wait for the hand-over backoff (until " + until.Format("15:04") + ")"
	}
	if s.Now.Before(led.Chest.PauseUntil) {
		return Decision{}, "chest rounds are paused until " + led.Chest.PauseUntil.Format("15:04")
	}
	if until, ok := led.BackedOff(ModeChest, s.Now); ok {
		return Decision{}, "chest rounds are backing off until " + until.Format("15:04") + " after " + strconv.Itoa(led.Fails(ModeChest)) + " failed in a row"
	}
	if n := led.Chest.OwedTotal(); n > 0 {
		return Decision{Mode: ModeChest, Resume: true, Why: strconv.Itoa(n) + " item(s) of crystal chest loot from an earlier round are still owed to the mule"}, ""
	}
	if in.OwnKeys > 0 {
		return Decision{Mode: ModeChest, Resume: true, Why: "this account's bank holds " + strconv.Itoa(in.OwnKeys) + " Crystal key(s) an earlier chest round did not spend"}, ""
	}
	if s.Stock == nil {
		return Decision{}, "chest: the mule has not published its bank"
	}
	n := stockOf(s.Stock, ChestKeyItem)
	if ChestReadyFor(set, n) {
		return Decision{Mode: ModeChest, Why: "the mule holds " + strconv.Itoa(n) + " Crystal keys (threshold " + strconv.Itoa(set.Sched.ChestStartKeys) + ")"}, ""
	}
	return Decision{}, "the mule holds " + strconv.Itoa(n) + " Crystal keys, under the threshold " + strconv.Itoa(set.Sched.ChestStartKeys)
}

// ---- the kit ----

// ChestCastable is which of the round's teleports Magic allows (scarabplan.Teleports' levels).
func ChestCastable(magic int) (falador, camelot bool) {
	for _, tp := range fleetplan.Teleports {
		switch tp.Spell.Name {
		case FaladorTeleport:
			falador = magic >= tp.Spell.Level
		case CamelotTeleport:
			camelot = magic >= tp.Spell.Level
		}
	}
	return falador, camelot
}

// ChestSpellLevel is the Magic level the named teleport needs (0 for a spell the round does not use).
func ChestSpellLevel(spell string) int {
	for _, tp := range fleetplan.Teleports {
		if tp.Spell.Name == spell {
			return tp.Spell.Level
		}
	}
	return 0
}

// ChestHomeSpell is the teleport that starts the way home from the Falador side (a user request:
// "the main account doesnt use camelot teleport to return to the mule ... walked from falador all the way to the
// spirit tree"): Camelot Teleport (Magic 45) lands 68 route cost from the mule's post, against the 697
// of the walk from the Falador west bank (planned over the real collision pack and graph); below 45,
// Varrock Teleport (Magic 25) lands at Varrock, where the same spirit-tree route has only 539 left, a
// walk of about 2 minutes less (the spirit tree it crosses to Tree Gnome Village is the same one). ""
// when neither is castable: the whole way home is walked.
func ChestHomeSpell(magic int) string {
	if _, cam := ChestCastable(magic); cam {
		return CamelotTeleport
	}
	if lvl := ChestSpellLevel(VarrockTeleport); lvl > 0 && magic >= lvl {
		return VarrockTeleport
	}
	return ""
}

// ChestHomeSpellFrom is ChestHomeSpell for an account that stands where it is: Camelot Teleport lands
// by the post wherever the account is, but Varrock Teleport lands EAST of White Wolf Mountain, a gain
// only from the Falador side (the bank, the road, the chest) and a long way backwards from anywhere
// west of it.
func ChestHomeSpellFrom(magic int, onFaladorSide bool) string {
	spell := ChestHomeSpell(magic)
	if spell == VarrockTeleport && !onFaladorSide {
		return ""
	}
	return spell
}

// ChestRunes is the runes the round carries: ChestFaladorCasts Falador Teleports and ChestHomeCasts
// of the way home's teleport (ChestHomeSpell), only the spells Magic allows, and none of a rune paid
// by a worn staff (paid: RunesPaidByStaff). Empty when no spell is castable: every leg is walked.
func ChestRunes(magic int, paid map[string]bool) map[string]int {
	fal, _ := ChestCastable(magic)
	home := ChestHomeSpell(magic)
	out := map[string]int{}
	for _, tp := range fleetplan.Teleports {
		casts := 0
		switch {
		case tp.Spell.Name == FaladorTeleport && fal:
			casts = ChestFaladorCasts
		case tp.Spell.Name == home:
			casts = ChestHomeCasts
		}
		for _, r := range tp.Spell.Runes {
			if casts > 0 && !paid[r.Name] {
				out[r.Name] += r.Count * casts
			}
		}
	}
	return out
}

// ChestSpellRunes is one cast's runes of the named teleport.
func ChestSpellRunes(spell string) map[string]int {
	out := map[string]int{}
	for _, tp := range fleetplan.Teleports {
		if tp.Spell.Name == spell {
			for _, r := range tp.Spell.Runes {
				out[r.Name] += r.Count
			}
		}
	}
	return out
}

// CanCastWith reports whether held pays one cast of the named teleport at Magic magic, a rune paid
// by a worn staff (paid) needing none in the pack.
func CanCastWith(spell string, magic int, paid map[string]bool, held func(rune string) int) bool {
	if lvl := ChestSpellLevel(spell); lvl == 0 || magic < lvl {
		return false
	}
	need := ChestSpellRunes(spell)
	if len(need) == 0 {
		return false
	}
	for name, n := range need {
		if paid[name] {
			continue
		}
		if held == nil || held(name) < n {
			return false
		}
	}
	return true
}

// ChestKeysToDraw is how many keys a bank visit draws: the trip topped up to six from the bank, as
// LootChestFisher does, but none at the mule's post unless the account can teleport from there:
// the long walk to Falador is made with no keys in the pack (the bank is one bank, so they are
// drawn at Falador instead).
func ChestKeysToDraw(atPost bool, magic, banked, held int) int {
	if fal, _ := ChestCastable(magic); atPost && !fal {
		return 0
	}
	return lootchestfisherplan.KeysToWithdraw(banked, held)
}

// ChestAsk is the kit to ask the mule for at the start of a chest round: all of its Crystal keys
// (lootchestfisherplan.Request: nothing at or under the mule's gate, else the whole stock up to
// its per-order cap; a note-sized hand-out takes one pack slot), the kit's runes the account's
// own bank is short of when the mule publishes them (none of an element a worn staff pays for:
// paid), and Strange fruit up to a thousand in the account's bank, as many as the mule publishes
// (ChestFruitWanted). The round asks for all of it as notes (one pack slot a line).
//
// own is how many of an item this account's bank holds, by name.
func ChestAsk(stock Stock, magic int, paid map[string]bool, own func(item string) int) []Line {
	ownOf := func(item string) int {
		if own == nil {
			return 0
		}
		return own(item)
	}
	var lines []Line
	if q := lootchestfisherplan.Request(stockOf(stock, ChestKeyItem), lootchestfisherplan.PackSlots); q > 0 {
		lines = append(lines, Line{Item: ChestKeyItem, Qty: q})
	}
	runes := ChestRunes(magic, paid)
	names := make([]string, 0, len(runes))
	for n := range runes {
		names = append(names, n)
	}
	sort.Strings(names)
	for _, name := range names {
		short := runes[name] - ownOf(name)
		if short <= 0 {
			continue
		}
		if q := min(short, stockOf(stock, name)); q > 0 {
			lines = append(lines, Line{Item: name, Qty: q})
		}
	}
	if q := ChestFruitWanted(stockOf(stock, ChestFruit), ownOf(ChestFruit)); q > 0 {
		lines = append(lines, Line{Item: ChestFruit, Qty: q})
	}
	return lines
}

// ---- the round's order ----

// ChestPlace is where the account is, as the round's steps see it.
type ChestPlace int

// The places.
const (
	// ChestAtOther is anywhere else: on a walk, or where a teleport landed.
	ChestAtOther ChestPlace = iota
	// ChestAtPost is the mule's post (its bank, Catherby).
	ChestAtPost
	// ChestAtFalador is the Falador west bank.
	ChestAtFalador
	// ChestAtChest is the chest's own standing tile or next to it.
	ChestAtChest
)

// ChestStep is the next thing the round does.
type ChestStep string

// The steps.
const (
	// ChestStepAsk banks the pack at the post, reads the bank and asks the mule for its keys.
	ChestStepAsk ChestStep = "ask"
	// ChestStepBank is a bank visit: everything but the kit (keys, the kit's runes, the fare) is
	// banked, and the keys and runes topped up (ChestKeysToDraw).
	ChestStepBank ChestStep = "bank"
	// ChestStepToChest goes to the chest: from the post, a Falador Teleport and the walk; else the
	// walk.
	ChestStepToChest ChestStep = "to-chest"
	// ChestStepOpen opens the chest with a key and takes the reward.
	ChestStepOpen ChestStep = "open"
	// ChestStepToFalador goes to the Falador west bank: a Falador Teleport when one is castable and
	// paid for, else the walk.
	ChestStepToFalador ChestStep = "to-falador"
	// ChestStepHome goes back to the mule's post: everything is banked first, then the teleport
	// home (ChestHomeSpellFrom) and the walk to Catherby, or the walk.
	ChestStepHome ChestStep = "home"
	// ChestStepHandover hands the owed loot to the mule.
	ChestStepHandover ChestStep = "handover"
	// ChestStepAlch casts High Level Alchemy at the chest on a piece of its loot (a cut gem, the shield, the
	// legs), when that makes room for the next open: the chest cannot be looted again until a slot is free.
	ChestStepAlch ChestStep = "alch"
	// ChestStepEat eats a Strange fruit at the chest to empty its slot, when that makes room for
	// the next open (the fruit is carried for the run to the chest; one left over would cost the
	// pack a slot of loot).
	ChestStepEat ChestStep = "eat"
	// ChestStepDone ends the round.
	ChestStepDone ChestStep = "done"
)

// ChestView is what NextChestStep reads.
type ChestView struct {
	// Asked is set once the ask at the post has been made this round.
	Asked bool
	// PostBanked is set once the bank at the post was visited after the ask.
	PostBanked bool
	Where      ChestPlace
	// KeysPack is the unnoted keys in the pack, KeysNoted the noted ones (a bank visit turns them
	// into keys), KeysBank the keys in the bank when last read.
	KeysPack, KeysNoted, KeysBank int
	// Room is the free-slot rule (ChestRoom) on the pack as it is.
	Room bool
	// LootInPack is set when the pack holds anything but the kit.
	LootInPack bool
	// Expired is the round's time used up: no more trips, home with what there is.
	Expired bool
	// HandedOver is set once the hand-over at the end was made.
	HandedOver bool
	// Fruit is the unnoted Strange fruit in the pack; EatForRoom is set when the pack has no room for
	// the next open but would have it once those were eaten (ChestRoomWithFruit): at the chest, the
	// fruit are eaten first.
	Fruit      int
	EatForRoom bool
	// Alchables is the loot in the pack that can be alched (ChestAlchable, unnoted), AlchReady whether a
	// cast can go (Magic 55, a Nature rune, Fire runes or a Staff of fire, the setting on and no refusal
	// this round), and AlchForRoom whether the pack has no room for the next open but would have it once
	// those were cast away (ChestRoomWithAlch).
	Alchables              int
	AlchReady, AlchForRoom bool
	// Resumed is set for a round that started from keys of its own, without going to the mule first
	// (ChestResumes); MuleKeysReady that the mule publishes keys enough for a round of its own
	// (ChestReady). Once the hand-over is made, a resumed round asks for them: it never did.
	Resumed, MuleKeysReady bool
}

// NextChestStep is the round's order: ask; bank at the post; then trips of (to the chest, open
// while there are keys and room, to Falador, bank and draw the next keys) until the keys run out;
// then home and the hand-over. It is a function of where the account is and what it holds, so a
// step that fails is simply tried again from where it left the account.
func NextChestStep(v ChestView) ChestStep {
	switch {
	case v.HandedOver:
		if v.Resumed && v.MuleKeysReady && !v.Expired {
			return ChestStepAsk
		}
		return ChestStepDone
	case !v.Asked:
		return ChestStepAsk
	}
	atBank := v.Where == ChestAtPost || v.Where == ChestAtFalador
	if atBank && (v.KeysNoted > 0 || (v.Where == ChestAtPost && !v.PostBanked)) {
		return ChestStepBank
	}
	if v.Expired {
		switch {
		case v.Where == ChestAtPost:
			return ChestStepHandover
		case (v.LootInPack || v.KeysPack > 0) && v.Where != ChestAtFalador:
			return ChestStepToFalador
		}
		return ChestStepHome
	}
	if v.KeysPack > 0 && v.Room {
		switch {
		case v.Where == ChestAtChest:
			return ChestStepOpen
		case v.LootInPack && atBank:
			return ChestStepBank
		}
		return ChestStepToChest
	}
	if v.KeysPack > 0 && v.AlchForRoom && v.AlchReady && v.Where == ChestAtChest {
		return ChestStepAlch
	}
	if v.KeysPack > 0 && v.EatForRoom && v.Where == ChestAtChest {
		return ChestStepEat
	}
	if v.KeysPack > 0 || v.LootInPack {
		if atBank {
			return ChestStepBank
		}
		return ChestStepToFalador
	}
	if v.KeysBank > 0 {
		if v.Where == ChestAtFalador {
			return ChestStepBank
		}
		return ChestStepToFalador
	}
	if v.Where == ChestAtPost {
		return ChestStepHandover
	}
	return ChestStepHome
}

// ---- the loot ----

// ChestDropIDs is every object id the chest can give (lootchestfisherplan.Table, the uncut
// dragonstone every open adds, and the other key half and the plateskirt the table models by
// their twins), in both forms the pack can hold them.
func ChestDropIDs() map[int]bool {
	out := map[int]bool{
		lootchestfisherplan.UncutDragonstone: true,
		lootchestfisherplan.KeyHalf2:         true,
		lootchestfisherplan.RunePlateskirt:   true,
		// The ores the table gives as notes, and their items.
		lootchestfisherplan.NotedIronOre - 1: true,
		lootchestfisherplan.NotedCoal - 1:    true,
	}
	for _, b := range lootchestfisherplan.Table() {
		for _, r := range b.Rewards {
			out[r.ID] = true
		}
	}
	return out
}

// GroundStack is one stack lying on the ground.
type GroundStack struct {
	ID, Count, X, Z, Level int
}

// NewChestDrops is the chest's rewards that landed on the ground at the standing tile in an open:
// stacks of a drop id at stand in after that were not in before (content's inv_add drops what does
// not fit at the player's feet). They are picked up, so nothing is left behind even if the
// free-slot rule was ever wrong.
func NewChestDrops(before, after []GroundStack, standX, standZ, standLevel int) []GroundStack {
	drops := ChestDropIDs()
	seen := map[GroundStack]int{}
	for _, g := range before {
		seen[g]++
	}
	var out []GroundStack
	for _, g := range after {
		if seen[g] > 0 {
			seen[g]--
			continue
		}
		if g.X == standX && g.Z == standZ && g.Level == standLevel && drops[g.ID] {
			out = append(out, g)
		}
	}
	return out
}
