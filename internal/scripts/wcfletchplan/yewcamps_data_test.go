package wcfletchplan_test

import (
	"testing"

	"rsb0t-go/internal/scripts/wcfletchplan"
)

// TestYewCampsAgreeWithTheLocIndex re-derives every yew camp's tree count and walk from the real
// loc index and pack and holds it to the maple camps' safety rules (maplecamps_data_test.go's
// shared checker), plus the one rule the yew camps were drawn with and the maple camps were not:
// every counted tree has a stand tile within the camp's leash of its anchor, so an account
// chopping it counts as at the camp. A content or pack change fails here instead of sending the
// yew cohort to a camp that is not there or past something that kills it.
//
//	RSB0T_LOC_INDEX=<locs.lcli.gz> RSB0T_CONFIG_JAG=<config archive> \
//	RSB0T_COLLISION_PACK=<collision.lcnav.gz> go test -run 'TestYewCamps|TestNoOtherYew' ./internal/scripts/wcfletchplan/
func TestYewCampsAgreeWithTheLocIndex(t *testing.T) {
	checkCampsAgainstTheData(t, wcfletchplan.YewCamps, true)
}

// TestNoOtherYewIsSafeAndInReach is the other half: every yew within SeersRadius of the bank is
// in a camp. The next yews out are 157 and 160 tiles from the bank (2736,3334 and 2731,3331),
// and past those 229 (2496,3490): all outside SeersRadius, where DecideWoodPhase stops treating
// an account as at Seers and sends it down the fare road. A yew that turns up inside the radius
// (a content change) and is safe fails here until it is given a camp.
func TestNoOtherYewIsSafeAndInReach(t *testing.T) {
	checkNoOtherTreeIsSafe(t, "Yew", wcfletchplan.YewCamps, nil)
}
