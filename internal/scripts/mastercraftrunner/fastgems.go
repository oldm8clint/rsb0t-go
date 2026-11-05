package mastercraftrunner

// THE FAST RUN: whole loads in whole ticks, with no wait for a reply anywhere inside the cycle
// (a user request: "build the 6-tick gem loop that sends packets blind and accepts that no real
// client sends that pattern"; of the pearls: "can we make sure the main account will do the same to the oyster
// pearls? at this speed?"; "make sure we speed up bow stringing while i have manual mode on too").
// One run serves every such job, through a spec: uncut gems into cut gems (FastGems), oyster pearls
// into Pearl bolttips (FastPearls), both six ticks a load, and bows strung (FastBows, four ticks a
// load of 14). The stream and every engine fact it rests on are mastercraftplan's (fast.go); this is
// the sending, the pacing and the checks.
//
//   - It starts from a primed quick bank (QuickBank.Prime: the booth op went out behind the last
//     load's last burst, or by itself), and waits for the bank to show. That one wait is the run's
//     only round trip: the bank's own inventory gives the items' bank slots and counts, the side
//     panel the pack, and the tick that opened it is the run's tick boundary.
//   - Each chunk of five events goes out as ONE Send (one websocket frame, buffered whole by the
//     engine), a few chunks ahead of the tick the server is on, one more every tick: the server's
//     own five-a-tick read sets the pace, never a round trip. The booth op is the fifth event of its
//     chunk (mastercraftplan.FastStream), so the chunk boundary is the tick boundary.
//   - Every tick it reads the skill's XP: the uses it shows must keep up with the chunks the server
//     has had time to read (fastSlack ticks of slack). Two ticks behind in a row is a DESYNC (the
//     bank did not open, a deposit or a withdrawal was refused, the uses named empty slots, a stray
//     click put the chunks off the tick boundary): it stops sending, lets the server drain what it
//     has, and hands back to the caller, whose next visit banks everything and draws again from
//     whatever state the pack is in.
//   - A refusal in the chat, a stop, the round's budget or a random event (the caller's hold, read
//     every tick) or another caller's movement hold stops it the same way.
//   - A planned end (the bank's stock, the run's length, a level that would be gained in a booth
//     tick) sends the last load without its booth op, waits for every use, and checks the pack.
//
// THE PEARL LOAD IS THE GEM LOAD, event for event. A cut gem takes the slot its uncut gem left; the
// Pearl bolttips stack (skill_fletching/configs/bolts/bolts.obj: stackable=yes), so the first use of
// a load puts the stack in the slot its pearl left (engine Inventory.add: a new stack goes to
// nextFreeSlot, the lowest empty slot) and every later use adds to it and leaves its own slot empty.
// Either way the uses, named lowest first, each find their item still in the slot named (a use
// removes the lowest slot holding it: Inventory.remove), and the load ends with the product in the
// first slot that is not the chisel's: one Deposit-All empties the pack but the chisel, and
// Withdraw-All refills every other slot. Keeping the tips in the pack instead would save the deposit
// but cost a pearl (26 free slots) and leave 28 events a load, which does not fit whole ticks of five
// with the booth op last; so the load stays 30 events and 27 pearls.
//
// THE BOW LOAD (skill_fletching/scripts/bows.rs2 [label,string_bow]: inv_del the bow, inv_del a
// string, inv_add the strung bow, no delay) has no tool. It starts from the last load's 14 strung bows
// in slots 0..13 and takes the owner's order (6 Oct 2026): Withdraw-All of the strings, which fills
// exactly the 14 free slots (14..27); Deposit-All of the strung bows, which empties 0..13; Withdraw-All
// of the unstrung bows, which fills them. Then use k names the bow in slot k and the string in slot
// 14+k: the bow is the lowest unstrung bow, the string the lowest string, and the strung bow goes to
// the lowest empty slot, k. 17 events, and two CLOSE_MODAL fillers before the booth op make 20, four
// ticks, with no Withdraw-X and no count dialog (the old order was Deposit-All, Withdraw-X and its
// count, Withdraw-All). From an empty pack (the first load of a run) Withdraw-All of the strings would
// take all 28 slots and there is nothing to deposit, so those three events are Withdraw-X of the 14
// bows and its count, then Withdraw-All of the strings (the 14 slots left): bows in 0..13, strings in
// 14..27, the same layout.

