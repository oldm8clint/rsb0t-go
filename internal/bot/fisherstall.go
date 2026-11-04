package bot

import (
	"rsb0t-go/internal/scripts/fisherrunner"
)

// ProgressiveFisher's and BassOysterCaskets' side of the run staller (fisherrunner/stall.go; owner, 6 Oct
// 2026: "the same interaction can be used for the fishers bank trips when travelling back to fishing
// spot, to be used for both progressivefisher and bassoyster scripts"). The walks rub a carried Games
// necklace or Ring of dueling; this file is where the one rub item an account carries comes from when its
// pack holds none: its own bank first (fisherrunner.DrawRubItem at every bank visit), and failing that the
// mule, on the hand-off the account is making anyway.
//
// The ask RIDES the hand-off: the account stands at the mule's post with its load just traded, so one more
// line in the kit it asks for there (bassoyster_fletch.go's handoffKit, which judges every line against the
// mule's published bank first, as ScarabSwarm's asks are) costs one more trade and no walk
// (scarabplan.RubAskLine: "a Rider: it never starts a walk, and a mule that holds none answers it with
// nothing"). Nothing is asked of a mule that holds none, which was every mule until the jewellery round made
// Games necklaces on 6 Oct.
//
// Setting: walkRub (bool, default true), the same key and meaning as ScarabSwarm's.

// readFisherStall reads walkRub (default true, the same key and default as ScarabSwarm's).
func readFisherStall(settings map[string]interface{}) *fisherrunner.Stall {
	return &fisherrunner.Stall{On: BoolSetting(settings, "walkRub", true)}
}

// stallOn is whether this bot's run staller is on.
func (b *fisherBot) stallOn() bool { return b.stall != nil && b.stall.On }

// stallKeepNames is the names a hand-off must never give the mule: every Games necklace and Ring of
// dueling, in the pack and in the bank, while the stall is on.
func (b *fisherBot) stallKeepNames() []string {
	if !b.stallOn() {
		return nil
	}
	return fisherrunner.RubNames()
}
