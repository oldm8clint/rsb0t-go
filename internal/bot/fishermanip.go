package bot

import (
	"fmt"
	"strings"
	"time"

	"rsb0t-go/internal/scripts/fisherplan"
	"rsb0t-go/internal/scripts/fisherrunner"
)

// ProgressiveFisher's and BassOysterCaskets' side of the fishing tick manipulation (fisherrunner/fishmanip.go, the pure rules
// in fishmanipplan; a user request: all tick manipulation is high priority, maximum XP an hour). A fisher that carries a
// knife and a plain log fishes its net, big net, lobster pot or harpoon spot with a knife-on-log batch and one lone spot click
// every 3 ticks instead of the engine's one roll every 6.
//
// The kit is one knife and a few plain logs. The knife is kept in the pack through every bank deposit
// (fisherrunner.ManipConfig.KeepsName); the logs are banked with everything else and drawn back, a few, at the bank visit
// (fisherrunner.DrawManipKit), because keeping logs in a deposit leaves a pack full of the Fletching training's logs full for
// ever. Neither crosses to the mule on a hand-off, and the kit is asked of the mule on the hand-off the account makes anyway (fisherKitLines,
// bassoyster_fletch.go) when neither the pack nor the bank holds them. An account without the kit fishes the plain way and
// says so once ("fish manip: no knife...").
//
// Setting: fishManip (bool, default true). False is the plain click and asks the mule for nothing on its account.

// manipLogsWanted is how many plain logs the kit asks for. A batch cancels the fletch it queues, so a log is used up only when
// a batch is torn (a lost tick), and three last a long while; the pack slots they take are a fish less each.
const manipLogsWanted = fisherrunner.ManipLogsWanted

// readFisherManip reads fishManip (default true).
func readFisherManip(settings map[string]interface{}) bool {
	return BoolSetting(settings, "fishManip", true)
}

// manipConfig is the manipulation the bot's Deps carry: the one state the bot keeps across every Deps it builds, so the
// measured link delay and the plain baseline survive from one call to the next. nil while it is off.
func (b *fisherBot) manipConfig() *fisherrunner.ManipConfig {
	if !b.manipOn {
		return nil
	}
	if b.manip == nil {
		b.manip = fisherrunner.NewManipState()
	}
	return &fisherrunner.ManipConfig{Enabled: true, State: b.manip}
}

// bankRepeatMin is the least time between the end of one bank step and the start of the next.
const bankRepeatMin = 10 * time.Second

// bankRepeatWait is how long the next bank step must wait for the last one (ended at last) to be bankRepeatMin old; 0 for none.
func bankRepeatWait(last, now time.Time) time.Duration {
	if last.IsZero() {
		return 0
	}
	if wait := bankRepeatMin - now.Sub(last); wait > 0 {
		return wait
	}
	return 0
}

// bankWhy says what in the snapshot sent the account to the bank (fisherplan.DecideFishPhase's last test), for the log.
func bankWhy(s fisherplan.FishSnapshot) string {
	var why []string
	if s.PackFull {
		why = append(why, fmt.Sprintf("the pack is full (%d raw fish held)", s.FishHeld))
	}
	if s.Coins > 0 {
		why = append(why, fmt.Sprintf("the pack holds %d coins", s.Coins))
	}
	why = append(why, fmt.Sprintf("tools held %v, banked %v", s.HeldTools, s.BankTools))
	return strings.Join(why, ", ")
}
