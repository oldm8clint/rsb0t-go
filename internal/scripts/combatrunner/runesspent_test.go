package combatrunner

import (
	"reflect"
	"testing"
)

func TestRunesSpentCountsOnlyWhatThePackLost(t *testing.T) {
	before := map[int32]int{554: 300, 556: 100, 558: 50}
	after := map[int32]int{554: 300, 556: 97, 558: 47, 560: 5}
	want := map[int32]int{556: 3, 558: 3}
	if got := runesSpent(before, after); !reflect.DeepEqual(got, want) {
		t.Errorf("spent %v, want %v (a staff's fire runes untouched, a rune picked up not negative)", got, want)
	}
	if !isRune(554) || !isRune(566) || isRune(553) || isRune(995) {
		t.Error("isRune bounds wrong")
	}
}
