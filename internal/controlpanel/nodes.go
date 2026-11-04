package controlpanel

import (
	"net/http"
	"strings"
)

// NodeStatus is the controller-facing health snapshot for one worker/controller node.
// It is intentionally optional so existing Source implementations do not need node telemetry.
type NodeStatus struct {
	ID            string  `json:"id"`
	State         string  `json:"state"`
	Sessions      int     `json:"sessions"`
	CPUPercent    float64 `json:"cpuPercent"`
	MemoryBytes   uint64  `json:"memoryBytes"`
	TickLatencyMs float64 `json:"tickLatencyMs"`
	ErrorRate     float64 `json:"errorRate"`
	Reconnects    int64   `json:"reconnects"`
	UpdatedUnixMs int64   `json:"updated"`
}

// NodeSource is an optional extension for fleets that expose distributed node telemetry.
// Implementations must return snapshots that are safe to retain after the call returns.
type NodeSource interface {
	Nodes(dst []NodeStatus) []NodeStatus
}

func (s *Server) handleNodes(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		methodNotAllowed(w, "GET, HEAD")
		return
	}
	ns, ok := s.src.(NodeSource)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "this fleet does not report node telemetry"})
		return
	}
	requested := strings.TrimPrefix(r.URL.Path, "/api/nodes")
	requested = strings.Trim(requested, "/")
	nodes := ns.Nodes(nil)
	if requested == "" {
		writeJSON(w, http.StatusOK, map[string]interface{}{"nodes": nodes})
		return
	}
	for _, node := range nodes {
		if node.ID == requested {
			writeJSON(w, http.StatusOK, node)
			return
		}
	}
	writeError(w, http.StatusNotFound, "node not found")
}