import (
	"fmt"
	"regexp"
	"strconv"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/anticheat"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/packet"
	"rsb0t-go/internal/protocol/clientprot"
	"rsb0t-go/internal/scripts/bowstringrunner"
	"rsb0t-go/internal/scripts/fleetrunner"
	"rsb0t-go/internal/scripts/gemplan"
	"rsb0t-go/internal/scripts/gemrunner"
	"rsb0t-go/internal/scripts/mastercraftplan"
	"rsb0t-go/internal/scripts/pearlplan"
)

// fastLookahead is how many chunks the run keeps sent beyond the tick the server is on: enough
// for a late send or a slow tick never to leave the server a tick with nothing to read.
const fastLookahead = 3

// fastSlack is how many ticks the uses may trail the chunks the server has had time to read before
// the run counts a tick as behind (the trip back, and a stat that arrives after its tick's
// PLAYER_INFO).
const fastSlack = 2

// FastOptions shapes one fast run.
type FastOptions struct {
	// MaxLoads bounds the run (the bank's stock and the next level bound it too).
	MaxLoads int
	// Hold runs every tick; false stops the run (a stop, the round's budget, a random event).
	Hold func() bool
}

// FastResult is what a fast run did.
type FastResult struct {
	// Loads is the whole loads the uses came to, Cuts the uses (from the skill's XP), XP the XP,
	// Planned the loads the run set out to make, Chunks the chunks sent, Ticks the server ticks it
	// spanned.
	Loads, Cuts, XP, Planned, Chunks, Ticks int
	// Reason is "done", "not-fast" (the bank opened but the run could not start: the visit was made
	// the 7-tick way instead), "no-open", "desync", "held", "refused", "members" or "stopped".
	Reason       string
	RefusedLevel int
	Why          string
	// Short is a "not-fast" for want of stock: the bank cannot fund a whole load.
	Short bool
	// Fallback is FastGems' 7-tick quick visit when the run could not start; Load FastPearls' and
	// FastBows'.
	Fallback *GemVisit
	Load     *LoadVisit
	Took     time.Duration
}

// batch is a Session that collects packets and sends them as one message.
type batch struct {
	s      actions.Session
	builds []func(w *packet.Packet)
}

func (b *batch) Send(build func(w *packet.Packet)) error {
	b.builds = append(b.builds, build)
	return nil
}
func (b *batch) State() *gamestate.State     { return b.s.State() }
func (b *batch) Cheats() *anticheat.Counters { return b.s.Cheats() }

func (b *batch) flush() error {
	builds := b.builds
	b.builds = nil
	if len(builds) == 0 {
		return nil
	}
	return b.s.Send(func(w *packet.Packet) {
		for _, f := range builds {
			f(w)
		}
	})
}

// closeModal is a raw CLOSE_MODAL, a handled user event that changes nothing the stream reads
// (actions.CloseModal also marks the client's modals shut, which a packet sent ahead must not).
func closeModal(s actions.Session) error {
	return s.Send(func(w *packet.Packet) { w.P1Enc(clientprot.CLOSE_MODAL) })
}

// fastSpec is what a fast run does.
type fastSpec struct {
	// what names the work for a log.
	what   string
	stream mastercraftplan.FastStream
	// stat is the skill's UPDATE_STAT index and xpTenths one use's experience in content tenths.
	stat, xpTenths int
	refused        func(said string) (string, int)
	// never, when set, is why a run can never start (a gem that can be smashed).
	never string
	// prepare reads the pack (the side panel) and the bank as the bank opened: why the run cannot
	// start ("" when it can) and the most loads the bank funds. It readies send.
	prepare func(pack, bank []gamestate.SlotItem) (loads int, why string)
	// useXP, when set, is read after prepare for the experience one use pays (a run whose item is chosen from the bank: bones).
	useXP func() int
	// send adds one event's packets to b (the run itself sends the booth op and the filler).
	send func(b actions.Session, ev mastercraftplan.FastEvent, invCom, sideCom int) error
	// done says whether a pack is a finished load: the planned end's check.
	done func(pack []gamestate.SlotItem) (bool, string)
	// keep and draw are the 7-tick visit's when the run cannot start (draw nil: bank only).
	keep func(id int32) bool
	draw func(bank []gamestate.SlotItem) []int32
}

func (s fastSpec) xp(d *fleetrunner.Deps) int {
	if sk, ok := d.Session.State().Skill(s.stat); ok {
		return sk.XP
	}
	return 0
}

