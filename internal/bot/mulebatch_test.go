package bot

import (
	"testing"
	"time"

	"rsb0t-go/internal/scripts/mulereceiverplan"
)

// The promise this whole feature is gated on: a roster that names none of the new keys gets a
// mule that does not travel. the main account's fleet runs this code on 266 accounts with a fixed post, so
// this is the regression that protects it — asserted, not described in a comment.
func TestTravelIsOffAndUnchangedForARosterThatNamesNoNewKey(t *testing.T) {
	for _, settings := range []map[string]interface{}{
		nil,
		{},
		{"allowedPartners": []string{"someone"}, "bank": true, "cookIdle": false},
	} {
		got := readTravelSettings(settings)
		if got.on {
			t.Errorf("travelToRequests defaulted ON for %v — a fixed-post fleet would start walking off", settings)
		}
	}
	// An explicitly false key is still off, and a non-bool value must not be read as true.
	if readTravelSettings(map[string]interface{}{"travelToRequests": false}).on {
		t.Error("an explicit false must stay off")
	}
	if readTravelSettings(map[string]interface{}{"travelToRequests": "yes"}).on {
		t.Error("a string must not switch travelling on")
	}
}

func TestTravelSettingsDefaults(t *testing.T) {
	got := readTravelSettings(nil)
	if got.walkRadius != defaultWalkRadius {
		t.Errorf("walkRadius = %d, want %d", got.walkRadius, defaultWalkRadius)
	}
	// The whole point of the larger-than-mulereceiverplan default: the two banks this fleet
	// uses must be a WALK, because this mule does not cast a teleport. If this ever drops below
	// the real distance, every trip between them is refused instead.
	apart := chebyshevTiles(mulereceiverplan.SeersStand.X, mulereceiverplan.SeersStand.Z, 2809, 3441)
	if got.walkRadius < apart {
		t.Errorf("walkRadius %d is under the %d tiles between the Seers stand and the Catherby bank — PlanTrip would refuse the trip this feature exists for", got.walkRadius, apart)
	}
	if got.maxLegTiles != mulereceiverplan.MuleDefaults().MaxLegTiles {
		t.Errorf("maxLegTiles = %d, want the plan package's own default %d", got.maxLegTiles, mulereceiverplan.MuleDefaults().MaxLegTiles)
	}
	if got.campRadius != defaultCampRadius || got.maxCampsInARow != defaultMaxCampsInARow || got.dwell != defaultCampDwell {
		t.Errorf("camp defaults = %d/%d/%s", got.campRadius, got.maxCampsInARow, got.dwell)
	}
	// Two donors at the same bank must land in one camp, and the two banks must not.
	if got.campRadius >= apart {
		t.Errorf("campRadius %d would merge the two banks (%d apart) into a single camp", got.campRadius, apart)
	}
}

func TestTravelSettingsAreReadAndClamped(t *testing.T) {
	got := readTravelSettings(map[string]interface{}{
		"travelToRequests": true,
		"walkRadius":       200,
		"maxLegTiles":      120,
		"campRadius":       10,
		"maxCampsInARow":   1,
		"campDwellSec":     240,
	})
	if !got.on || got.walkRadius != 200 || got.maxLegTiles != 120 || got.campRadius != 10 || got.maxCampsInARow != 1 || got.dwell != 4*time.Minute {
		t.Errorf("settings not read through: %+v", got)
	}
	// A typo must not produce a mule that walks nowhere or camps forever.
	clamped := readTravelSettings(map[string]interface{}{
		"walkRadius": 0, "maxLegTiles": -5, "campRadius": -1, "maxCampsInARow": -2, "campDwellSec": 1,
	})
	if clamped.walkRadius < 1 || clamped.maxLegTiles < 1 || clamped.campRadius != 0 || clamped.maxCampsInARow != 0 || clamped.dwell != minCampDwell {
		t.Errorf("clamping let a bad value through: %+v", clamped)
	}
}

// maxCampsInARow of 0 is the documented "no cap, age alone decides" case, which ChooseCamp reads
// directly — proven here so the setting's documentation and the plan package agree.
func TestMaxCampsInARowZeroDisablesTheCap(t *testing.T) {
	busy := mulereceiverplan.PickupRequest{From: "fisher", Tile: mulereceiverplan.SeersStand, FirstAt: time.Unix(10, 0)}
	camps := mulereceiverplan.GroupByCamp([]mulereceiverplan.PickupRequest{busy}, defaultCampRadius)
	at := mulereceiverplan.SeersStand
	got, ok := mulereceiverplan.ChooseCamp(mulereceiverplan.ChooseCampInput{
		Camps: camps, Here: at, ServedAt: &at, InARow: 99, MaxInARow: 0, SameCampRadius: defaultCampRadius,
	})
	if !ok || got.Camp.At != at {
		t.Errorf("with the cap off the oldest request still wins however many batches ran there: %+v ok=%v", got.Camp.At, ok)
	}
}

func chebyshevTiles(ax, az, bx, bz int) int {
	dx, dz := ax-bx, az-bz
	if dx < 0 {
		dx = -dx
	}
	if dz < 0 {
		dz = -dz
	}
	if dx > dz {
		return dx
	}
	return dz
}
