package questmods

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/scripts/questplan"
	"rsb0t-go/internal/scripts/questrunner"
)

// The longhall table answers an account that owns a keg of beer (in its bank, say) with one game message and gives
// nothing. The pickup looks for exactly that line instead of waiting for a respawn that is not coming (live 13-14 Oct
// 2026: three bursts of nine retries, twelve seconds apart, one account).
func TestTheKegPickupRecognisesTheLonghallsRefusal(t *testing.T) {
	d := &questrunner.Deps{Chat: chatlog.New()}
	mark := time.Now()
	if sawSince(d, mark, questplan.ManniKegRefusal) {
		t.Fatal("a refusal seen before any message")
	}
	d.Chat.Record("Nothing interesting happens.")
	if sawSince(d, mark, questplan.ManniKegRefusal) {
		t.Error("an unrelated message counted as the refusal")
	}
	d.Chat.Record(questplan.ManniKegRefusal)
	if !sawSince(d, mark, questplan.ManniKegRefusal) {
		t.Error("the longhall's own refusal was not recognised")
	}
	// An earlier step's refusal is not this one's.
	if sawSince(d, time.Now().Add(time.Second), questplan.ManniKegRefusal) {
		t.Error("a refusal from before the mark counted")
	}
}

// The refusal's log line says what the engine's last bank read holds, so a stuck account's log shows the keg that the
// next step will take out.
func TestTheRefusalLogSaysWhatTheBankHolds(t *testing.T) {
	e := &questrunner.Engine{}
	if got := bankHolding(e, questplan.ObjBeerKeg); !strings.Contains(got, "not been read") {
		t.Errorf("no bank read yet: %q", got)
	}
	e.Provisioner().Bank = &questrunner.BankView{}
	e.Provisioner().Bank.Note(map[int]int{questplan.ObjBeerKeg: 1, questplan.ObjLowAlcKeg: 15})
	if got := bankHolding(e, questplan.ObjBeerKeg); !strings.Contains(got, "holds 1") {
		t.Errorf("one keg banked: %q", got)
	}
	if got := bankHolding(e, questplan.ObjLowAlcKeg); !strings.Contains(got, "holds 15") {
		t.Errorf("fifteen low alcohol kegs banked: %q", got)
	}
	if got := bankHolding(e, questplan.ObjBeer); !strings.Contains(got, "holds 0") {
		t.Errorf("none banked: %q", got)
	}
}

// The table's keg is back two ticks after a pickup (respawnrate=2 in content): a retry for longer than a few seconds is
// not a respawn wait, it is the refusal above or an empty table.
func TestTheKegTableWaitIsAFewSecondsNotAMinute(t *testing.T) {
	if kegTableWait < 3*time.Second || kegTableWait > 15*time.Second {
		t.Errorf("the table is retried for %s; the keg respawns in 1.2 s", kegTableWait)
	}
}