// fastRun is the run itself, for any spec (see the file comment). When the run cannot start, the
// visit is made the 7-tick way (the deposit, the spec's draw, the close) and returned for the caller.
func fastRun(d *fleetrunner.Deps, q *QuickBank, s fastSpec, o FastOptions) (r FastResult, fallback *QuickVisit) {
	start := time.Now()
	defer func() { r.Took = time.Since(start) }()
	var qv QuickVisit
	bank, ok := q.awaitOpen(d, &qv, start)
	if !ok {
		r.Reason, r.Why = "no-open", qv.Err
		return r, nil
	}
	// Can the run start? Else make this visit the 7-tick way.
	why := s.never
	stock := 0
	if why == "" {
		stock, why = s.prepare(bank.Inventory(), qv.AtOpen)
		if s.useXP != nil {
			s.xpTenths = s.useXP()
		}
	}
	xp0 := s.xp(d)
	if why == "" {
		r.Planned = s.stream.Loads(min(o.MaxLoads, stock), xp0, s.xpTenths)
		if r.Planned < 1 {
			why, r.Short = "the bank holds less than a load", stock < 1
		}
	}
	if why != "" {
		q.finish(d, bank, &qv, s.keep, s.draw)
		r.Reason, r.Why = "not-fast", why
		return r, &qv
	}

	// The run.
	gd := GemDeps(d)
	st := d.Session.State()
	invCom := actions.NewEquipment(d.Session, d.Components).InventoryComponent()
	sideCom := q.sideCom
	at := time.Now()
	tick0 := st.Tick()
	defer func() { r.Ticks = st.Tick() - tick0 }()
	total := s.stream.Chunks(r.Planned)
	totalUses := r.Planned * s.stream.Uses
	uses := func() int { return mastercraftplan.FastCutsSeen(s.xp(d)-xp0, s.xpTenths) }
	defer func() {
		r.Cuts = min(uses(), totalUses)
		r.XP = s.xp(d) - xp0
		r.Loads = r.Cuts / s.stream.Uses
	}()
	send := func(k int) error {
		b := &batch{s: d.Session}
		for _, ev := range s.stream.ChunkAt(k, r.Planned) {
			var err error
			switch ev.Kind {
			case mastercraftplan.FastBooth:
				if actions.MovementHeldBy(d.Session) {
					return fmt.Errorf("another caller holds this account's movement")
				}
				err = actions.OpLoc(b, q.booth.Op, q.booth.X, q.booth.Z, q.booth.ID)
			case mastercraftplan.FastPad:
				err = closeModal(b)
			default:
				err = s.send(b, ev, invCom, sideCom)
			}
			if err != nil {
				return err
			}
		}
		return b.flush()
	}
	// drain lets the server read what it was sent, then waits for the uses to stop changing.
	drain := func() {
		still, last := 0, uses()
		for i := 0; i < fastLookahead+8 && still < 3; i++ {
			if !gemrunner.NextTick(gd, st.Tick()) {
				return
			}
			if c := uses(); c == last {
				still++
			} else {
				still, last = 0, c
			}
		}
	}
	stop := func(reason, why string) (FastResult, *QuickVisit) {
		r.Reason, r.Why = reason, why
		drain()
		return r, nil
	}
	for r.Chunks < min(fastLookahead, total) {
		if err := send(r.Chunks); err != nil {
			return stop("desync", "sending: "+err.Error())
		}
		r.Chunks++
	}
	behind := 0
	last := tick0
	for {
		if !gemrunner.NextTick(gd, last) {
			r.Reason = "stopped"
			return r, nil
		}
		now := st.Tick()
		elapsed := now - tick0
		last = now
		if d.Chat != nil && s.refused != nil {
			if why, level := s.refused(d.Chat.Since(at)); why != "" {
				r.RefusedLevel = level
				return stop(why, "the chat refused the use")
			}
		}
		if o.Hold != nil && !o.Hold() {
			return stop("held", "the round's hold")
		}
		c := uses()
		if want := s.stream.UsesIn(min(r.Chunks, elapsed-fastSlack), r.Planned); c < want {
			if behind++; behind >= 2 {
				return stop("desync", fmt.Sprintf("%d uses by tick %d, %d expected", c, elapsed, want))
			}
		} else {
			behind = 0
		}
		if r.Chunks == total && c >= totalUses {
			break
		}
		if r.Chunks == total && elapsed > total+fastLookahead+fastSlack+4 {
			return stop("desync", fmt.Sprintf("%d of %d uses after the last chunk", c, totalUses))
		}
		for r.Chunks < total && r.Chunks < elapsed+fastLookahead {
			if err := send(r.Chunks); err != nil {
				return stop("desync", "sending: "+err.Error())
			}
			r.Chunks++
		}
	}
	// The planned end: the pack must show the last load's product.
	layoutOK := func() bool {
		fine, _ := s.done(packItems(d))
		return fine
	}
	if !waitUntil(d, layoutOK, quickDoneWait) {
		_, why := s.done(packItems(d))
		r.Reason, r.Why = "desync", "after the last load the pack shows "+why
		return r, nil
	}
	r.Reason = "done"
	return r, nil
}

