package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/wcfletchrunner"
	"rsb0t-go/internal/webwalk"
)

func TestWalkStallSettingDefaultsOn(t *testing.T) {
	if !newWalkStall(map[string]interface{}{}, "x").on() {
		t.Fatal("walkRub must default to true, as ScarabSwarm's and the fishers' do")
	}
	off := newWalkStall(map[string]interface{}{"walkRub": false}, "x")
	if off.on() {
		t.Fatal("walkRub=false left the stall on")
	}
	if off.keepNames() != nil {
		t.Fatal("an off stall keeps names")
	}
	var nilStall *walkStall
	if nilStall.on() || nilStall.keepNames() != nil {
		t.Fatal("a nil stall is on")
	}
	names := newWalkStall(nil, "x").keepNames()
	if len(names) != 16 || names[0] != "Games necklace(8)" {
		t.Fatalf("keepNames = %v", names)
	}
}

func TestWalkStallInstallOnNilWalkerIsANoOp(t *testing.T) {
	newWalkStall(nil, "x").install(&Context{}, nil) // an account whose webwalk failed to build has no walker
}

func TestWalkStallInstallSetsPrepareAndLeavesAnOwnAsideAlone(t *testing.T) {
	w := &webwalk.Walker{}
	newWalkStall(nil, "x").install(&Context{}, w)
	if w.Prepare == nil {
		t.Fatal("install left the Walker without a Prepare")
	}
	own := func(webwalk.AsidePoll) {}
	o := webwalk.WalkOptions{Aside: own}
	if done := w.Prepare(&o); done != nil || o.Aside == nil {
		t.Fatal("the stall took over a walk with its own aside")
	}
}

func TestWalkFletchCarriesItsSetting(t *testing.T) {
	if !wcfletchrunner.NewWalkFletch(true).On || wcfletchrunner.NewWalkFletch(false).On {
		t.Fatal("NewWalkFletch does not carry its setting")
	}
	var nilWF *wcfletchrunner.WalkFletch
	if nilWF.Allowed(time.Now()) {
		t.Fatal("a nil walk-fletch is allowed")
	}
	if !wcfletchrunner.NewWalkFletch(true).Allowed(time.Now()) {
		t.Fatal("an on walk-fletch is not allowed")
	}
}
