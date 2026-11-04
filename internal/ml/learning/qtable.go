// Package learning contains small, dependency-free learning algorithms for offline experiments.
package learning

import (
	"math/rand"

	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

// QTable is a compact tabular learner. It is intended for deterministic
// simulation first; it does not persist models or execute live actions.
type QTable struct {
	values map[string][]float32
	alpha  float32
	gamma  float32
	rng    *rand.Rand
}

func NewQTable(seed int64, alpha, gamma float32) *QTable {
	if alpha <= 0 {
		alpha = .2
	}
	if gamma < 0 || gamma > 1 {
		gamma = .95
	}
	return &QTable{values: make(map[string][]float32), alpha: alpha, gamma: gamma, rng: rand.New(rand.NewSource(seed))}
}

var actions = [...]policy.Action{policy.WalkToFish, policy.Fish, policy.WalkToBank, policy.Bank, policy.Wait, policy.Recover}

func (q *QTable) valuesFor(key string) []float32 {
	v, ok := q.values[key]
	if !ok {
		v = make([]float32, len(actions))
		q.values[key] = v
	}
	return v
}

// StateKey intentionally buckets observations so the table remains small.
func StateKey(s observation.State) string {
	inv := s.InventoryUsed
	if inv > 28 {
		inv = 28
	}
	return string(rune(s.InventorySize)) + ":" + string(rune(inv)) + ":" + string(rune(boolByte(s.DistanceToFish))) + ":" + string(rune(boolByte(s.DistanceToBank))) + ":" + string(rune(s.NearbyFish)) + ":" + string(rune(s.NearbyBank))
}

func boolByte(v uint16) byte {
	if v > 0 {
		return 1
	}
	return 0
}

func actionIndex(a policy.Action) int {
	for i, x := range actions {
		if x == a {
			return i
		}
	}
	return 0
}

func (q *QTable) Best(s observation.State) policy.Recommendation {
	v := q.valuesFor(StateKey(s))
	best := 0
	for i := 1; i < len(v); i++ {
		if v[i] > v[best] {
			best = i
		}
	}
	return policy.Recommendation{Action: actions[best], Confidence: 1}
}

func (q *QTable) Choose(s observation.State, epsilon float32) policy.Recommendation {
	if epsilon > 0 && q.rng.Float32() < epsilon {
		return policy.Recommendation{Action: actions[q.rng.Intn(len(actions))], Confidence: 0}
	}
	return q.Best(s)
}

// Update applies the standard one-step Q-learning update.
func (q *QTable) Update(s observation.State, a policy.Action, reward float32, next observation.State, done bool) {
	v := q.valuesFor(StateKey(s))
	i := actionIndex(a)
	target := reward
	if !done {
		n := q.valuesFor(StateKey(next))
		max := n[0]
		for _, x := range n[1:] {
			if x > max {
				max = x
			}
		}
		target += q.gamma * max
	}
	v[i] += q.alpha * (target - v[i])
}

func (q *QTable) States() int { return len(q.values) }