// ---- the chisel: gems and oyster pearls ----

// fastLayout reads a pack for a chisel spec's load just made: the chisel's slot, and every other
// slot holding the product (or, stacked, one product stack in the first slot that is not the
// chisel's).
func fastLayout(pack []gamestate.SlotItem, product int32, stacked bool, what string) (chiselSlot int, ok bool, why string) {
	chiselSlot = -1
	products, stackSlot := 0, -1
	for _, it := range pack {
		switch it.ID {
		case gemplan.ChiselID:
			if chiselSlot >= 0 {
				return -1, false, "two chisels in the pack"
			}
			chiselSlot = it.Slot
		case product:
			products++
			stackSlot = it.Slot
		default:
			return -1, false, fmt.Sprintf("item %d in the pack", it.ID)
		}
	}
	switch {
	case chiselSlot < 0:
		return -1, false, "no chisel in the pack"
	case stacked && (products != 1 || stackSlot != mastercraftplan.FastEventAt(0, chiselSlot).Slot):
		return -1, false, fmt.Sprintf("%d %s stack(s), not one in the first slot", products, what)
	case !stacked && products != mastercraftplan.FastLoadUses:
		return -1, false, fmt.Sprintf("%d %s in the pack, not %d", products, what, mastercraftplan.FastLoadUses)
	}
	return chiselSlot, true, ""
}

// chiselSpec is the spec for the chisel on target, making product (stacked: one stack), the run
// starting only when next (the round's choice from the bank) still names target.
func chiselSpec(what string, target, product int32, stacked bool, stat, xpTenths int, refused func(string) (string, int), next func(bank []gamestate.SlotItem) int32) fastSpec {
	chiselSlot, bankSlot := -1, -1
	s := fastSpec{
		what: what, stream: mastercraftplan.ChiselStream, stat: stat, xpTenths: xpTenths, refused: refused,
		keep: KeepChisel,
		done: func(pack []gamestate.SlotItem) (bool, string) {
			_, ok, why := fastLayout(pack, product, stacked, what)
			return ok, why
		},
		draw: func(bank []gamestate.SlotItem) []int32 {
			if id := next(bank); id != 0 {
				return []int32{id}
			}
			return nil
		},
	}
	s.prepare = func(pack, bank []gamestate.SlotItem) (int, string) {
		if next(bank) != target {
			return 0, "the next item to draw is not " + what
		}
		var fine bool
		var why string
		if chiselSlot, fine, why = fastLayout(pack, product, stacked, what); !fine {
			return 0, why
		}
		for _, it := range bank {
			if it.ID == target {
				bankSlot = it.Slot
				return int(it.Count) / mastercraftplan.FastLoadUses, ""
			}
		}
		return 0, ""
	}
	s.send = func(b actions.Session, ev mastercraftplan.FastEvent, invCom, sideCom int) error {
		switch ev.Kind {
		case mastercraftplan.FastDeposit:
			return actions.InvButton(b, 4, product, mastercraftplan.FastEventAt(0, chiselSlot).Slot, sideCom)
		case mastercraftplan.FastWithdraw:
			return actions.InvButton(b, 4, target, bankSlot, actions.BankWithdraw)
		case mastercraftplan.FastUse:
			slot := mastercraftplan.FastEventAt(len(mastercraftplan.ChiselStream.Head)+ev.Use, chiselSlot).Slot
			return actions.OpHeldU(b, target, slot, invCom, gemplan.ChiselID, chiselSlot, invCom)
		}
		return fmt.Errorf("the chisel stream has no event %d", ev.Kind)
	}
	return s
}

