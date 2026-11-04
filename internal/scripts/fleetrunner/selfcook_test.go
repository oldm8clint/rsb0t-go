package fleetrunner

import (
	"rsb0t-go/internal/actions/actionsim"
)

func stackCount(rows []actionsim.Stack, id int32) int {
	n := 0
	for _, r := range rows {
		if r.ID == id {
			n += r.Count
		}
	}
	return n
}
