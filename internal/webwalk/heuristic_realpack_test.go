package webwalk

import "testing"

// liveSlowRoutes are routes the live fleet planned on 4 October 2026 that expanded 100k-300k
// nodes each under the old long-edge floor, which capped the heuristic near every dungeon
// ladder's landing and turned most searches into Dijkstra.
var liveSlowRoutes = [][2]Point{
	{{X: 2972, Z: 3314}, {X: 3203, Z: 3397}},
	{{X: 3032, Z: 3217, Level: 1}, {X: 3253, Z: 3420}},
	{{X: 2816, Z: 3182}, {X: 3253, Z: 3420}},
	{{X: 2775, Z: 3234, Level: 1}, {X: 3253, Z: 3420}},
	{{X: 2977, Z: 3384}, {X: 3321, Z: 3188}},
	{{X: 2655, Z: 3283}, {X: 3203, Z: 3433}},
	{{X: 2666, Z: 3111}, {X: 3253, Z: 3420}},
	{{X: 2613, Z: 3094}, {X: 3253, Z: 3420}},
	{{X: 2975, Z: 3314}, {X: 3113, Z: 9836}},
	{{X: 3113, Z: 9836}, {X: 2975, Z: 3314}},
}

// TestHeuristicMatchesDijkstraOnLiveRoutes checks the A* bound costs the same as Dijkstra on
// the live routes, and logs how many nodes each expands.
func TestHeuristicMatchesDijkstraOnLiveRoutes(t *testing.T) {
	p := realPack(t)
	for _, opt := range []Options{{}, {AllowNPC: true, AllowSpecial: true}} {
		for _, r := range liveSlowRoutes {
			fast := p.Plan(r[0], r[1], opt)
			slowOpt := opt
			slowOpt.noHeuristic = true
			slow := p.Plan(r[0], r[1], slowOpt)
			if fast.OK != slow.OK || fast.Cost != slow.Cost {
				t.Errorf("%s -> %s: A* ok=%v cost %d, Dijkstra ok=%v cost %d", r[0], r[1], fast.OK, fast.Cost, slow.OK, slow.Cost)
			}
			t.Logf("%s -> %s: cost %d, expanded %d (Dijkstra %d)", r[0], r[1], fast.Cost, fast.Expanded, slow.Expanded)
		}
	}
}

func BenchmarkPlanLiveRoutes(b *testing.B) {
	p := realPack(b)
	opt := Options{AllowNPC: true, AllowSpecial: true}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		for _, r := range liveSlowRoutes {
			p.Plan(r[0], r[1], opt)
		}
	}
}
