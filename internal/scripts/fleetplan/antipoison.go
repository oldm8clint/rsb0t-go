package fleetplan

import "strings"

// AntipoisonForms are the antipoison potions a Queen life carries, fewest doses first, so a life drinks the
// Observatory's one-dose spawns (2437,3164 and 2467,3177) before it opens a fuller potion. One dose cures and
// holds poison off for five of poison's 30-tick cycles (anti_poison.rs2: %poison = min(%poison,-5)), about
// 90 seconds.
//
// Why a life needs them (1 Nov 2026, the overnight log): poison alone killed 38 of 207 zero-cast chamber
// lives, and was on the killer list of most of the rest; nothing in Stage 6 handled it.
//
// Superantipoison (a user request: the mule holds 1,000 Superantipoison(4)) comes last, so the mule ask, which walks this
// list from the end, offers it first.
var AntipoisonForms = []string{
	"Antipoison(1)", "Antipoison(2)", "Antipoison(3)", "Antipoison(4)",
	"Superantipoison(1)", "Superantipoison(2)", "Superantipoison(3)", "Superantipoison(4)",
}

// AntipoisonDoses is the doses an antipoison potion name holds, 0 for anything else.
func AntipoisonDoses(name string) int {
	n := strings.TrimSpace(name)
	for i, f := range AntipoisonForms {
		if strings.EqualFold(n, f) {
			return i%4 + 1
		}
	}
	return 0
}

// AntipoisonDue says a life should drink now: it is poisoned (a poison hitmark recent enough to still be
// ticking) and that hitmark came after the last dose it drank, so the dose has worn off or been beaten.
func AntipoisonDue(poisoned bool, lastPoisonTick, lastDoseTick int) bool {
	return poisoned && lastPoisonTick > lastDoseTick
}
