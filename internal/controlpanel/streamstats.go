package controlpanel

import (
	"net/http"
	"sort"
)

// GET /api/stream/stats — what serving the stream client's observations actually costs.
//
// Why this exists. DefaultStreamMaxFPS is 5 and its own doc comment says not to raise it
// casually, because one observation is a whole gamestate.RenderSnapshot deep-copied out of
// a live session and encoded. The scene-delta transport (streamscene.go) cut what goes over
// the wire by an order of magnitude, and that made it tempting to read "frames are small
// now" as "the rate can go up" — which does not follow: a delta is computed FROM the same
// full observation, so the per-observation cost is unchanged by the transport. This
// endpoint exists so that question is answered with a number from the machine actually
// running the fleet, not from an argument.
//
// It is read-only, always available (no flag), and costs nothing when nobody is streaming:
// the ring is fixed-size, written under the lock the frame handler already takes, and this
// handler only reads it. Nothing here is per-account or per-session data — no tiles, names,
// chat or inventory, only durations, byte counts and the current view's account name, which
// the dashboard already shows.
//
// Honest limits, so nobody over-reads the output:
//   - the durations are WALL time, so they include any wait for the observed session's own
//     lock. That is the right number for "how many of these can the bridge serve a second"
//     and the wrong one for "how much CPU did the copy burn".
//   - they describe ONE account's session. The bridge only ever observes one at a time
//     (there is a single view lease), so this says nothing about what N concurrent viewers
//     would cost — there is no such thing on this build.
//   - the ring holds the last streamObsRing observations, so a long idle gap ages out of
//     the percentiles while the totals keep counting.
//   - the durations are QUANTISED by the platform clock. Measured on Windows against a
//     real local-engine session, every reading came back either 0 or a multiple of
//     roughly 500us, so a p50 of 0 means "under the clock's resolution", NOT "free",
//     and a p95 of ~850us is an upper bound carrying one quantum of slack.
//     ObservationsPerSecond is counted from timestamps across the whole window rather
//     than per observation and does not have that problem: prefer it when arguing
//     about the rate.
func (s *Server) handleStreamStats(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	s.stream.mu.Lock()
	n := s.stream.obsN
	held := int(n)
	if held > streamObsRing {
		held = streamObsRing
	}
	recent := make([]streamObservation, 0, held)
	for i := 0; i < held; i++ {
		recent = append(recent, s.stream.obs[(n-int64(held)+int64(i))%streamObsRing])
	}
	throttled, waiting := s.stream.throttled, s.stream.waiting
	var view *streamStatsView
	if v := s.stream.view; v != nil {
		view = &streamStatsView{Account: v.account, Transport: v.transport, FPS: v.fps, Seq: v.seq}
	}
	s.stream.mu.Unlock()

	out := streamStatsBody{
		MaxFPS:       s.streamMaxFPS(),
		Observations: n,
		Throttled:    throttled,
		Waiting:      waiting,
		View:         view,
		Window:       len(recent),
	}
	if len(recent) > 0 {
		out.Span = streamStatsSpan{From: recent[0].At, To: recent[len(recent)-1].At}
		for _, o := range recent {
			switch o.Kind {
			case "snapshot":
				out.Kinds.Snapshot++
			case "scene":
				out.Kinds.Scene++
			case "delta":
				out.Kinds.Delta++
			}
			out.Bytes.total += int64(o.Bytes)
		}
		out.Snapshot = summariseStreamMicros(recent, func(o streamObservation) int64 { return o.SnapshotUs })
		out.Project = summariseStreamMicros(recent, func(o streamObservation) int64 { return o.ProjectUs })
		out.Encode = summariseStreamMicros(recent, func(o streamObservation) int64 { return o.EncodeUs })
		out.Total = summariseStreamMicros(recent, func(o streamObservation) int64 { return o.SnapshotUs + o.ProjectUs + o.EncodeUs })
		out.Bytes.Summary = summariseStreamMicros(recent, func(o streamObservation) int64 { return int64(o.Bytes) })
		out.Bytes.Mean = out.Bytes.total / int64(len(recent))
		if span := out.Span.To - out.Span.From; span > 0 {
			out.ObservationsPerSecond = round1(float64(len(recent)-1) * 1000 / float64(span))
			out.BytesPerSecond = out.Bytes.total * 1000 / span
		}
		// The rate one observation's own cost would allow if nothing else ran and the
		// server did not pace itself. Reported because it is the number the cap is
		// argued about with; it is a CEILING, never a recommendation.
		if out.Total.P95 > 0 {
			out.CostCeilingFPS = round1(1e6 / float64(out.Total.P95))
		}
	}
	writeJSON(w, http.StatusOK, out)
}

