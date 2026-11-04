// Package controlpanel is the fleet's panel-based control panel: a grid of panels the user adds,
// removes, arranges and sets up, fed live over Server-Sent Events. It is modelled on the Control
// page of rsb0t-scripts (pages/control.html) and runs apart from cmd/fleet's dashboard.
//
// It binds to 127.0.0.1 by default and costs one idle goroutine and a few fixed rings while no
// browser is open. Everything it keeps is bounded by the constants in config.go; charting and
// longer history live in the browser. docs/controlpanel.md is the design: the panel model, layout
// persistence, the API contract, auth and the memory budget.
//
// A fleet plugs in through Source. FleetSource adapts internal/fleet; FakeSource simulates a fleet
// for development and tests.
//
//	srv, err := controlpanel.Start(ctx, controlpanel.Config{Enabled: true}, controlpanel.NewFleetSource(mgr, roster, delay))
//
// # Running it
//
//	go run ./cmd/controlpanel -fake 8                     a simulated fleet, no game connection
//	go run ./cmd/controlpanel -config fleet.config.json   the real roster
//
// then open http://127.0.0.1:7274/. The frontend is plain files under web/, embedded at build time,
// so after editing one, restart the command.
package controlpanel
