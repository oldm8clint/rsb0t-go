package fleetrunner

import (
	"os"
	"testing"
	"time"

	"rsb0t-go/internal/actions/actionsim"
	"rsb0t-go/internal/cachecfg"
)

// realCfg is the real 289 object config (RSB0T_CONFIG_JAG, e.g. fleet-data/prod-config.jag); the test
// is skipped without it, as every test that needs real item names is.
func realCfg(t *testing.T) *cachecfg.Config {
	t.Helper()
	path := os.Getenv("RSB0T_CONFIG_JAG")
	if path == "" {
		t.Skip("RSB0T_CONFIG_JAG not set")
	}
	cfg, err := cachecfg.LoadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func quickDeps(sim *actionsim.Session, cfg *cachecfg.Config) *Deps {
	return &Deps{Session: sim, Config: cfg, Components: actionsim.Components(), Log: func(string) {},
		Delay: func(d time.Duration) bool { time.Sleep(time.Millisecond); return true }}
}
