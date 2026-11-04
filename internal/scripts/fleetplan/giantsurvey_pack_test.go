package fleetplan

import (
	"fmt"
)

type surveyHerd struct {
	name    string
	threats []PenThreat
}

func spawnsOf(herd []PenThreat, idx []int) []string {
	var out []string
	for _, i := range idx {
		out = append(out, fmt.Sprintf("%d,%d", herd[i].At.X, herd[i].At.Z))
	}
	return out
}
