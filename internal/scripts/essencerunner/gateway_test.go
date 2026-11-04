package essencerunner

import (
	"strings"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/chatlog"
	"rsb0t-go/internal/scripts/essenceplan"
)

func gatewayDeps(sim *actionsim.Session) (*Deps, *[]string) {
	var logs []string
	return &Deps{
		Session: sim, Chat: chatlog.New(),
		Delay: func(d time.Duration) bool { time.Sleep(d / 10); return true },
		Log:   func(s string) { logs = append(logs, s) },
	}, &logs
}

const refusalLine = "You need to have completed the Rune Mysteries Quest to use this feature."

// A teleport that lands is seen at once, with where and how many ticks it took.
func TestAwaitLandingSeesTheMine(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2891, 4830)
	d, _ := gatewayDeps(sim)
	gw, _ := essenceplan.GatewayByKey(essenceplan.GatewayGuild)
	res := awaitLanding(d, gw, time.Now(), 0, 2*time.Second, nil)
	if !res.Landed || res.At.X != 2891 || res.At.Z != 4830 {
		t.Fatalf("%+v", res)
	}
}

// The refusal that cost the fleet 39 timeouts and 25 refusals in one log window, none of them a success:
// the line is answered in the click's own tick, so the wait ends the moment it is seen -- not after the
// fifteen seconds the teleport is given to land.
func TestARefusedTeleportFailsAtOnce(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2594, 3090) // outside, by Distentor
	d, logs := gatewayDeps(sim)
	gw, _ := essenceplan.GatewayByKey(essenceplan.GatewayGuild)
	clickAt := time.Now().Add(-time.Millisecond)
	d.Chat.Record(refusalLine)
	started := time.Now()
	res := awaitLanding(d, gw, clickAt, 0, 15*time.Second, nil)
	if res.Landed || !res.QuestRefused || !strings.Contains(res.Reason, "Rune Mysteries") {
		t.Fatalf("%+v", res)
	}
	if took := time.Since(started); took > 2*time.Second {
		t.Errorf("a refusal took %s to be seen", took)
	}
	if len(*logs) == 0 || !strings.Contains((*logs)[0], "failing at once") || !strings.Contains((*logs)[0], "Wizard Distentor") {
		t.Errorf("log %v", *logs)
	}
}

// A refusal from before the click is not this click's.
func TestAnOldRefusalIsNotThisClicks(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2594, 3090)
	d, logs := gatewayDeps(sim)
	gw, _ := essenceplan.GatewayByKey(essenceplan.GatewayGuild)
	d.Chat.Record(refusalLine)
	time.Sleep(20 * time.Millisecond)
	res := awaitLanding(d, gw, time.Now(), 0, 300*time.Millisecond, nil)
	if res.Landed || res.QuestRefused || !strings.Contains(res.Reason, "did not land") {
		t.Fatalf("%+v", res)
	}
	if len(*logs) == 0 || !strings.Contains((*logs)[len(*logs)-1], "timed out waiting for the essence mine") {
		t.Errorf("log %v", *logs)
	}
}

// A chat-less caller (a tool) still waits for the landing.
func TestAwaitLandingWithoutAChatLog(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2594, 3090)
	d, _ := gatewayDeps(sim)
	d.Chat = nil
	gw := essenceplan.AuburyGateway()
	if res := awaitLanding(d, gw, time.Now(), 0, 200*time.Millisecond, nil); res.Landed || res.QuestRefused {
		t.Fatalf("%+v", res)
	}
}

// A stop request ends the wait.
func TestAwaitLandingStopsWithTheBot(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2594, 3090)
	d, _ := gatewayDeps(sim)
	d.Delay = func(time.Duration) bool { return false }
	res := awaitLanding(d, essenceplan.AuburyGateway(), time.Now(), 0, 5*time.Second, nil)
	if res.Landed || res.Reason != "stopped" {
		t.Fatalf("%+v", res)
	}
}

// An unanswered click is sent again every teleportReclickTicks ticks, and the landing that follows a
// later click is seen and counted: the server drops a click while the player or the wizard is delayed.
func TestAnUnansweredClickIsSentAgain(t *testing.T) {
	sim := actionsim.New(actionsim.Objs{}, nil)
	sim.StandAt(2594, 3090)
	d, logs := gatewayDeps(sim)
	d.Delay = func(time.Duration) bool { sim.Tick(); return true }
	gw, _ := essenceplan.GatewayByKey(essenceplan.GatewayGuild)
	var at []int
	reclick := func(r resend) (chatMark, bool) {
		at = append(at, sim.State().Tick())
		if r.CantReach {
			t.Errorf("resend %+v: nothing was said", r)
		}
		if len(at) == 2 {
			sim.StandAt(2891, 4830) // the second resend lands
		}
		return markChat(d), true
	}
	res := awaitLanding(d, gw, time.Now(), sim.State().Tick(), 5*time.Second, reclick)
	if !res.Landed || res.Clicks != 3 {
		t.Fatalf("%+v (resends at ticks %v)", res, at)
	}
	if len(at) != 2 || at[1]-at[0] != teleportReclickTicks {
		t.Errorf("resends at ticks %v, want two %d ticks apart", at, teleportReclickTicks)
	}
	if len(*logs) == 0 || !strings.Contains((*logs)[len(*logs)-1], "landed after 3 click(s)") {
		t.Errorf("log %v", *logs)
	}
}
