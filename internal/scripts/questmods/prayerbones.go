package questmods

// The Prayer rungs' bones: bury what the pack holds, draw the own bank's, ask the mule for the rest and only then kill
// cows (questplan/prayerbones.go has the rule and the reason). This file carries the moves out: runPrayerBones is the
// loop, over the boneIO it is given so that its order can be tested without a game; liveBones is that world.

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// boneIO is what a Prayer rung's bones loop needs of the world.
type boneIO interface {
	// Need is the experience still to gain.
	Need() int
	// Pack is the bones the pack holds as items, by object id.
	Pack() map[int]int
	// Bury buries the pack's bones where the account stands.
	Bury()
	// Bank goes to the nearest bank, reads it, and draws its bones a pack at a time and buries them until it holds
	// none, the need is met or the time is spent. counts is the bank as last read; ok is false when no booth opened.
	Bank() (counts map[int]int, ok bool)
	// Hint is an earlier run's reading of the bank, ok false when there is none worth trusting.
	Hint() (counts map[int]int, ok bool)
	// Mule is the mule's published bones by object id, known false when there is no mule or it is not heard.
	Mule() (stock map[int]int, known bool)
	// Ask asks the mule for lines in one kit and reports how many bones (or their notes) reached the pack, and why
	// when none did.
	Ask(lines []questplan.ItemCount) (arrived int, why string)
	// Left is the time the run has left; Stopped is true when the bot is stopping.
	Left() time.Duration
	Stopped() bool
	// Logf says a line in the account's log.
	Logf(format string, args ...any)
}

// boneTally is the experience a run gained, by where the bones came from, and the moves it made.
type boneTally struct {
	Bank, Mule int
	Moves      []questplan.BonesMoveKind
	Asks       int
	// ToCows is that no bone was to be had from the pack, the bank or the mule: the cows are what is left.
	// Stopped is that the run ended on questplan.MoveStop: no bone to be had and too little time left to walk to the cows.
	ToCows, Stopped bool
}

// Gained is the experience of the whole run.
func (t boneTally) Gained() int { return t.Bank + t.Mule }

// maxBoneMoves bounds one run's moves: a bank trip draws a pack and each ask a thousand, so this is far more than any
// need, and it ends a loop the guards below somehow missed.
const maxBoneMoves = 60

// runPrayerBones is the item rule for a Prayer rung's bones, move by move (questplan.NextBonesMove), until the level,
// the run's time or the last bone there is. muleOK says whether this rung may ask the mule at all.
func runPrayerBones(io boneIO, muleOK bool) boneTally {
	var t boneTally
	st := questplan.BonesState{MuleOK: muleOK, BankIs: questplan.BankUnread}
	if hint, ok := io.Hint(); ok {
		st.Bank, st.BankIs = hint, questplan.BankHint
	}
	fromMule := false // the bones now being buried are the mule's
	for i := 0; i < maxBoneMoves && !io.Stopped(); i++ {
		st.Need, st.Pack, st.Left = io.Need(), io.Pack(), io.Left()
		st.Mule, st.MuleKnown = nil, false
		if muleOK {
			st.Mule, st.MuleKnown = io.Mule()
		}
		mv := questplan.NextBonesMove(st)
		t.Moves = append(t.Moves, mv.Kind)
		before := st.Need
		gained := func() int { return max(before-io.Need(), 0) }
		switch mv.Kind {
		case questplan.MoveDone, questplan.MoveStop:
			io.Logf("%s — %s", mv.Kind, mv.Why)
			t.Stopped = mv.Kind == questplan.MoveStop
			return t
		case questplan.MoveBury:
			io.Logf("burying: %s", mv.Why)
			io.Bury()
			if questplan.BoneCount(io.Pack()) >= questplan.BoneCount(st.Pack) {
				st.BuryBlocked = true // nothing went: carry them to the bank instead
				io.Logf("the pack's bones would not bury; carrying them on")
			}
			if fromMule {
				t.Mule += gained()
			} else {
				t.Bank += gained()
			}
		case questplan.MoveBank:
			io.Logf("to the bank: %s", mv.Why)
			counts, ok := io.Bank()
			g := gained()
			switch {
			case !ok:
				st.BankBlocked = true
				io.Logf("no bank booth opened; the bank is left alone this run")
			case g <= 0 && questplan.BoneCount(counts) > 0:
				st.BankBlocked = true // it holds bones and none were buried: do not go round again
				st.Bank, st.BankIs = counts, questplan.BankFresh
				io.Logf("the bank holds %s and none could be drawn and buried; leaving it", questplan.DescribeBones(counts))
			default:
				st.Bank, st.BankIs = counts, questplan.BankFresh
			}
			if fromMule {
				t.Mule += g
			} else {
				t.Bank += g
			}
			fromMule = false
		case questplan.MoveAsk:
			io.Logf("asking the mule: %s", mv.Why)
			arrived, why := io.Ask(mv.Ask)
			st.Asks++
			t.Asks++
			if arrived <= 0 {
				st.AskEmpty = true
				io.Logf("the mule's ask brought nothing (%s)", why)
			} else {
				st.BankIs = questplan.BankDirty
				fromMule = true
				io.Logf("the mule's ask brought %d bones to the pack (the rest is in the bank)", arrived)
			}
		case questplan.MoveCows:
			io.Logf("cows: %s", mv.Why)
			t.ToCows = true
			return t
		}
	}
	return t
}

