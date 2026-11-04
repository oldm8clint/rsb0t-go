package bot

import (
	"rsb0t-go/internal/gamestate"
	"rsb0t-go/internal/protocol/serverprot"
)

func mindHoldStat(st *gamestate.State, stat, xp, level int) {
	st.Handle(serverprot.UPDATE_STAT, []byte{byte(stat), byte(xp >> 24), byte(xp >> 16), byte(xp >> 8), byte(xp), byte(level)})
}
