package bot

import (
	"runtime/debug"
	"strings"
)

// BuildRevision is the binary's VCS revision (short, with "+dirty" when built from a modified
// tree), or "unknown": what a lab log must name so a proof can be tied to its exact tip.
func BuildRevision() string { return shortRevision(BuildIdentity()) }

// BuildIdentity is the binary's full VCS revision (40 hex digits, "+dirty" when built from a modified
// tree), or "unknown". It is what a quest completion records (questplan.Completion.Build): cmd/questpromote
// asks git whether that build contains a module's last change, and a short revision is only probably
// unambiguous.
func BuildIdentity() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "unknown"
	}
	rev, dirty := "unknown", false
	for _, s := range info.Settings {
		switch s.Key {
		case "vcs.revision":
			rev = s.Value
		case "vcs.modified":
			dirty = s.Value == "true"
		}
	}
	if dirty {
		rev += "+dirty"
	}
	return rev
}

// shortRevision is a BuildIdentity cut to its first 8 digits, "+dirty" kept.
func shortRevision(id string) string {
	rev := strings.TrimSuffix(id, "+dirty")
	if len(rev) > 8 {
		rev = rev[:8]
	}
	if strings.HasSuffix(id, "+dirty") {
		rev += "+dirty"
	}
	return rev
}
