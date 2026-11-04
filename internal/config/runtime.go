package config

import (
	"os"
	"runtime"
	"runtime/debug"
)

// Applied reports what Runtime.Apply set, for the startup log line.
type Applied struct {
	MemoryLimit int64
	GCPercent   int
	MaxProcs    int
	// FromEnv lists the knobs left to GOMEMLIMIT, GOGC or GOMAXPROCS because the environment set them.
	FromEnv []string
}

// Apply sets the memory limit, GC percent and GOMAXPROCS. A knob whose Go environment variable is
// set is left alone, so an operator can override the file for one run. lookupEnv nil means
// os.LookupEnv. Call it once, before any account starts.
func (rt Runtime) Apply(lookupEnv func(string) (string, bool)) Applied {
	if lookupEnv == nil {
		lookupEnv = os.LookupEnv
	}
	var out Applied

	if _, set := lookupEnv("GOMEMLIMIT"); set {
		out.FromEnv = append(out.FromEnv, "GOMEMLIMIT")
		out.MemoryLimit = debug.SetMemoryLimit(-1)
	} else {
		debug.SetMemoryLimit(rt.MemoryLimitBytes)
		out.MemoryLimit = rt.MemoryLimitBytes
	}

	if _, set := lookupEnv("GOGC"); set {
		out.FromEnv = append(out.FromEnv, "GOGC")
		// Why: SetGCPercent is the only reader, so read by setting and restoring.
		out.GCPercent = debug.SetGCPercent(100)
		debug.SetGCPercent(out.GCPercent)
	} else {
		debug.SetGCPercent(rt.GCPercent)
		out.GCPercent = rt.GCPercent
	}

	_, set := lookupEnv("GOMAXPROCS")
	if set {
		out.FromEnv = append(out.FromEnv, "GOMAXPROCS")
	}
	if set || rt.MaxProcs == 0 {
		out.MaxProcs = runtime.GOMAXPROCS(0)
	} else {
		runtime.GOMAXPROCS(rt.MaxProcs)
		out.MaxProcs = rt.MaxProcs
	}
	return out
}
