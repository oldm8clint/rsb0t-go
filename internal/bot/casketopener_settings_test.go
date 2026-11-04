package bot

import "testing"

func TestCasketOpenerVerboseSetting(t *testing.T) {
	b := &casketOpenerBot{}
	b.verbose = StringSetting(map[string]interface{}{"logDetail": "Verbose"}, "logDetail", "Normal") == "Verbose"
	if !b.verbose {
		t.Error("logDetail Verbose should set verbose")
	}
	b2 := &casketOpenerBot{}
	b2.verbose = StringSetting(nil, "logDetail", "Normal") == "Verbose"
	if b2.verbose {
		t.Error("no settings should default to non-verbose")
	}
}

func TestCasketOpenerOnStartWithNoConfigLeavesIDsUnresolved(t *testing.T) {
	b := &casketOpenerBot{}
	ctx := NewContext(nil, "acct", nil, nil, nil, nil, nil, nil, nil, nil, nil)
	if err := b.OnStart(ctx); err != nil {
		t.Fatalf("OnStart with a nil Config should not error, got %v", err)
	}
	if b.idsOK {
		t.Error("idsOK should stay false without a Config to resolve \"Casket\" against")
	}
}
