package freshstartrunner

import (
	"time"

	"rsb0t-go/internal/actions"
	"rsb0t-go/internal/scripts/freshstart"
)

// BrightPending reports whether the brightness still wants turning up to very bright: not very
// bright yet (varp 166 option_brightness) and not given up on. Ported from newAccount.ts's
// Brightness.done, which both ProgressiveFisher.ts and ProgressiveCrafter.ts put in their snapshot.
func BrightPending(d *Deps, b *freshstart.Brightness) bool {
	return !b.Done(d.Session.State().Varp(freshstart.BrightnessVarp))
}

// BrightnessStep is one pass of Brightness.step: press the Very Bright button on whichever options
// interface the options tab holds (IF_SETICON tells which) and wait for the varp to read very
// bright. After freshstart.BrightTries misses it gives up so the run is not held up. The TS step
// also switches the side tab to options and back; that is client-only (no packet), so the button
// is pressed directly.
func BrightnessStep(d *Deps, b *freshstart.Brightness) {
	root := d.Session.State().SideIcon(freshstart.OptionsTab)
	button, known := freshstart.VeryBrightButton(root)
	pressed := false
	if known {
		if err := actions.IfButton(d.Session, button); err != nil {
			d.logf("press Very Bright: %v", err)
		} else {
			pressed = true
		}
	}
	if pressed && waitFor(d, "the brightness to read very bright", 3*time.Second, func() bool {
		return d.Session.State().Varp(freshstart.BrightnessVarp) == freshstart.VeryBright
	}) {
		d.logf("brightness set to very bright")
		return
	}
	if b.Missed() {
		if known {
			d.logf("could not set the brightness (options tab holds interface %d) — carrying on without it", root)
		} else {
			d.logf("could not set the brightness (options tab holds interface %d, whose brightness buttons are not known) — carrying on without it", root)
		}
		return
	}
	d.Delay(1200 * time.Millisecond)
}