// gemSpec is the fast run's spec for cutting gem, pick the round's choice of the next gem.
func gemSpec(gem gemplan.Gem, pick func(uncut map[string]int) (gemplan.Gem, bool)) fastSpec {
	s := chiselSpec(gem.Name, gem.UncutID, gem.CutID, false, gemrunner.CraftingStat, gem.XP, gemrunner.GemRefusal,
		func(bank []gamestate.SlotItem) int32 {
			if g, ok := pick(UncutCounts(bank)); ok {
				return g.UncutID
			}
			return 0
		})
	if gem.CanCrush {
		s.never = gem.Name + " can be smashed, so the pack after a load cannot be foretold"
	}
	return s
}

// pearlSpec is the fast run's spec for cutting oyster pearls of kind into Pearl bolttips.
func pearlSpec(kind pearlplan.Kind) fastSpec {
	return chiselSpec(kind.Name, kind.ID, pearlplan.TipsID, true, bowstringrunner.FletchingStat, pearlplan.XPTenths, pearlplan.Refusal,
		func(bank []gamestate.SlotItem) int32 {
			if ids := PearlDraw(bank); len(ids) > 0 {
				return ids[0]
			}
			return 0
		})
}

// FastGems runs loads of gem the fast way, starting from a primed quick bank: the booth op has gone
// out behind the last load's last burst. pick is the gem round's choice: a run starts only when it
// still names gem. When the run cannot start, the quick visit is made the 7-tick way instead
// (Fallback), so the caller cuts that load as usual.
func FastGems(d *fleetrunner.Deps, q *QuickBank, gem gemplan.Gem, pick func(uncut map[string]int) (gemplan.Gem, bool), o FastOptions) FastResult {
	r, qv := fastRun(d, q, gemSpec(gem, pick), o)
	if qv != nil {
		v := GemVisit{Visit: Visit{Opened: qv.Opened, Err: qv.Err, Banked: qv.AtOpen}}
		v.ChiselHeld = packCount(d, gemplan.ChiselID) > 0
		for _, id := range qv.Drew {
			if g, ok := gemplan.GemByUncutID(id); ok {
				v.Gem, v.Drawn = g, packCount(d, id)
			}
		}
		r.Fallback = &v
	}
	return r
}

// loadVisit is a fallback quick visit as a LoadVisit.
func loadVisit(d *fleetrunner.Deps, qv *QuickVisit) *LoadVisit {
	lv := LoadVisit{Visit: Visit{Opened: qv.Opened, Err: qv.Err, Banked: qv.AtOpen}, Drew: qv.Drew}
	lv.ChiselHeld = packCount(d, gemplan.ChiselID) > 0
	if !qv.Done && lv.Err == "" {
		lv.Err = "the quick visit did not finish"
	}
	return &lv
}

// FastPearls runs loads of oyster pearls of kind the fast way, as FastGems does gems: the chisel on
// 27 pearls a load, the Pearl bolttips stack banked by the next load's Deposit-All. The level guard
// and the desync check read Fletching XP (3.2 a pearl). When the run cannot start, the quick visit
// is made the 7-tick way instead (Load), so the caller cuts that load as usual.
func FastPearls(d *fleetrunner.Deps, q *QuickBank, kind pearlplan.Kind, o FastOptions) FastResult {
	r, qv := fastRun(d, q, pearlSpec(kind), o)
	if qv != nil {
		r.Load = loadVisit(d, qv)
	}
	return r
}

// ---- bow stringing ----

// bowLayout reads a pack for a finished bow load: 14 strung bows in slots 0..13 and nothing else
// (empty reports an empty pack, which a first run starts from).
func bowLayout(pack []gamestate.SlotItem, ids bowstringrunner.IDs) (ok, empty bool, why string) {
	if len(pack) == 0 {
		return false, true, "the pack is empty"
	}
	n := mastercraftplan.BowStream.Uses
	strung := 0
	for _, it := range pack {
		if it.ID != ids.Strung || it.Slot >= n {
			return false, false, fmt.Sprintf("item %d in slot %d", it.ID, it.Slot)
		}
		strung++
	}
	if strung != n {
		return false, false, fmt.Sprintf("%d strung bows, not %d", strung, n)
	}
	return true, false, ""
}

var bowLevelRefusal = regexp.MustCompile(`You need level (\d+) Fletching to string`)

// BowRefusal reads stringing's chat for content's refusals (skill_fletching/scripts/bows.rs2
// [label,string_bow]): "You need level <n> Fletching to string that bow." is "refused" with the
// level; the members-world line (^mes_members_fletching) is "members".
func BowRefusal(said string) (string, int) {
	if m := bowLevelRefusal.FindStringSubmatch(said); m != nil {
		n, _ := strconv.Atoi(m[1])
		return "refused", n
	}
	if why, _ := pearlplan.Refusal(said); why == "members" {
		return "members", 0
	}
	return "", 0
}

