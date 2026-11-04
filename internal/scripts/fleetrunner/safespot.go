package fleetrunner

import (
	"fmt"
)

func SafeGapKey(d *Deps) string { return fmt.Sprintf("%s@%d", d.Me, d.World) }

// PackCountOf is how many of obj id the pack holds (stack counts summed).
func PackCountOf(d *Deps, id int) int {
	n := 0
	for _, it := range PackSlotItems(d) {
		if int(it.ID) == id {
			n += int(it.Count)
		}
	}
	return n
}