// boneStock is the mule's published bones by kind. The mule's rows are keyed by the FIRST un-noted object of a name
// (donorMuleAsker.Published), and "Bones" names several (the members' variants 280-283 come before the 526 the mule
// really holds), so a kind's stock is the rows summed over every un-noted object its name answers to.
func boneStock(pub map[int]int, objIDs func(name string) []int, isNote func(id int) bool) map[int]int {
	out := map[int]int{}
	for _, k := range questplan.BoneKinds {
		out[k.ID] = pub[k.ID]
		if objIDs == nil {
			continue
		}
		for _, id := range objIDs(k.Name) {
			if id != k.ID && !isNote(id) {
				out[k.ID] += pub[id]
			}
		}
	}
	return out
}

// liveBones is the boneIO of a rung running in an engine.
type liveBones struct {
	e        *questrunner.Engine
	d        *questrunner.Deps
	id       string
	skill    string
	level    int
	deadline time.Time
	// openBank replaces the walk to the nearest bank and its booth (a test's simulated bank); nil is the real one.
	openBank func() (*actions.Bank, bool)
}

// open walks to the nearest bank and opens a booth.
func (l *liveBones) open() (*actions.Bank, bool) {
	if l.openBank != nil {
		return l.openBank()
	}
	return questrunner.OpenBankAt(l.d, questplan.NearestBank(questrunner.CurrentTile(l.d)).Tile)
}

// xp is the skill's experience and whether the server has reported it yet: an unread skill is not an empty one.
func (l *liveBones) xp() (int, bool) {
	for i, sk := range l.d.Session.State().Skills() {
		if strings.EqualFold(gamestate.SkillName(i), l.skill) {
			return sk.XP, sk.Generation > 0
		}
	}
	return 0, false
}

// Need is the experience still to gain; 0 until the skill has been read, so that no mule is asked for a level nobody has checked.
func (l *liveBones) Need() int {
	xp, read := l.xp()
	if !read {
		return 0
	}
	return questplan.PrayerNeed(xp, l.level)
}

func (l *liveBones) Pack() map[int]int {
	pack := questrunner.PackCounts(l.d)
	out := map[int]int{}
	for _, k := range questplan.BoneKinds {
		if n := pack[k.ID]; n > 0 {
			out[k.ID] = n
		}
	}
	return out
}

func (l *liveBones) Bury() { buryAll(l.d) }

func (l *liveBones) Hint() (map[int]int, bool) {
	b := l.e.Provisioner().Bank
	if b == nil || !b.Known || time.Since(b.At) > questplan.BankHintMaxAge {
		return nil, false
	}
	return b.Counts, true
}

// keepCoins is the bones trips' deposit: everything but the coins goes into the bank, as it always did here.
func keepCoins(id int) bool { return id == questplan.CoinsID }

func (l *liveBones) Bank() (map[int]int, bool) {
	d := l.d
	var last map[int]int
	opened := false
	for l.Left() >= questplan.BoneMinLeft && !l.Stopped() {
		b, ok := l.open()
		if !ok {
			return last, opened
		}
		opened = true
		questrunner.DepositExcept(d, b, keepCoins)
		last = questrunner.BankCounts(b)
		l.e.Provisioner().Bank.Note(last)
		draws := questplan.PlanBoneDraws(l.Need(), last, questrunner.FreeSlots(d))
		if len(draws) == 0 {
			_ = b.Close()
			return last, true
		}
		lines := make([]questrunner.Draw, 0, len(draws))
		for _, c := range draws {
			lines = append(lines, questrunner.Draw{ID: c.ID, N: c.Count})
		}
		questrunner.WithdrawExactMany(d, b, lines)
		_ = b.Close()
		buryAll(d)
		if questplan.BoneCount(l.Pack()) > 0 {
			return last, true // nothing would bury (the pack is not visible to the server): going round again only withdraws the same bones
		}
		if l.Need() <= 0 {
			return last, true
		}
	}
	return last, opened
}

func (l *liveBones) Mule() (map[int]int, bool) {
	m := l.e.Provisioner().Mule
	if m == nil {
		return nil, false
	}
	pub, ok := m.Published()
	if !ok {
		return nil, false
	}
	var ids func(string) []int
	isNote := func(int) bool { return false }
	if l.d.Config != nil {
		ids = l.d.Config.ObjIDs
		isNote = func(id int) bool {
			o, ok := l.d.Config.Obj(id)
			return ok && o.Noted()
		}
	}
	return boneStock(pub, ids, isNote), true
}

