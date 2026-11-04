// Package environment contains deterministic, side-effect-free environments used
// to evaluate learning policies without connecting them to a live game session.
package environment

import (
	"rsb0t-go/internal/ml/observation"
	"rsb0t-go/internal/ml/policy"
)

type FisherState struct {
	Tick      uint64
	Inventory uint8
	Capacity  uint8
	XP        int64
	AtFish    bool
	AtBank    bool
	Trips     uint32
}

type StepResult struct {
	Observation observation.State
	Reward      float32
	Done        bool
}

// FisherEnv is a tiny deterministic training/evaluation environment. It is not
// connected to the game client and deliberately models only script decisions.
type FisherEnv struct{ State FisherState }

func NewFisherEnv() *FisherEnv {
	return &FisherEnv{State: FisherState{Capacity: 28, AtFish: true}}
}

func (e *FisherEnv) Observe() observation.State {
	s := e.State
	return observation.State{SchemaVersion: 1, Tick: s.Tick, InventoryUsed: s.Inventory, InventorySize: s.Capacity, XPDelta: 0,
		DistanceToFish: boolDistance(!s.AtFish), DistanceToBank: boolDistance(!s.AtBank), NearbyFish: boolCount(s.AtFish), NearbyBank: boolCount(s.AtBank)}
}

func (e *FisherEnv) Step(a policy.Action) StepResult {
	e.State.Tick++
	var reward float32
	switch a {
	case policy.WalkToFish:
		e.State.AtFish, e.State.AtBank = true, false
		reward = -0.01
	case policy.Fish:
		if e.State.AtFish && e.State.Inventory < e.State.Capacity {
			e.State.Inventory++
			e.State.XP += 10
			reward = 1
		} else {
			reward = -1
		}
	case policy.WalkToBank:
		e.State.AtFish, e.State.AtBank = false, true
		reward = -0.01
	case policy.Bank:
		if e.State.AtBank {
			e.State.Inventory = 0
			e.State.Trips++
			reward = .5
		} else {
			reward = -1
		}
	case policy.Wait:
		reward = -.05
	case policy.Recover:
		reward = -.5
	}
	return StepResult{Observation: e.Observe(), Reward: reward}
}

func boolDistance(v bool) uint16 {
	if v {
		return 1
	}
	return 0
}
func boolCount(v bool) uint8 {
	if v {
		return 1
	}
	return 0
}
