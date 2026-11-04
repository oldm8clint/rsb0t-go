package bot

// The Asgarnian ice dungeon's Ice warrior cave (combatplan's "icewarriors" rung) killed nine ScarabSwarm accounts
// between 15 and 17 Oct 2026: nine deaths in 327 account-hours, against none on any other trainer rung. Its causes
// were an event monster the guardian could not leave (randomevents/event_leash.go) and what the trainer brought
// to the cave: a thin pack, bones buried among aggressive warriors (a buried bone is where a Shade or a Zombie
// spawns), and a death that sent the account straight back to the same stand.

import (
	"os"
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/scripts/combatplan"
	"rsb0t-go/internal/scripts/combatrunner"
)

func iceWarriorsRung(t *testing.T) combatplan.Rung {
	t.Helper()
	r, ok := combatplan.RungByKey(combatplan.SpreadLadder(), "icewarriors")
	if !ok || !r.Herd {
		t.Fatalf("the icewarriors rung is missing or not a Herd rung: %+v", r)
	}
	return r
}

// herdTrainer is an embedded trainer on rung r, recording how its run ended.
func herdTrainer(r combatplan.Rung, ended *[]string) *combatTrainerBot {
	return &combatTrainerBot{
		cfg:      combatTrainerSettings{food: "Bass", eatFrac: 0.5},
		rung:     r,
		embed:    &combatTrainerEmbed{ended: func(reason string, met bool) { *ended = append(*ended, reason) }},
		said:     map[string]bool{},
		warnedAt: map[string]time.Time{},
	}
}

// A trip that came away with 11 or 5 bites of the 20 it is sized for used to "fight on at the 5-bite danger
// floor". At a herd rung it ends the run, with an end reason that makes the next pick avoid the rung.
func TestAThinPackEndsARunAtAHerdRungAndMakesTheNextPickAvoidIt(t *testing.T) {
	iw := iceWarriorsRung(t)
	floor := combatplan.HerdFoodFloor(iw, 13)
	if floor < 12 {
		t.Fatalf("control: the herd floor is %d bites", floor)
	}
	for _, got := range []int{11, 5, floor - 1} {
		var ended []string
		b := herdTrainer(iw, &ended)
		ctx := apiContext(newAPISession(), nil)
		if !b.bankFoodPolicy(ctx, combatrunner.Deposit{}, combatrunner.Withdrawal{}, combatplan.FoodPerTrip(iw, 13), got) {
			t.Fatalf("a trip of %d bites at the herd rung went on", got)
		}
		if len(ended) != 1 || !combatplan.AvoidEnd(ended[0]) || !strings.HasPrefix(ended[0], combatplan.HerdFoodShort) {
			t.Fatalf("a trip of %d bites ended with %q: want an end reason the next pick avoids", got, ended)
		}
	}
	// A full-enough trip goes on, and so does a thin one at a rung that is not a herd.
	var ended []string
	b := herdTrainer(iw, &ended)
	if b.bankFoodPolicy(apiContext(newAPISession(), nil), combatrunner.Deposit{}, combatrunner.Withdrawal{}, combatplan.FoodPerTrip(iw, 13), floor) || len(ended) != 0 {
		t.Fatalf("a trip of %d bites (the floor) ended the run: %v", floor, ended)
	}
	giants, _ := combatplan.RungByKey(combatplan.SpreadLadder(), "giants")
	b = herdTrainer(giants, &ended)
	if b.bankFoodPolicy(apiContext(newAPISession(), nil), combatrunner.Deposit{}, combatrunner.Withdrawal{}, combatplan.FoodPerTrip(giants, 13), 6) || len(ended) != 0 {
		t.Fatalf("a thin trip at the giants ended the run for the herd rule: %v", ended)
	}
}

// The trainer buries nothing and picks up no bones at a herd rung. Structural, as the fight step needs a live
// rung to run.
func TestTheTrainerBuriesNoBonesAtAHerdRung(t *testing.T) {
	src, err := os.ReadFile("combattrainer.go")
	if err != nil {
		t.Fatal(err)
	}
	s := string(src)
	for _, c := range []struct{ fn, want string }{
		{"func (b *combatTrainerBot) fightStep(", "combatrunner.HasBone(d) && !b.rung.Herd"},
		{"func (b *combatTrainerBot) lootStep(", "b.rung.Herd || !combatplan.WantBones("},
	} {
		start := strings.Index(s, c.fn)
		if start < 0 {
			t.Fatalf("no %s in combattrainer.go", c.fn)
		}
		body := s[start:]
		body = body[:strings.Index(body[1:], "\nfunc ")+1]
		if !strings.Contains(body, c.want) {
			t.Errorf("%s does not keep a herd rung bone-free (%q)", c.fn, c.want)
		}
	}
}