// bowSpec is the fast run's spec for stringing bows of ids, xpTenths a bow.
func bowSpec(name string, ids bowstringrunner.IDs, xpTenths int) fastSpec {
	return pairSpec(name, ids, xpTenths, bowstringrunner.FletchingStat, BowRefusal)
}

// pairSpec is the bow load for any two unstackable items used on one another (a bow and its string, a Battlestaff and a Fire
// orb): ids.Unstrung is the item held in slots 0..13, ids.String the one in 14..27 and ids.Strung the product; stat is the skill
// whose experience the run reads and refused the chat check (nil for a job whose refusals are not chat lines).
func pairSpec(name string, ids bowstringrunner.IDs, xpTenths, stat int, refused func(string) (string, int)) fastSpec {
	n := mastercraftplan.BowStream.Uses
	bowSlot, strSlot := -1, -1
	startEmpty := false
	s := fastSpec{
		what: name, stream: mastercraftplan.BowStream, stat: stat, xpTenths: xpTenths,
		refused: refused,
		done: func(pack []gamestate.SlotItem) (bool, string) {
			ok, _, why := bowLayout(pack, ids)
			return ok, why
		},
	}
	s.prepare = func(pack, bank []gamestate.SlotItem) (int, string) {
		ok, empty, why := bowLayout(pack, ids)
		if !ok && !empty {
			return 0, why
		}
		startEmpty = empty
		bows, strs := 0, 0
		for _, it := range bank {
			switch it.ID {
			case ids.Unstrung:
				bowSlot, bows = it.Slot, int(it.Count)
			case ids.String:
				strSlot, strs = it.Slot, int(it.Count)
			}
		}
		return min(bows, strs) / n, ""
	}
	s.send = func(b actions.Session, ev mastercraftplan.FastEvent, invCom, sideCom int) error {
		if ev.Kind == mastercraftplan.FastUse {
			return actions.OpHeldU(b, ids.Unstrung, ev.Use, invCom, ids.String, n+ev.Use, invCom)
		}
		// The load's three bank events, by place: see the file comment.
		if ev.Load == 0 && startEmpty {
			switch ev.Head {
			case 0:
				return actions.InvButton(b, 5, ids.Unstrung, bowSlot, actions.BankWithdraw) // Withdraw-X of the bows
			case 1:
				return actions.ResumeCountDialog(b, int32(n)) // its count
			case 2:
				return actions.InvButton(b, 4, ids.String, strSlot, actions.BankWithdraw) // Withdraw-All of the strings
			}
		} else {
			switch ev.Head {
			case 0:
				return actions.InvButton(b, 4, ids.String, strSlot, actions.BankWithdraw) // Withdraw-All of the strings
			case 1:
				return actions.InvButton(b, 4, ids.Strung, 0, sideCom) // Deposit-All of the strung bows
			case 2:
				return actions.InvButton(b, 4, ids.Unstrung, bowSlot, actions.BankWithdraw) // Withdraw-All of the bows
			}
		}
		return fmt.Errorf("the bow stream has no event %d (head %d)", ev.Kind, ev.Head)
	}
	return s
}

// FastBows strings bows of ids the fast way, starting from a primed quick bank, with the pack
// empty or holding a finished load (14 strung bows in slots 0..13): 14 bows a load in four ticks.
// The level guard and the desync check read Fletching XP (xpTenths a bow). When the run cannot
// start, the bank visit banks the whole pack and draws nothing (Load; Short when the bank holds
// less than a load), for the caller's own bank step.
func FastBows(d *fleetrunner.Deps, q *QuickBank, name string, ids bowstringrunner.IDs, xpTenths int, o FastOptions) FastResult {
	r, qv := fastRun(d, q, bowSpec(name, ids, xpTenths), o)
	if qv != nil {
		r.Load = loadVisit(d, qv)
	}
	return r
}

// Describe is a one-line account of a run for a log.
func (r FastResult) Describe() string {
	s := fmt.Sprintf("fast run: %d of %d load(s), %d used, +%d XP, %d chunk(s) over %d tick(s) in %s (%s",
		r.Loads, r.Planned, r.Cuts, r.XP, r.Chunks, r.Ticks, r.Took.Round(100*time.Millisecond), r.Reason)
	if r.Why != "" {
		s += ": " + r.Why
	}
	return s + ")"
}
