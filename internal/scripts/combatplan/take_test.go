package combatplan

import "testing"

func TestJudgeTake(t *testing.T) {
	const settle, timeout = 1200, 20000

	cases := []struct {
		name string
		obs  TakeObservation
		want TakeVerdict
	}{
		{
			name: "still lying there a moment after the click",
			obs:  TakeObservation{OnGround: true, SinceMs: 300},
			want: TakeWaiting,
		},
		{
			name: "the pack gained it",
			obs:  TakeObservation{PackGained: true, OnGround: false, MissingForMs: 200, SinceMs: 600},
			want: TakeDone,
		},
		{
			// The race this exists for: our own pickup sends OBJ_DEL and the inventory update,
			// and the ground half can be decoded first. A judge that read the ground first
			// would call a successful take "gone".
			name: "our own OBJ_DEL arrived before the inventory update",
			obs:  TakeObservation{PackGained: true, OnGround: false, MissingForMs: settle + 500, SinceMs: 2000},
			want: TakeDone,
		},
		{
			name: "missing for less than the settle window proves nothing yet",
			obs:  TakeObservation{OnGround: false, MissingForMs: settle - 1, SinceMs: 800},
			want: TakeWaiting,
		},
		{
			name: "somebody else took it",
			obs:  TakeObservation{OnGround: false, MissingForMs: settle, SinceMs: 1500},
			want: TakeGone,
		},
		{
			// The only outcome that actually accuses the click: the stack never moved and the
			// pack never changed.
			name: "still on the ground when the budget ran out",
			obs:  TakeObservation{OnGround: true, SinceMs: timeout},
			want: TakeTimedOut,
		},
		{
			name: "a gain on the very last poll still wins",
			obs:  TakeObservation{PackGained: true, OnGround: true, SinceMs: timeout + 5000},
			want: TakeDone,
		},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := JudgeTake(c.obs, settle, timeout); got != c.want {
				t.Fatalf("JudgeTake(%+v) = %q, want %q", c.obs, got, c.want)
			}
		})
	}
}
