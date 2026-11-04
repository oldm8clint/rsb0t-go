package experiment

import (
	"encoding/json"
	"fmt"
	"os"
)

// Manifest describes a reproducible offline ML experiment.
type Manifest struct {
	Name     string  `json:"name"`
	Task     string  `json:"task"`
	Episodes int     `json:"episodes"`
	Steps    int     `json:"steps"`
	Seed     int64   `json:"seed"`
	Epsilon  float32 `json:"epsilon"`
	Alpha    float32 `json:"alpha"`
	Gamma    float32 `json:"gamma"`
}

func Default() Manifest {
	return Manifest{Name: "learning-fisher", Task: "fishing", Episodes: 100, Steps: 500, Seed: 1, Epsilon: .15, Alpha: .2, Gamma: .95}
}

func Load(path string) (Manifest, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Manifest{}, err
	}
	m := Default()
	if err := json.Unmarshal(data, &m); err != nil {
		return Manifest{}, fmt.Errorf("decode experiment manifest: %w", err)
	}
	if m.Name == "" || m.Task == "" {
		return Manifest{}, fmt.Errorf("experiment name and task are required")
	}
	if m.Episodes < 1 || m.Steps < 1 {
		return Manifest{}, fmt.Errorf("episodes and steps must be positive")
	}
	if m.Alpha <= 0 || m.Alpha > 1 || m.Gamma < 0 || m.Gamma > 1 || m.Epsilon < 0 || m.Epsilon > 1 {
		return Manifest{}, fmt.Errorf("alpha, gamma and epsilon are out of range")
	}
	return m, nil
}
