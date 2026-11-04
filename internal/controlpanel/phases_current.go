package controlpanel

// currentPhasesJSON omits historical splits and timelines for panels that only
// display the latest recorded phase. It reads the same recorder as query.
type currentPhasesJSON struct {
	NowMs    int64              `json:"now"`
	Accounts []currentPhaseJSON `json:"accounts"`
}

type currentPhaseJSON struct {
	Name   string `json:"name"`
	Script string `json:"script"`
	Phase  string `json:"phase"`
	Since  int64  `json:"since"`
}

func (r *phaseRecorder) current() currentPhasesJSON {
	out := currentPhasesJSON{NowMs: r.now().UnixMilli()}
	names := r.rec.Accounts()
	out.Accounts = make([]currentPhaseJSON, 0, len(names))
	for _, name := range names {
		phase, since, _ := r.rec.Current(name)
		out.Accounts = append(out.Accounts, currentPhaseJSON{Name: name, Script: r.script(name), Phase: phase, Since: since.UnixMilli()})
	}
	return out
}
