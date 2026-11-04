package controlpanelapp

import (
	"log"
	"net/http"
	// Registers /debug/pprof/* on http.DefaultServeMux as an import side effect. This
	// file is the only place that mux is ever listened on for this binary — see
	// startPprof below — so importing it here cannot expose anything unless -pprof is
	// explicitly passed.
	_ "net/http/pprof"
)

// startPprof serves net/http/pprof on addr in the background, for `go tool pprof
// http://<addr>/debug/pprof/profile?seconds=30` (CPU), /debug/pprof/heap, /debug/pprof/goroutine,
// etc. Diagnostic-only: only called when the operator passes -pprof explicitly (see main()),
// so the normal startup path (no flag) never binds this listener, imports nothing extra at
// runtime beyond the registration above, and costs nothing. Intended for a local/loopback
// address on a machine already running the fleet — this endpoint has no auth of its own, so
// do not bind it to a non-loopback address on a shared or internet-facing host.
//
// This has NOT been run against the live 179-account production process as of this change —
// it is new instrumentation, opt-in, proven safe in isolation (go build/vet/test clean, and
// exercised locally under cmd/controlpanel -fake), but turning it on against the live process
// is the account owner's own call to make, not something this change does automatically.
func startPprof(addr string) {
	go func() {
		log.Printf("controlpanel: pprof diagnostics listening on http://%s/debug/pprof/ (loopback only recommended; no auth)", addr)
		if err := http.ListenAndServe(addr, nil); err != nil {
			log.Printf("controlpanel: pprof listener on %s failed: %v", addr, err)
		}
	}()
}
