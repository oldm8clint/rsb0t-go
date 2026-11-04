package controlpanel

import (
	"fmt"
	"math/rand"
	"sync"
	"time"

	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/mulestatus"
	"rsb0t-go/internal/phaselog"
)

// The simulated fleet's phases and mule, so both panels show realistic data under -fake with no
// game connection. SYNTHETIC: every duration and probability below is invented to look like the
// live fleet's shape (a fishing loop with bank trips, the occasional random event, hand-off or
// recovery, a few accounts that lose time to idle gaps and long walks), not measured.

// fakeMuleName is the simulated mule. It is not one of the fake accounts; it only exists in
// Phases and MuleStatuses.
const fakeMuleName = "mule"

// fakeWork is each fake bot's own "working" phase name.
var fakeWork = map[string]string{
	"fisher": phaselog.Fishing, "miner": "mining", "thief": "thieving",
	"cutter": "chopping", "crafter": "spinning", "agility": "agility",
}

var fakeCatalog = []string{"Small fishing net", "Big fishing net", "Harpoon", "Lobster pot", "Air rune", "Mind rune", "Staff of air", "Coins"}

type fakePhaseState struct {
	name  string
	phase string
	since time.Time
	until time.Time
	slow  bool // loses time to idle gaps and long walks
	leg   int  // position in the work -> walk -> bank -> walk loop
}

type fakeMuleSim struct {
	tracker *mulestatus.Tracker
	phase   string
	since   time.Time
	until   time.Time
	stage   int
	partner string
	fail    string
	queue   map[string]time.Time // donor -> when it arrives at the mule
	raw     map[string]int64
	cooked  map[string]int64
	coins   int64
	kit     map[string]int64
	nextOut time.Time
	// nextDonor is when the rest of the (simulated, unseen) fleet next announces a hand-off: a
	// real mule serves ~250 fishers, far more than a -fake fleet holds.
	nextDonor time.Time
}

// fakePhaseSim is the zero-value-ready simulation state kept on FakeSource.
type fakePhaseSim struct {
	mu   sync.Mutex
	rng  *rand.Rand
	acc  map[string]*fakePhaseState
	mule *fakeMuleSim
	// ledger is the fake fleet's mule ledger, which the simulated mule's trades are written to.
	ledger *muleledger.Ledger
	// clock is the simulated mule's own time, which its tracker reads: the simulated moment of
	// each step while catching up, so records carry the times they were simulated at.
	clock time.Time
}

// fakeMuleBackfill is how much simulated mule history exists the moment -fake starts, so the
// dashboard's hourly figures are populated from the first page load.
const fakeMuleBackfill = time.Hour

func (s *fakePhaseSim) between(lo, hi time.Duration) time.Duration {
	return lo + time.Duration(s.rng.Int63n(int64(hi-lo)+1))
}

// record writes one simulated finished trade to the fake fleet's mule ledger (the same one
// fakeloot.go writes to), stamped with the simulated time.
func (s *fakePhaseSim) record(e muleledger.Entry) {
	e.AtMs, e.Mule = s.clock.UnixMilli(), fakeMuleName
	s.ledger.Record(e)
}

// fakeMuleLedger is the fake fleet's mule ledger, created on first use (fakeloot.go's
// stepMuleTrade does the same).
func (f *FakeSource) fakeMuleLedger() *muleledger.Ledger {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.ledger == nil {
		f.ledger = muleledger.Open("", 0)
		f.ledger.SetClock(f.now)
	}
	return f.ledger
}

