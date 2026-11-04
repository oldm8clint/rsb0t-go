// Package barcrawlrunner is the I/O half of the Alfred Grimhand's Barcrawl port: it drives the
// pure card parsing and bar ordering in internal/scripts/barcrawlplan against a live session.
// Ported from the TypeScript client's src/bot/api/ai/quests/barcrawl/RunBarcrawl.ts.
//
// It reuses internal/scripts/questrunner for travel, dialogue driving and the standard runner
// boilerplate, because the barcrawl's conversations are ordinary NPC dialogues even though the
// crawl itself is not a quest-engine module.
//
// Deliberately NOT ported: the Electron wall's progress paint. The `Progress` callback is kept
// because a caller's own log wants it, but nothing here draws.
package barcrawlrunner

import (
	"fmt"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/barcrawlplan"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
	"rsb0t-go/internal/scripts/tile"
)

// Deps is questrunner's dependency set; the barcrawl needs nothing extra.
type Deps = questrunner.Deps

// readTries is how many times a Read that opens nothing is re-sent before the card is called
// unreadable.
//
// Three, and the number is load-bearing rather than arbitrary. The client clears the main modal
// on the close it sends, but the SERVER clears it a tick or more later, and an opheld arriving
// in that window is dropped with no refusal at all. Upstream's drinkAt survived this by reading
// three times while every other caller read once, called the card unreadable and failed a quest
// step — once per bar, ten bars. A send that can be dropped needs its own retry, not a caller
// who happens to have one.
const readTries = 3

// cardOpenTimeout is how long one Read gets to put the scroll up.
const cardOpenTimeout = 6 * time.Second

// cardSettleTimeout is how long the scroll's own lines get to render once it is up.
const cardSettleTimeout = 5 * time.Second

// CardCount is how many barcrawl cards the pack holds.
func CardCount(d *Deps) int {
	return heldCount(d, barcrawlplan.CardName)
}

// Coins is the coin stack in the pack.
func Coins(d *Deps) int { return heldCount(d, barcrawlplan.CoinsName) }

func heldCount(d *Deps, name string) int {
	eq := actions.NewEquipment(d.Session, d.Components)
	total := 0
	for _, it := range eq.Inventory() {
		if it.ID <= 0 {
			continue
		}
		if !nameMatches(d, int(it.ID), name) {
			continue
		}
		c := int(it.Count)
		if c < 1 {
			c = 1
		}
		total += c
	}
	return total
}

func nameMatches(d *Deps, id int, want string) bool {
	if d.Config == nil {
		return false
	}
	obj, ok := d.Config.Obj(id)
	if !ok {
		return false
	}
	return equalFold(obj.Name, want)
}

func equalFold(a, b string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := 0; i < len(a); i++ {
		ca, cb := a[i], b[i]
		if 'A' <= ca && ca <= 'Z' {
			ca += 'a' - 'A'
		}
		if 'A' <= cb && cb <= 'Z' {
			cb += 'a' - 'A'
		}
		if ca != cb {
			return false
		}
	}
	return true
}

func cardID(d *Deps) (int32, bool) {
	if d.Config == nil {
		return 0, false
	}
	for _, id := range d.Config.ObjIDs(barcrawlplan.CardName) {
		return int32(id), true
	}
	return 0, false
}