// holding is the bones in the pack counted as items and as bank notes.
func (l *liveBones) holding() int {
	pack := questrunner.PackCounts(l.d)
	n := 0
	for _, k := range questplan.BoneKinds {
		n += pack[k.ID] + pack[k.Note]
	}
	return n
}

func (l *liveBones) Ask(lines []questplan.ItemCount) (int, string) {
	m := l.e.Provisioner().Mule
	if m == nil {
		return 0, "no mule"
	}
	before := l.holding()
	_, why := m.Ask("quest:prayer-bones", lines)
	arrived := l.holding() - before
	if arrived > 0 {
		// The kit banks what the pack has no room for, and this run may end (the mule's line was long) before the bank is
		// drawn: the bank hint the next run reads must say the bones are there, or that run asks the mule for them again.
		hint := l.e.Provisioner().Bank
		counts := map[int]int{}
		for id, n := range hint.Counts {
			counts[id] = n
		}
		for _, ln := range lines {
			counts[ln.ID] += ln.Count
		}
		hint.Note(counts)
	}
	return arrived, why
}

func (l *liveBones) Left() time.Duration { return time.Until(l.deadline) }

func (l *liveBones) Stopped() bool { return !questrunner.WaitForGuardian(l.d) }

func (l *liveBones) Logf(format string, args ...any) {
	logf(l.d, "[%s] prayer bones: %s", l.id, fmt.Sprintf(format, args...))
}

// prayerBones is the Prayer rungs' first step: the bones loop to the level, or until there is no bone to be had. It tells the
// engine what it gained (Engine.NoteTrained), so that a run which buried a pack of big bones is not parked like one that
// spun. It reports false when it found no bone to bury and the cows are what is left (the engine remembers the failure for
// the run, and questplan.CowsRungStep sends the account to prayerCows); true when the level is reached or when the run
// has too little time left for anything that pays, in which case it also ends the run (Engine.YieldRun).
func prayerBones(e *questrunner.Engine, id string, level int, askMule bool) bool {
	return prayerBonesAt(e, id, level, askMule, nil)
}

// prayerBonesAt is prayerBones with the bank's booth replaced (nil: the real one).
func prayerBonesAt(e *questrunner.Engine, id string, level int, askMule bool, openBank func() (*actions.Bank, bool)) bool {
	d := e.Deps()
	budget := e.RunLeft()
	if budget <= 0 {
		budget = 15 * time.Minute // not in a Run: the old cows budget
	}
	start := time.Now()
	l := &liveBones{e: e, d: d, id: id, skill: "prayer", level: level, deadline: start.Add(budget), openBank: openBank}
	xp0, _ := l.xp()
	t := runPrayerBones(l, askMule)
	xp1, _ := l.xp()
	gained := max(xp1-xp0, 0)
	e.NoteTrained(gained)
	perHour := 0.0
	if h := time.Since(start).Hours(); h > 0 {
		perHour = float64(gained) / h
	}
	if gained > 0 || t.Asks > 0 || t.ToCows || t.Stopped { // not a line for every tick of a level nobody has read yet
		l.Logf("+%d xp in %s (%.0f xp/h): own bank %d, mule %d (%d ask(s)); %d xp still to go to Prayer %d",
			gained, time.Since(start).Round(time.Second), perHour, t.Bank, t.Mule, t.Asks, l.Need(), level)
	}
	if t.Stopped {
		e.YieldRun() // nothing more to do this slot: the next quest starts now, not when the deadline comes
	}
	return l.Need() <= 0 || t.Stopped
}

// prayerCows is the Prayer rungs' fallback step: the cows, for what is left of the run, with the experience they gave
// reported to the engine like the bones' (a slot of cow kills is under questrunner.ProgressTrainXP, so a run of cows alone
// still parks as it did).
func prayerCows(e *questrunner.Engine, id string, level int) bool {
	d := e.Deps()
	l := &liveBones{e: e, d: d, id: id, skill: "prayer", level: level}
	xp0, _ := l.xp()
	start := time.Now()
	budget := cowBudget(e)
	if need := l.Need(); need > 0 {
		// The cows cannot finish a rung that is thousands of xp short, and the log should say so where the stints are read.
		l.Logf("cows: %d xp to go to Prayer %d is %d stints of %s alone in the pen, %d among %d accounts", need, level,
			questplan.CowStints(need, budget, 1), budget.Round(time.Minute), questplan.CowStints(need, budget, questplan.DemonCowFarmers), questplan.DemonCowFarmers)
	}
	ok := cowsTo(e, "prayer", level, budget)
	xp1, _ := l.xp()
	gained := max(xp1-xp0, 0)
	e.NoteTrained(gained)
	perHour := 0.0
	if h := time.Since(start).Hours(); h > 0 {
		perHour = float64(gained) / h
	}
	l.Logf("cows: +%d xp in %s (%.0f xp/h); %d xp still to go to Prayer %d", gained, time.Since(start).Round(time.Second), perHour, l.Need(), level)
	return ok
}