func (s *fakePhaseSim) init(now time.Time, ledger *muleledger.Ledger) {
	if s.rng != nil {
		return
	}
	s.ledger = ledger
	s.rng = rand.New(rand.NewSource(7))
	s.acc = map[string]*fakePhaseState{}
	start := now.Add(-fakeMuleBackfill)
	s.clock = start
	s.mule = &fakeMuleSim{
		tracker: mulestatus.NewTracker(fakeMuleName, func() time.Time { return s.clock }), phase: phaselog.Idle, since: start, until: start,
		queue:  map[string]time.Time{},
		raw:    map[string]int64{"Raw lobster": 4120, "Raw swordfish": 980, "Raw tuna": 2210, "Raw bass": 610, "Raw cod": 1480, "Raw mackerel": 2950},
		cooked: map[string]int64{"Lobster": 820, "Tuna": 340},
		coins:  182400,
		kit:    map[string]int64{"Small fishing net": 6, "Big fishing net": 14, "Harpoon": 3, "Lobster pot": 5, "Air rune": 2400, "Mind rune": 1900, "Staff of air": 2, "Coins": 0},
	}
	s.mule.tracker.SetCatalog(fakeCatalog)
	s.mule.nextOut = start.Add(90 * time.Second)
	s.mule.nextDonor = start.Add(10 * time.Second)
	s.stepMule(now)
}

// nextPhase moves one fake account on to its next phase.
func (s *fakePhaseSim) nextPhase(st *fakePhaseState, short string, now time.Time) {
	work := fakeWork[short]
	if work == "" {
		work = "working"
	}
	set := func(p string, d time.Duration) { st.phase, st.since, st.until = p, now, now.Add(d) }
	walk := func() time.Duration {
		if st.slow {
			return s.between(40*time.Second, 95*time.Second)
		}
		return s.between(12*time.Second, 22*time.Second)
	}
	if short == "agility" {
		if st.phase == "agility" && s.rng.Intn(6) == 0 {
			set(phaselog.Recovering, s.between(15*time.Second, 40*time.Second))
			return
		}
		set("agility", s.between(60*time.Second, 150*time.Second))
		return
	}
	switch st.leg {
	case 0: // finished working (or just started): the rare interruptions happen here
		st.leg = 1
		r := s.rng.Intn(100)
		switch {
		case r < 4:
			set(phaselog.Event, s.between(20*time.Second, 70*time.Second))
			st.leg = 0
			return
		case r < 7 && short == "fisher":
			d := s.between(50*time.Second, 110*time.Second)
			set(phaselog.Handoff, d)
			s.mule.queue[st.name] = now.Add(s.between(15*time.Second, 30*time.Second))
			st.leg = 0
			return
		case r < 9:
			set(phaselog.Recovering, s.between(60*time.Second, 180*time.Second))
			st.leg = 0
			return
		case (st.slow && r < 40) || r < 15:
			set(phaselog.Idle, s.between(5*time.Second, 60*time.Second))
			st.leg = 0
			return
		}
		set(phaselog.Walking, walk())
	case 1:
		st.leg = 2
		d := s.between(6*time.Second, 12*time.Second)
		if st.slow {
			d = s.between(15*time.Second, 35*time.Second)
		}
		set(phaselog.Banking, d)
	case 2:
		st.leg = 3
		set(phaselog.Walking, walk())
	default:
		st.leg = 0
		d := s.between(70*time.Second, 150*time.Second)
		if st.slow {
			d = s.between(40*time.Second, 90*time.Second)
		}
		set(work, d)
	}
}

// fakeEventNames are the random events the simulated guardian "handles", weighted by repetition.
var fakeEventNames = []string{"Genie", "Genie", "Sandwich lady", "Drunken dwarf", "Mysterious Old Man", "Genie", "Rick Turpentine", "Certer", "Drunken dwarf", "Evil Chicken"}

var fakeLoadFish = []string{"Raw lobster", "Raw swordfish", "Raw tuna", "Raw mackerel", "Raw cod", "Raw bass"}

var fakeFailReasons = []string{"no stable offer", "confirm screen timeout", "offer changed, never re-settled", "closed before confirm", "random event before confirm"}

