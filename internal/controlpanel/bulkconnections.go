package controlpanel

import "net/http"

type bulkConnectionBody struct {
	OK            bool              `json:"ok"`
	Changed       int               `json:"changed"`
	Skipped       int               `json:"skipped"`
	SkippedQueued int               `json:"skippedQueued"`
	Errors        map[string]string `json:"errors,omitempty"`
}

// These controls affect existing accounts, never the owning process or startup
// roster. Synthetic queued accounts have not reached the manager yet and cannot
// be cancelled or connected through ConnectionControl.
func (s *Server) handleBulkConnections(w http.ResponseWriter, r *http.Request, action string) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	cc, ok := s.src.(ConnectionControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "account connection control is unavailable")
		return
	}
	if !s.bulkConnectionMu.TryLock() {
		writeError(w, http.StatusConflict, "another bulk account connection action is in progress")
		return
	}
	defer s.bulkConnectionMu.Unlock()
	body := bulkConnectionBody{OK: true, Errors: map[string]string{}}
	for _, a := range s.src.Accounts(nil) {
		if a.State == StateQueued {
			body.Skipped++
			body.SkippedQueued++
			continue
		}
		eligible := false
		switch action {
		case "logout":
			eligible = a.State == StateIngame || a.State == StateConnecting || a.State == StateReconnecting
		case "login":
			eligible = a.State == StateOffline || a.State == StateStopped || a.State == StateError
		case "requeue":
			eligible = a.State == StateReconnecting || a.State == StateError
		}
		if !eligible {
			body.Skipped++
			continue
		}
		var err error
		if action == "logout" {
			err = cc.Disconnect(a.Name)
		} else {
			// ConnectExisting owns deduplication and the shared login gate;
			// requeue wakes an existing retry owner without creating another.
			err = cc.ConnectExisting(a.Name)
		}
		if err != nil {
			body.OK = false
			body.Errors[a.Name] = err.Error()
		} else {
			body.Changed++
		}
	}
	writeJSON(w, http.StatusOK, body)
}
