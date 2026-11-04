package controlpanel

import (
	"net/http"
	"strconv"
	"strings"

	"rsb0t-go/internal/muleledger"
)

// DefaultMuleLedgerFile is cmd/controlpanel's default -mule-ledger file, and DefaultStatsFile its
// default -stats-state file (which also carries the Loot panel's lifetime totals), both relative
// to the working directory like DefaultHeatmapFile, so a fleet started without either flag still
// keeps its trade record and lifetime loot across a restart.
const (
	DefaultMuleLedgerFile = "mule-ledger.jsonl"
	DefaultStatsFile      = "stats-state.json"
)

// MuleLedgerSource is optional: a Source that keeps the fleet's persisted mule trade record
// (internal/muleledger) answers GET /api/mule-ledger. internal/fleet via FleetSource and the fake
// fleet both do; any other Source answers 501.
type MuleLedgerSource interface {
	MuleTrades(q muleledger.Query) []muleledger.Entry
}

// MuleTrades answers MuleLedgerSource from the fleet manager's attached ledger.
func (f *FleetSource) MuleTrades(q muleledger.Query) []muleledger.Entry {
	if f.manager == nil {
		return nil
	}
	return f.manager.MuleLedger().Entries(q)
}

// MuleLedgerBody answers GET /api/mule-ledger.
type MuleLedgerBody struct {
	NowUnixMs int64              `json:"now"`
	Entries   []muleledger.Entry `json:"entries"`
}

// handleMuleLedger answers GET /api/mule-ledger?since=<unix ms>&mule=<name>&limit=<n>: the
// newest matching finished trades, oldest first (limit defaults to muleledger.DefaultLimit and
// is capped at muleledger.MaxLimit).
func (s *Server) handleMuleLedger(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	src, ok := s.src.(MuleLedgerSource)
	if !ok {
		writeJSON(w, http.StatusNotImplemented, map[string]string{"error": "this source keeps no mule trade record"})
		return
	}
	q := muleledger.Query{Mule: strings.TrimSpace(r.URL.Query().Get("mule"))}
	if v := r.URL.Query().Get("since"); v != "" {
		n, err := strconv.ParseInt(v, 10, 64)
		if err != nil || n < 0 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "since must be a non-negative Unix millisecond time"})
			return
		}
		q.SinceMs = n
	}
	if v := r.URL.Query().Get("limit"); v != "" {
		n, err := strconv.Atoi(v)
		if err != nil || n < 1 {
			writeJSON(w, http.StatusBadRequest, map[string]string{"error": "limit must be a positive integer"})
			return
		}
		q.Limit = n
	}
	entries := src.MuleTrades(q)
	if entries == nil {
		entries = []muleledger.Entry{}
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, MuleLedgerBody{NowUnixMs: s.opts.now().UnixMilli(), Entries: entries})
}
