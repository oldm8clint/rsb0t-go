package bot

// The embedded trainer's in-fight watch (combattrainer_embed.go). The stage5ab lab A/B of
// 2026-09-25 lost two Scarab accounts inside one combatrunner.FightOne each, food in the pack and
// no bite taken: FightOne waits up to 90s for the kill and the trainer only ever ate between
// fights. These drive the REAL wait (combatrunner.WaitForKill, which FightOne ends in) through the
// watched Deps, on a real gamestate whose hitpoints come off UPDATE_STAT.

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
	"rsb0t-go/internal/scripts/levels"
)

func fightGuardHP(st *gamestate.State, hp, level int) {
	xp := levels.XPAtLevel(level)
	st.Handle(serverprot.UPDATE_STAT, []byte{byte(gamestate.HitpointsStat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(hp)})
}

// fightGuardFixture is an embedded trainer (Cake, eat below 50%) on an account at hp of 31, and
// the Deps its fight runs on, whose Delay is quick so a test wait is short.
func fightGuardFixture(t *testing.T, hp int) (*combatTrainerBot, *Context, *combatrunner.Deps, *apiSession) {
	t.Helper()
	sess := newAPISession()
	fightGuardHP(sess.st, 31, 31) // MaxHP is a high-water mark: see it at 31 first
	fightGuardHP(sess.st, hp, 31)
	if got, _ := sess.st.HP(); got != hp {
		t.Fatalf("fixture: HP reads %d, want %d", got, hp)
	}
	ctx := apiContext(sess, nil)
	b := &combatTrainerBot{
		cfg:   combatTrainerSettings{food: "Cake", eatFrac: 0.5},
		embed: &combatTrainerEmbed{guardFights: true},
	}
	d := &combatrunner.Deps{Session: sess, Delay: func(time.Duration) bool { time.Sleep(time.Millisecond); return true }}
	return b, ctx, d, sess
}

func TestDecideInFightEatsUnderTheThresholdAndBreaksOffOnlyWithNoFood(t *testing.T) {
	heal := 4 // a Cake bite
	for _, c := range []struct {
		hp, max, food int
		want          inFight
		why           string
	}{
		{26, 31, 5, inFightOn, "healthy with food: fight on"},
		{16, 31, 5, inFightOn, "16 of 31 is not under half"},
		{15, 31, 5, inFightEat, "15 of 31 is under half: eat"},
		{3, 31, 1, inFightEat, "the safety floor with one bite: eat"},
		{15, 31, 0, inFightBreakOff, "under half with nothing to eat: break off"},
		{20, 31, 0, inFightOn, "no food but healthy: the kill may still come"},
		{0, 31, 5, inFightOn, "HP unknown or dead: nothing to decide"},
		{10, 0, 0, inFightOn, "MaxHP unknown: nothing to decide"},
	} {
		if got := decideInFight(c.hp, c.max, c.food, heal, 0.5); got != c.want {
			t.Errorf("%s: decideInFight(%d/%d, %d food) = %v, want %v", c.why, c.hp, c.max, c.food, got, c.want)
		}
	}
}

// The G2 death, replayed on the wait itself: 10 of 31 hp with three Cakes, mid-fight. The watched
// wait eats until it is back over half (10 -> 14 -> 18: two bites) and keeps waiting for the kill.
func TestAnEmbeddedTrainersFightEatsDuringTheWait(t *testing.T) {
	b, ctx, d, sess := fightGuardFixture(t, 10)
	w := b.newFightWatch(ctx, d, gamestate.NPC{Index: 77})
	if w == nil {
		t.Fatal("an embedded trainer with guardFights got no fight watch: its fights are unwatched")
	}
	food := 3
	w.food = func() int { return food }
	w.eat = func() bool {
		food--
		hp, _ := sess.st.HP()
		fightGuardHP(sess.st, hp+4, 31)
		return true
	}
	start := time.Now()
	v, _ := combatrunner.WaitForKill(watchedDeps(d, w), 77, 300*time.Millisecond)
	if w.bites != 2 || food != 1 {
		t.Fatalf("ate %d bites (%d food left), want 2 (10 -> 14 -> 18 of 31, then over half)", w.bites, food)
	}
	if hp, _ := sess.st.HP(); hp != 18 {
		t.Fatalf("HP after the fight = %d, want 18", hp)
	}
	if w.brokeOff || v != combatplan.Unknown || time.Since(start) < 250*time.Millisecond {
		t.Fatalf("a fed fight was cut short: brokeOff %v, verdict %v after %s", w.brokeOff, v, time.Since(start))
	}
}

// With nothing left to eat and under half, the watched wait breaks the fight off at once instead
// of waiting the fight out to 0 hp (T1: 14 hp to 0 among six Al-Kharid warriors).
func TestAnEmbeddedTrainersFightBreaksOffWithNoFoodUnderHalf(t *testing.T) {
	b, ctx, d, _ := fightGuardFixture(t, 12)
	w := b.newFightWatch(ctx, d, gamestate.NPC{Index: 77})
	if w == nil {
		t.Fatal("no fight watch")
	}
	w.food = func() int { return 0 }
	w.eat = func() bool { t.Fatal("ate with no food"); return false }
	start := time.Now()
	v, _ := combatrunner.WaitForKill(watchedDeps(d, w), 77, 3*time.Second)
	if !w.brokeOff || w.brokeAt != 12 {
		t.Fatalf("brokeOff %v at %d hp, want a break-off at 12", w.brokeOff, w.brokeAt)
	}
	if took := time.Since(start); took > time.Second || v != combatplan.Unknown {
		t.Fatalf("the break-off took %s (verdict %v): the wait was not cut short", took, v)
	}
	// Sticky: once off, every later poll stays off.
	if w.step() {
		t.Fatal("a broken-off fight resumed on the next poll")
	}
}

// The registered CombatTrainer, and an embed that did not ask for it, keep FightOne unwatched.
func TestTheRegisteredTrainersFightsAreUnwatched(t *testing.T) {
	_, ctx, d, _ := fightGuardFixture(t, 10)
	reg := &combatTrainerBot{cfg: combatTrainerSettings{food: "Cake", eatFrac: 0.5}}
	if reg.newFightWatch(ctx, d, gamestate.NPC{Index: 77}) != nil {
		t.Fatal("the registered trainer's fights are watched: its behaviour changed")
	}
	quiet := &combatTrainerBot{cfg: reg.cfg, embed: &combatTrainerEmbed{}}
	if quiet.newFightWatch(ctx, d, gamestate.NPC{Index: 77}) != nil {
		t.Fatal("an embed without guardFights got a fight watch")
	}
	// And its eater stays combatrunner.EatOnce: a host eater is used only when one is passed.
	called := false
	host := &combatTrainerBot{cfg: reg.cfg, embed: &combatTrainerEmbed{eat: func(f string) bool { called = f == "Cake"; return true }}}
	if !host.eatOne(d) || !called {
		t.Fatal("an embedding host's eater was not used")
	}
}

// fightStep must run every fight and every bite through the embed's hooks (fightOne, eatOne): a
// direct combatrunner.FightOne or EatOnce there is a fight the host cannot watch. Structural,
// because fightStep needs a live rung to run.
func TestFightStepRunsEveryFightAndBiteThroughTheEmbedHooks(t *testing.T) {
	src, err := os.ReadFile("combattrainer.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	start := strings.Index(s, "func (b *combatTrainerBot) fightStep(")
	if start < 0 {
		t.Fatal("no fightStep in combattrainer.go")
	}
	body := s[start:]
	body = body[:strings.Index(body[1:], "\nfunc ")+1]
	for _, want := range []string{"b.fightOne(ctx, d, npc)", "b.eatOne(d)"} {
		if !strings.Contains(body, want) {
			t.Errorf("fightStep does not call %s", want)
		}
	}
	for _, bare := range []string{"combatrunner.FightOne(", "combatrunner.EatOnce("} {
		if strings.Contains(body, bare) {
			t.Errorf("fightStep calls %s directly, bypassing the embed's watch", bare)
		}
	}
}

// Review of 95ed10ad, finding 5: the watch ate through the break-off reserve, so a fight that ran
// the pack down left for the bank with nothing to eat on the way. A bite that takes the food down
// to the reserve now breaks the fight off (atReserve), which fightStep answers with its own eat and
// restock rules; with no reserve set, the watch keeps eating as before.
func TestAWatchedFightBreaksOffAtTheFoodReserve(t *testing.T) {
	for _, c := range []struct {
		reserve   int
		wantBites int
		wantOff   bool
	}{
		{2, 1, true},  // 3 bites, reserve 2: one bite, then off with two in hand
		{0, 2, false}, // control: no reserve, eats back over half and fights on
	} {
		b, ctx, d, sess := fightGuardFixture(t, 10)
		w := b.newFightWatch(ctx, d, gamestate.NPC{Index: 77})
		food := 3
		w.food = func() int { return food }
		w.reserve = func() int { return c.reserve }
		w.eat = func() bool {
			food--
			hp, _ := sess.st.HP()
			fightGuardHP(sess.st, hp+4, 31)
			return true
		}
		combatrunner.WaitForKill(watchedDeps(d, w), 77, 200*time.Millisecond)
		if w.bites != c.wantBites || w.brokeOff != c.wantOff || w.atReserve != c.wantOff {
			t.Errorf("reserve %d: %d bites, brokeOff %v, atReserve %v; want %d, %v, %v",
				c.reserve, w.bites, w.brokeOff, w.atReserve, c.wantBites, c.wantOff, c.wantOff)
		}
	}
}