type streamStatsBody struct {
	// MaxFPS is the cap actually in force (DefaultStreamMaxFPS or -stream-max-fps).
	MaxFPS int `json:"maxFps"`
	// Observations, Throttled and Waiting are totals since this process started: real
	// observations served, replies refused by the server's own pacing, and replies that
	// found the account's scene mid-rebuild.
	Observations int64 `json:"observations"`
	Throttled    int64 `json:"throttled"`
	Waiting      int64 `json:"waiting"`
	// View is the lease currently held, nil when nobody is streaming.
	View *streamStatsView `json:"view,omitempty"`
	// Window is how many recent observations the percentiles below are over, and Span
	// when the first and last of them were taken.
	Window int             `json:"window"`
	Span   streamStatsSpan `json:"span"`
	Kinds  struct {
		Snapshot int `json:"snapshot"`
		Scene    int `json:"scene"`
		Delta    int `json:"delta"`
	} `json:"kinds"`
	// Snapshot/Project/Encode/Total are microseconds per observation over the window.
	Snapshot streamStatsSummary `json:"snapshotUs"`
	Project  streamStatsSummary `json:"projectUs"`
	Encode   streamStatsSummary `json:"encodeUs"`
	Total    streamStatsSummary `json:"totalUs"`
	Bytes    struct {
		Summary streamStatsSummary `json:"summary"`
		Mean    int64              `json:"mean"`
		total   int64
	} `json:"bytes"`
	ObservationsPerSecond float64 `json:"observationsPerSecond"`
	BytesPerSecond        int64   `json:"bytesPerSecond"`
	// CostCeilingFPS is 1s / the p95 observation cost: how often this machine could take
	// an observation of this account if it did nothing else. A ceiling, not a target.
	CostCeilingFPS float64 `json:"costCeilingFps"`
}

type streamStatsView struct {
	Account   string `json:"account"`
	Transport string `json:"transport"`
	FPS       int    `json:"fps"`
	Seq       int64  `json:"seq"`
}

type streamStatsSpan struct {
	From int64 `json:"from"`
	To   int64 `json:"to"`
}

type streamStatsSummary struct {
	P50 int64 `json:"p50"`
	P95 int64 `json:"p95"`
	Max int64 `json:"max"`
}

// summariseStreamMicros is nearest-rank p50/p95 over a copy of the window, so the ring
// itself is never reordered.
func summariseStreamMicros(obs []streamObservation, pick func(streamObservation) int64) streamStatsSummary {
	if len(obs) == 0 {
		return streamStatsSummary{}
	}
	v := make([]int64, len(obs))
	for i, o := range obs {
		v[i] = pick(o)
	}
	sort.Slice(v, func(i, j int) bool { return v[i] < v[j] })
	at := func(p float64) int64 {
		i := int(p * float64(len(v)))
		if i >= len(v) {
			i = len(v) - 1
		}
		return v[i]
	}
	return streamStatsSummary{P50: at(0.5), P95: at(0.95), Max: v[len(v)-1]}
}

func round1(f float64) float64 { return float64(int64(f*10+0.5)) / 10 }
