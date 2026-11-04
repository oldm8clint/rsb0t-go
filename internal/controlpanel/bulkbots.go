package controlpanel

import "net/http"

type bulkBotBody struct {
	OK      bool              `json:"ok"`
	Changed int               `json:"changed"`
	Skipped int               `json:"skipped"`
	Errors  map[string]string `json:"errors,omitempty"`
}

func (s *Server) handleBulkBots(w http.ResponseWriter, r *http.Request, start bool) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	bc, ok := s.src.(BotControl)
	if !ok {
		writeError(w, http.StatusNotImplemented, "script control is unavailable")
		return
	}
	body := bulkBotBody{OK: true, Errors: map[string]string{}}
	// Use the whole source, not the dashboard's paginated/truncated account list.
	for _, a := range s.src.Accounts(nil) {
		if a.State != StateIngame || a.Bot == "" {
			body.Skipped++
			continue
		}
		var err error
		if start {
			if a.BotState == "running" {
				body.Skipped++
				continue
			}
			err = bc.StartBot(a.Name, a.Bot, a.Settings)
		} else {
			if a.BotState != "running" && a.BotState != "stopping" {
				body.Skipped++
				continue
			}
			err = bc.StopBot(a.Name)
		}
		if err != nil {
			body.Errors[a.Name] = err.Error()
			body.OK = false
		} else {
			body.Changed++
		}
	}
	writeJSON(w, http.StatusOK, body)
}