// ReadCard opens the card's scroll and parses it.
//
// ok is false when the card could not be read at all, which a caller must NOT treat as "no bars
// signed": re-walking a finished tour on a failed read is exactly the loop this distinction
// prevents. The one exception is handled inside: once every bar is green,
// quest_barcrawl.rs2's opheld1 stops opening the scroll and prints "You are too drunk to be able
// to read the barcrawl card" instead, so that message is returned as a DONE progress rather than
// as a failure.
func ReadCard(d *Deps) (barcrawlplan.Progress, bool) {
	if CardCount(d) == 0 {
		return barcrawlplan.Progress{}, false
	}
	obj, ok := cardID(d)
	if !ok {
		d.Log(fmt.Sprintf("barcrawl: this cache has no object named %q", barcrawlplan.CardName))
		return barcrawlplan.Progress{}, false
	}
	st := d.Session.State()
	eq := actions.NewEquipment(d.Session, d.Components)

	// The card's Read is an opheld, which the server refuses outright while tab 3 is not set
	// up — and actions.Equipment refuses to send it at all in that state. Reading the pack
	// works anyway (it falls back to the fixed PlayerPack component), so "the card is there"
	// is not evidence that it can be clicked.
	if !questrunner.PackVisible(d) {
		d.Log("barcrawl: cannot click the card while the backpack panel is invisible to the server")
		return barcrawlplan.Progress{}, false
	}

	// The previous read's close takes a tick to land, and a `before` sampled while that
	// scroll is still up cannot differ from the id the next Read opens, so every read after
	// a successful one would time out.
	questrunner.ClearLeftoverModal(d)
	waitQuiet(d, func() bool { m, _, _ := st.Modals(); return m == gamestate.NoModal }, 3*time.Second)
	before, _, _ := st.Modals()

	mark := chatMark(d)
	opened := false
	for attempt := 0; attempt < readTries && !opened; attempt++ {
		if attempt > 0 && !d.Delay(1800*time.Millisecond) {
			return barcrawlplan.Progress{}, false
		}
		if CardCount(d) == 0 {
			return barcrawlplan.Progress{}, false
		}
		if err := eq.InteractHeld(d.Config, obj, barcrawlplan.ReadOp); err != nil {
			d.Log(fmt.Sprintf("barcrawl: the card refused its %s op: %v", barcrawlplan.ReadOp, err))
			break
		}
		opened = waitQuiet(d, func() bool {
			m, _, _ := st.Modals()
			return m != gamestate.NoModal && m != before
		}, cardOpenTimeout)
		// A finished card answers the Read with "You are too drunk..." and opens nothing, so
		// the retry loop's exit condition never fires and a done card costs all three
		// attempts. Check the success-refusal HERE rather than only after the loop: once the
		// tour is over, every later read is a done card, and paying 3 x cardOpenTimeout for
		// each of them is the difference between a quick hand-in and a slow one. Measured on
		// the first full live tour: five "too drunk" lines for what is one question.
		if !opened && sawSince(d, mark, barcrawlplan.TooDrunk) {
			return barcrawlplan.Progress{Done: true}, true
		}
	}

	if opened {
		// The modal id arrives in if_openmain and each line in its own if_settext, and
		// component text persists between modals — so sampling on the tick the id changes
		// reads an empty scroll, or the previous read's. Wait for text that PARSES; the id
		// alone proves nothing.
		d.Delay(600 * time.Millisecond)
		waitQuiet(d, func() bool {
			_, ok := barcrawlplan.ParseCard(actions.MainModalTexts(d.Session, d.Components))
			return ok
		}, cardSettleTimeout)
		progress, parsed := barcrawlplan.ParseCard(actions.MainModalTexts(d.Session, d.Components))
		questrunner.ClearLeftoverModal(d)
		if parsed {
			return progress, true
		}
	}

	if sawSince(d, mark, barcrawlplan.TooDrunk) {
		return barcrawlplan.Progress{Done: true}, true
	}
	if opened {
		d.Log("barcrawl: the card scroll opened but never rendered a parseable line")
	} else {
		d.Log("barcrawl: the card scroll never opened, and no \"too drunk\" message came back")
	}
	return barcrawlplan.Progress{}, false
}

// chatMark takes a mark in the chat log so a later sawSince only sees lines that arrived after
// it. A nil Chat means message oracles are unavailable, which is reported by sawSince.
func chatMark(d *Deps) time.Time { return time.Now() }

// sawSince reports whether a message matching re arrived after mark.
func sawSince(d *Deps, mark time.Time, re interface{ MatchString(string) bool }) bool {
	if d.Chat == nil {
		return false
	}
	return re.MatchString(d.Chat.Since(mark))
}

