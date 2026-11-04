// Command controlpanel connects the fleet through internal/fleet and serves the panel-based
// control panel (internal/controlpanel) on its own port, apart from cmd/fleet's dashboard.
//
//	go run ./cmd/controlpanel -fake 8                     a simulated fleet: no game connection at all
//	go run ./cmd/controlpanel -config fleet.config.json   the real roster, in cmd/fleet's config format
//
// Then open http://127.0.0.1:7274/. Ctrl+C stops the server and logs every account out. The
// program itself is internal/controlpanelapp, which cmd/rsb0t shares.
package main

import "rsb0t-go/internal/controlpanelapp"

func main() { controlpanelapp.Run() }
