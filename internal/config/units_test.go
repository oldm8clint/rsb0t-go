package config

import (
	"encoding/json"
	"math"
	"reflect"
	"runtime"
	"runtime/debug"
	"testing"
	"time"
)

// Golden vectors from the vendored client's JString.toUserhash, printed with bun from rsb0t/.
func TestUserhashMatchesTheClient(t *testing.T) {
	golden := map[string]uint64{
		"alice":         2494434,
		"Bob_Smith":     8454104798272,
		"bob smith":     8454104798272,
		"zezima":        1813643468,
		"a1b2c3d4e5f6":  312920131694109864,
		"_lead":         614722,
		"trail_":        38396688,
		"ABCDEFGHIJKL":  187939216216112118,
		"abcdefghijklm": 187939216216112118,
		"9":             36,
	}
	for name, want := range golden {
		if got := userhash(name); got != want {
			t.Errorf("userhash(%q) = %d, want %d", name, got, want)
		}
	}
}

func TestExpandUser(t *testing.T) {
	cases := map[string]string{"alice": "PW_ALICE", "Bob_Smith": "PW_BOB_SMITH", "iron man 2": "PW_IRON_MAN_2"}
	for user, want := range cases {
		if got := expandUser("PW_{USER}", user); got != want {
			t.Errorf("expandUser(%q) = %q, want %q", user, got, want)
		}
	}
	if got := expandUser("SHARED_PW", "alice"); got != "SHARED_PW" {
		t.Errorf("a template without {USER} changed: %q", got)
	}
}

func TestParseByteSize(t *testing.T) {
	good := map[string]ByteSize{"45MiB": 45 * MiB, "512KiB": 512 * KiB, "1GiB": GiB, "123": 123, " 7 mib ": 7 * MiB, "9B": 9}
	for in, want := range good {
		if got, err := ParseByteSize(in); err != nil || got != want {
			t.Errorf("ParseByteSize(%q) = %d, %v; want %d", in, got, err, want)
		}
	}
	for _, in := range []string{"", "MiB", "5MB", "5TB", "-5MiB", "99999999999999999999GiB", "9999999999GiB"} {
		if _, err := ParseByteSize(in); err == nil {
			t.Errorf("ParseByteSize(%q) accepted", in)
		}
	}
	formats := map[ByteSize]string{45 * MiB: "45MiB", 1500: "1500B", 2 * GiB: "2GiB", 0: "0B", 3 * KiB: "3KiB"}
	for in, want := range formats {
		if got := in.String(); got != want {
			t.Errorf("%d.String() = %q, want %q", int64(in), got, want)
		}
	}
}

func TestDurationJSON(t *testing.T) {
	formats := map[time.Duration]string{5 * time.Minute: `"5m"`, 90 * time.Minute: `"1h30m"`, time.Hour: `"1h"`, 16 * time.Second: `"16s"`, 10 * time.Second: `"10s"`, 0: `"0s"`}
	for in, want := range formats {
		got, err := json.Marshal(Duration(in))
		if err != nil || string(got) != want {
			t.Errorf("Marshal(%v) = %s, %v; want %s", in, got, err, want)
		}
		var back Duration
		if err := json.Unmarshal(got, &back); err != nil || back.D() != in {
			t.Errorf("round trip of %v = %v, %v", in, back, err)
		}
	}
}

func TestApplySetsRuntimeKnobsUnlessTheEnvironmentDoes(t *testing.T) {
	prevLimit := debug.SetMemoryLimit(-1)
	prevGC := debug.SetGCPercent(100)
	prevProcs := runtime.GOMAXPROCS(0)
	defer func() {
		debug.SetMemoryLimit(prevLimit)
		debug.SetGCPercent(prevGC)
		runtime.GOMAXPROCS(prevProcs)
	}()
	gcPercent := func() int {
		p := debug.SetGCPercent(100)
		debug.SetGCPercent(p)
		return p
	}

	got := Runtime{MemoryLimitBytes: 45 << 20, GCPercent: 50, MaxProcs: 1}.Apply(envOf(nil))
	if limit := debug.SetMemoryLimit(-1); limit != 45<<20 {
		t.Errorf("memory limit = %d", limit)
	}
	if p := gcPercent(); p != 50 {
		t.Errorf("gc percent = %d", p)
	}
	if n := runtime.GOMAXPROCS(0); n != 1 {
		t.Errorf("GOMAXPROCS = %d", n)
	}
	if want := (Applied{MemoryLimit: 45 << 20, GCPercent: 50, MaxProcs: 1}); !reflect.DeepEqual(got, want) {
		t.Errorf("Applied = %+v, want %+v", got, want)
	}

	debug.SetMemoryLimit(math.MaxInt64)
	debug.SetGCPercent(100)
	got = Runtime{MemoryLimitBytes: 45 << 20, GCPercent: 50}.Apply(envOf(map[string]string{"GOMEMLIMIT": "1GiB", "GOGC": "100"}))
	if limit := debug.SetMemoryLimit(-1); limit != math.MaxInt64 {
		t.Errorf("GOMEMLIMIT in the environment was overridden: limit = %d", limit)
	}
	if p := gcPercent(); p != 100 {
		t.Errorf("GOGC in the environment was overridden: gc percent = %d", p)
	}
	if !reflect.DeepEqual(got.FromEnv, []string{"GOMEMLIMIT", "GOGC"}) {
		t.Errorf("FromEnv = %v", got.FromEnv)
	}
}