func waitQuiet(d *Deps, cond func() bool, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if cond() {
			return true
		}
		if !d.Delay(100 * time.Millisecond) {
			return false
		}
	}
	return cond()
}

// barReachRadius is how close to a bar's tile the walk must get before the bartender is looked
// for. The bartenders stand behind their counters, so three tiles is the practical arrival.
const barReachRadius = 3

// drinkBudget is how long one bar's dialogue gets to take the coins.
const drinkBudget = 25 * time.Second

// signTries is how many times the card is re-read after a drink before the bar is called
// unsigned. The coin deduction says the drink was bought, but the card only turns green several
// p_delays later.
const signTries = 3

// DrinkAt buys one bar's barcrawl drink and confirms the card signed.
func DrinkAt(d *Deps, bar barcrawlplan.Bar) bool {
	if !questrunner.Travel(d, bar.Tile, barReachRadius) {
		d.Log(fmt.Sprintf("barcrawl: could not reach the %s bartender", bar.Line))
		return false
	}
	d.Delay(1200 * time.Millisecond)

	stop := questplan.NpcStop{Npc: bar.Npc, ID: bar.ID, Anchor: bar.Tile, Leash: 8, Prefer: barcrawlplan.BarPrefer}
	npc, found := questrunner.FindNpc(d, stop, 12)
	if !found {
		d.Log(fmt.Sprintf("barcrawl: no bartender %d at the %s", bar.ID, bar.Line))
		return false
	}
	obj, ok := cardID(d)
	if !ok {
		return false
	}

	// Walk adjacent: this engine runs clientRoutefinder, so a use-on sent at an
	// out-of-reach NPC is dropped with no refusal.
	at := tile.Tile{X: npc.X, Z: npc.Z, Level: npc.Level}
	if tile.Chebyshev(questrunnerTile(d), at) > 1 {
		questrunner.WalkNear(d, at, 1)
		if npc, found = questrunner.FindNpc(d, stop, 12); !found {
			return false
		}
	}

	beforeCoins := Coins(d)
	// Using an item on an NPC is an OPNPCU, gated on the same tab visibility as the card's
	// own Read.
	if !questrunner.PackVisible(d) {
		d.Log("barcrawl: cannot use the card on a bartender while the backpack panel is invisible to the server")
		return false
	}
	eq := actions.NewEquipment(d.Session, d.Components)
	// Using the card on the bartender takes the barcrawl branch straight away, skipping the
	// ordinary talk menu. Nine of the ten then go straight into the drink; the Rising Sun's
	// barmaid offers her ale list first, which is why BarPrefer still has to be driven.
	if err := eq.UseOnNpc(obj, npc.Index); err != nil {
		d.Log(fmt.Sprintf("barcrawl: use card on %s: %v", bar.Line, err))
		return false
	}

	dl := questrunner.NewDialogue(d)
	// The coin deduction is the oracle for "the drink was bought", not the dialogue ending.
	dl.DriveUntil(func() bool { return Coins(d) < beforeCoins }, barcrawlplan.BarPrefer, drinkBudget)

	for attempt := 0; attempt < signTries; attempt++ {
		if !d.Delay(3 * time.Second) {
			return false
		}
		after, ok := ReadCard(d)
		if ok && after.Signed(bar) {
			d.Log(fmt.Sprintf("barcrawl: signed at the %s (%d/%d)", bar.Line, after.SignedCount(), len(barcrawlplan.Bars)))
			return true
		}
	}
	d.Log(fmt.Sprintf("barcrawl: the %s did not sign the card", bar.Line))
	return false
}

func questrunnerTile(d *Deps) tile.Tile {
	x, z, level, _ := d.Session.State().Tile()
	return tile.Tile{X: x, Z: z, Level: level}
}

// Progress is reported once per pass so a caller's log tracks the tour.
type Progress func(signed, total int)

// giveUp is how many bars in a row may answer nothing before the tour is called broken.
const giveUp = 3