// stepMule advances the simulated mule to now.
func (s *fakePhaseSim) stepMule(now time.Time) {
	m := s.mule
	defer func() { s.clock = now }()
	for !now.Before(m.nextDonor) {
		name := fmt.Sprintf("swarm%d", 20+s.rng.Intn(230))
		m.queue[name] = m.nextDonor.Add(s.between(15*time.Second, 45*time.Second))
		m.nextDonor = m.nextDonor.Add(s.between(25*time.Second, 70*time.Second))
	}
	for now.After(m.until) {
		at := m.until
		s.clock = at
		set := func(p string, d time.Duration) { m.phase, m.since, m.until = p, at, at.Add(d) }
		switch m.phase {
		case phaselog.Trading:
			stages := []string{mulestatus.StageReciprocating, mulestatus.StageOffer, mulestatus.StageAccepted, mulestatus.StageConfirm, mulestatus.StageReceipt}
			if m.fail != "" && m.stage >= 2 {
				m.tracker.Failed(m.partner)
				s.record(muleledger.Entry{Partner: m.partner, Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeFailed, Reason: m.fail})
				m.fail = ""
				set(phaselog.Idle, 2*time.Second)
				continue
			}
			if m.stage < len(stages) {
				m.tracker.Stage(m.partner, stages[m.stage])
				d := []time.Duration{2 * time.Second, s.between(8*time.Second, 22*time.Second), s.between(2*time.Second, 5*time.Second), s.between(time.Second, 2*time.Second), time.Second}[m.stage]
				m.stage++
				m.until = at.Add(d)
				continue
			}
			// A fisher's load: a few noted fish stacks, often coins, sometimes a casket's runes.
			var load []muleledger.Item
			for _, fish := range fakeLoadFish[:2+s.rng.Intn(len(fakeLoadFish)-1)] {
				n := int64(20 + s.rng.Intn(60))
				m.raw[fish] += n
				load = append(load, muleledger.Item{ID: -1, Name: fish, Count: n})
			}
			if s.rng.Intn(3) > 0 {
				n := int64(s.rng.Intn(400))
				m.coins += n
				load = append(load, muleledger.Item{ID: 995, Name: "Coins", Count: n})
			}
			if s.rng.Intn(4) == 0 {
				load = append(load, muleledger.Item{ID: 561, Name: "Nature rune", Count: int64(1 + s.rng.Intn(8))}, muleledger.Item{ID: 1623, Name: "Uncut sapphire", Count: 1})
			}
			m.tracker.Completed(m.partner)
			s.record(muleledger.Entry{Partner: m.partner, Kind: muleledger.KindReceive, Outcome: muleledger.OutcomeAccepted, In: load})
			set(phaselog.Banking, s.between(8*time.Second, 13*time.Second))
		case phaselog.Banking:
			m.tracker.Banked()
			set(phaselog.Idle, time.Second)
		case phaselog.Cooking:
			n := int64(28)
			if m.raw["Raw lobster"] >= n {
				m.raw["Raw lobster"] -= n
				m.cooked["Lobster"] += n - 2
			}
			set(phaselog.Idle, time.Second)
		case phaselog.Handout:
			item := fakeCatalog[s.rng.Intn(len(fakeCatalog)-1)]
			count := 1
			if item == "Air rune" || item == "Mind rune" {
				count = 100
			}
			if m.kit[item] >= int64(count) {
				m.kit[item] -= int64(count)
				s.record(muleledger.Entry{Partner: fmt.Sprintf("fisher%02d", 1+s.rng.Intn(20)), Kind: muleledger.KindHandout, Outcome: muleledger.OutcomeAccepted, Out: []muleledger.Item{{ID: -1, Name: item, Count: int64(count)}}})
			}
			set(phaselog.Idle, time.Second)
		default: // idle, event, walking: pick the next job
			var next string
			for name, arrive := range m.queue {
				if !arrive.After(at) && (next == "" || name < next) {
					next = name
				}
			}
			switch {
			case next != "":
				delete(m.queue, next)
				m.partner, m.stage = next, 0
				if s.rng.Intn(6) == 0 {
					m.fail = fakeFailReasons[s.rng.Intn(len(fakeFailReasons))]
				}
				set(phaselog.Trading, 0)
			case at.After(m.nextOut):
				m.nextOut = at.Add(s.between(3*time.Minute, 6*time.Minute))
				set(phaselog.Handout, s.between(25*time.Second, 40*time.Second))
			case s.rng.Intn(50) == 0:
				set(phaselog.Event, s.between(20*time.Second, 45*time.Second))
			case len(m.queue) == 0 && s.rng.Intn(3) == 0:
				set(phaselog.Cooking, s.between(60*time.Second, 100*time.Second))
			default:
				set(phaselog.Idle, 3*time.Second)
			}
		}
	}
	s.clock = now
	m.tracker.SetIncoming(queued(m.queue, now))
}

