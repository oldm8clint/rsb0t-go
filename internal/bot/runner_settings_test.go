package bot

import "testing"

// A script that swaps ctx.Settings for a while (RuneRunner running ScarabSwarm while it waits)
// must not change what a world-hop restart reads: live 7 Oct 2026, acct35's hop restarted
// RuneRunner with ScarabSwarm's settings and the script refused them.
func TestRunnerSettingsAreTheStartSettings(t *testing.T) {
	ctx := &Context{Settings: map[string]interface{}{"mule": "mule"}}
	r := NewRunner("RuneRunner", nil, ctx)
	ctx.Settings = map[string]interface{}{"kqSpell": "Wind Bolt"}
	if got := r.Settings(); got["mule"] != "mule" || got["kqSpell"] != nil {
		t.Fatalf("Settings() = %v, want the start settings", got)
	}
}