// RunTour drives the crawl until the card is fully signed. The guard still has to be told,
// which HandIn does.
func RunTour(d *Deps, onProgress Progress) bool {
	missed := 0
	for pass := 0; pass < len(barcrawlplan.Bars)*2; pass++ {
		if !questrunner.WaitForGuardian(d) {
			return false
		}
		progress, ok := ReadCard(d)
		if !ok {
			if CardCount(d) == 0 {
				d.Log("barcrawl: no barcrawl card in the pack to read")
			} else {
				// Re-walking a ten-bar tour on a failed read is far worse than
				// stopping and saying why.
				d.Log("barcrawl: holding a card but could not read it — stopping rather than re-walking the tour")
			}
			return false
		}
		if onProgress != nil {
			onProgress(progress.SignedCount(), len(barcrawlplan.Bars))
		}
		if progress.Done {
			d.Log("barcrawl: card fully signed")
			return true
		}
		next, any := barcrawlplan.NextBar(progress.Remaining, questrunnerTile(d), true)
		if !any {
			return true
		}
		d.Log(fmt.Sprintf("barcrawl: %d bars left, heading for the %s", len(progress.Remaining), next.Line))
		if DrinkAt(d, next) {
			missed = 0
		} else {
			missed++
		}
		// The next pass re-reads the card and re-sorts, so the loop moves on past one bar
		// that will not answer and comes back to it later.
		if missed >= giveUp {
			d.Log(fmt.Sprintf("barcrawl: %d bars in a row refused the card — stopping", missed))
			return false
		}
	}
	final, ok := ReadCard(d)
	return ok && final.Done
}

// guardReachRadius is how close to the gate guard the walk must get.
const guardReachRadius = 4

// guardBudget is how long the guard's conversation gets.
const guardBudget = 30 * time.Second

// guardStop is the gate guard as an NpcStop. He is found by ID: the outpost's other "Barbarian
// guard" is the attackable one and the gate loc runs the same conversation, so a talk aimed by
// display name can land on either.
var guardStop = questplan.NpcStop{
	Npc:    barcrawlplan.GuardName,
	ID:     barcrawlplan.GuardID,
	Anchor: barcrawlplan.GuardTile,
	Leash:  8,
	Prefer: barcrawlplan.GuardPrefer,
}

// TalkToGuard walks to the gate and opens the guard's conversation.
func TalkToGuard(d *Deps) bool {
	if !questrunner.Travel(d, barcrawlplan.GuardTile, guardReachRadius) {
		d.Log("barcrawl: could not reach the outpost gate guard")
		return false
	}
	d.Delay(1200 * time.Millisecond)
	if _, found := questrunner.FindNpc(d, guardStop, 12); !found {
		d.Log("barcrawl: no barbarian guard at the outpost gate")
		return false
	}
	return questrunner.OpenDialogue(d, guardStop)
}

// AskGuard opens the gate conversation and classifies it.
//
// The greeting is the verdict because %barcrawl is not on the wire and an empty pack looks the
// same before a card is issued as after one is handed in.
func AskGuard(d *Deps) barcrawlplan.GuardVerdict {
	if !TalkToGuard(d) {
		d.Log("barcrawl: the outpost guard never opened a dialogue")
		return barcrawlplan.VerdictRetry
	}
	dl := questrunner.NewDialogue(d)
	greeting := dl.Texts()
	d.Log(fmt.Sprintf("barcrawl: guard opens with: %v", greeting))
	if verdict, ok := barcrawlplan.ReadGreeting(greeting); ok && verdict == barcrawlplan.VerdictComplete {
		return barcrawlplan.VerdictComplete
	}
	dl.DriveUntil(func() bool { return CardCount(d) > 0 }, barcrawlplan.GuardPrefer, guardBudget)
	questrunner.ClearLeftoverModal(d)
	held := CardCount(d)
	d.Log(fmt.Sprintf("barcrawl: guard done — cards in pack: %d", held))
	if held > 0 {
		return barcrawlplan.VerdictIssued
	}
	if barcrawlplan.CrawlUnderway(greeting) {
		// outpost_guard_progress hands over a new card whenever neither the pack nor the bank holds one, so an
		// account that is mid-crawl, holds none and is given none has its card in the bank ("Not too bad, my
		// barcrawl card is in my bank now"). Live 13-15 Oct 2026: 2,196 such talks in the logs scanned, every one a retry.
		return barcrawlplan.VerdictBanked
	}
	return barcrawlplan.VerdictRetry
}