// queued is the simulated donors that have announced by at (20 seconds before they arrive).
func queued(queue map[string]time.Time, at time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	for name, arrive := range queue {
		if announced := arrive.Add(-20 * time.Second); !announced.After(at) {
			out[name] = announced
		}
	}
	return out
}

// Phases implements PhaseSource for the simulated fleet: SYNTHETIC phases (see this file's doc).
func (f *FakeSource) Phases(dst []AccountPhase) []AccountPhase {
	f.mu.Lock()
	type acc struct {
		name, bot, botState string
		short               string
		ingame              bool
	}
	accs := make([]acc, 0, len(f.accounts))
	for i, a := range f.accounts {
		accs = append(accs, acc{name: a.Name, bot: a.Bot, botState: a.BotState, short: fakeBots[f.bots[i]].short, ingame: a.State == StateIngame})
	}
	f.mu.Unlock()

	now := f.now()
	ledger := f.fakeMuleLedger()
	s := &f.phaseSim
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init(now, ledger)
	dst = dst[:0]
	for i, a := range accs {
		st := s.acc[a.name]
		if st == nil {
			st = &fakePhaseState{name: a.name, slow: i%7 == 3, until: now}
			s.acc[a.name] = st
		}
		p := AccountPhase{Username: a.name, InGame: a.ingame, Script: a.bot, Running: a.botState != "stopped"}
		if a.ingame && p.Running {
			for !now.Before(st.until) {
				s.nextPhase(st, a.short, now)
			}
			if st.phase == phaselog.Event {
				p.Event, p.EventSince = fakeEventNames[int(st.since.Unix()/7)%len(fakeEventNames)], st.since
			} else {
				p.Phase, p.Since = st.phase, st.since
			}
		} else {
			st.until = now // restart the loop on the next login
		}
		dst = append(dst, p)
	}
	s.stepMule(now)
	m := AccountPhase{Username: fakeMuleName, InGame: true, Script: "MuleReceiver", Running: true, Phase: s.mule.phase, Since: s.mule.since}
	if s.mule.phase == phaselog.Event {
		m.Phase, m.Event, m.EventSince = "", "Sandwich lady", s.mule.since
	}
	return append(dst, m)
}

// MuleStatuses implements MuleSource for the simulated fleet: one SYNTHETIC mule.
func (f *FakeSource) MuleStatuses() []MuleStatus {
	now := f.now()
	ledger := f.fakeMuleLedger()
	s := &f.phaseSim
	s.mu.Lock()
	defer s.mu.Unlock()
	s.init(now, ledger)
	s.stepMule(now)
	m := s.mule
	st := m.tracker.Snapshot()
	var names []string
	var counts []int64
	for n, c := range m.raw {
		names, counts = append(names, n), append(counts, c)
	}
	for n, c := range m.cooked {
		names, counts = append(names, n), append(counts, c)
	}
	for n, c := range m.kit {
		names, counts = append(names, n), append(counts, c)
	}
	names, counts = append(names, "Coins", "Bones", "Seaweed"), append(counts, m.coins, 12, 40)
	stock := mulestatus.ClassifyStock(names, counts, fakeCatalog, now.Add(-12*time.Second))
	st.Stock = &stock
	return []MuleStatus{st}
}