// DrawCardFromBank walks to the nearest bank, opens it and takes the barcrawl card out, and reports whether the pack
// holds a card afterwards. The guard issues no second card while one sits in the bank, and the engine's other quests
// and the Stage 5 trainer bank everything they do not keep between runs, so a crawl that spans two runs (or one the
// slot cut short) resumes here.
func DrawCardFromBank(d *Deps) bool {
	if CardCount(d) > 0 {
		return true
	}
	bank, ok := questrunner.OpenBankAt(d, questplan.NearestBank(questrunnerTile(d)).Tile)
	if !ok {
		d.Log("barcrawl: could not open a bank to draw the card out of")
		return false
	}
	defer func() { _ = bank.Close() }()
	return TakeCardFrom(d, bank)
}

// TakeCardFrom draws the barcrawl card out of an open bank. A full pack cannot take it, so everything but the coins
// (and what Deps.KeepInPack names) is banked first. It reports whether the pack holds a card afterwards; a bank with
// no card in it is logged, not an error: the guard's own script says that is the lost-card case.
func TakeCardFrom(d *Deps, bank *actions.Bank) bool {
	obj, ok := cardID(d)
	if !ok {
		d.Log(fmt.Sprintf("barcrawl: this cache has no object named %q", barcrawlplan.CardName))
		return false
	}
	if CardCount(d) > 0 {
		return true
	}
	if questrunner.BankCounts(bank)[int(obj)] == 0 {
		d.Log("barcrawl: the bank holds no barcrawl card")
		return false
	}
	if questrunner.FreeSlots(d) < 1 {
		d.Log("barcrawl: the pack is full: banking all but the coins to make room for the card")
		questrunner.DepositExcept(d, bank, func(id int) bool { return id == barcrawlplan.CoinsID })
	}
	got := questrunner.WithdrawExact(d, bank, int(obj), 1)
	d.Log(fmt.Sprintf("barcrawl: drew %d barcrawl card out of the bank", got))
	return CardCount(d) > 0
}

// HandIn gives the signed card to the gate guard, which is what sets %barcrawl to complete and
// therefore what opens the gate.
func HandIn(d *Deps) bool {
	if !TalkToGuard(d) {
		return false
	}
	dl := questrunner.NewDialogue(d)
	dl.DriveUntil(func() bool { return CardCount(d) == 0 }, nil, guardBudget)
	questrunner.ClearLeftoverModal(d)
	return CardCount(d) == 0
}

// Ensure is everything between "no barcrawl" and a gate that opens. It returns true once the
// guard will let the account through.
func Ensure(d *Deps, onProgress Progress) bool {
	if CardCount(d) == 0 {
		switch AskGuard(d) {
		case barcrawlplan.VerdictComplete:
			d.Log("barcrawl: the guard waves us through — the crawl is already done")
			return true
		case barcrawlplan.VerdictBanked:
			d.Log("barcrawl: the crawl is under way and the guard has no card for us — it is in the bank; drawing it")
			if !DrawCardFromBank(d) {
				d.Log("barcrawl: could not draw the card out of the bank — retrying")
				return false
			}
		case barcrawlplan.VerdictRetry:
			// Usually a random event cutting the conversation short. Fail so the
			// caller walks back and asks again, rather than starting a tour with no
			// card.
			d.Log("barcrawl: the guard handed over no card and did not wave us through — retrying")
			return false
		}
	}
	if !RunTour(d, onProgress) {
		return false
	}
	return HandIn(d)
}
