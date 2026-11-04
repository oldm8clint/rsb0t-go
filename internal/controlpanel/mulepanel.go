package controlpanel

import (
	"net/http"
	"time"

	"rsb0t-go/internal/muleledger"
	"rsb0t-go/internal/mulestatus"
)

// The mule dashboard (GET /api/mule). Three sources, kept apart on purpose:
//
//   - live state from each MuleReceiver's own published status (internal/mulestatus.Tracker):
//     donors on the way, the trade in progress and its stage, recent demand;
//   - trade history from the fleet's persisted trade record (internal/muleledger) through
//     MuleLedgerSource (muleledger.go): completed and failed trades per hour, success rate,
//     declines by reason, items received and hand-outs, read by mulestatus.Summarize. The panel
//     keeps no trade history of its own;
//   - stock from the fleet's cached copy of the mule's last bank read (BankSource), and the time
//     split and current phase from the phase recorder.

// MuleSource is optional. A Source whose mules publish live status lets the control panel show
// the mule dashboard. The live fleet answers from fleet.Manager.MuleStatuses.
type MuleSource interface {
	MuleStatuses() []MuleStatus
}

// MuleStatus is mulestatus.Status.
type MuleStatus = mulestatus.Status

type muleJSON struct {
	NowMs int64        `json:"now"`
	Mules []MuleStatus `json:"mules"`
	// History says whether trade history is available at all (a MuleLedgerSource exists).
	History bool `json:"history"`
}

// muleHistorySpan is how far back the trade record is read: the dashboard's window is the last
// hour, and its failure and hand-out lists are the most recent few within this. At most
// muleledger.MaxLimit entries, the newest.
const muleHistorySpan = 24 * time.Hour

func (s *Server) muleStatuses() muleJSON {
	now := s.opts.now()
	out := muleJSON{NowMs: now.UnixMilli(), Mules: []MuleStatus{}}
	ms, ok := s.src.(MuleSource)
	if !ok {
		return out
	}
	bs, _ := s.src.(BankSource)
	ls, hasHistory := s.src.(MuleLedgerSource)
	out.History = hasHistory
	for _, st := range ms.MuleStatuses() {
		if hasHistory {
			entries := ls.MuleTrades(muleledger.Query{SinceMs: now.Add(-muleHistorySpan).UnixMilli(), Mule: st.Account, Limit: muleledger.MaxLimit})
			sum := mulestatus.Summarize(entries, now, st.StartedMs)
			st.History = &sum
		}
		if st.Stock == nil && bs != nil {
			if slots, updated, ok := bs.Bank(st.Account); ok && updated > 0 {
				names := make([]string, 0, len(slots))
				counts := make([]int64, 0, len(slots))
				for _, sl := range slots {
					names = append(names, sl.Name)
					counts = append(counts, int64(sl.Count))
				}
				stock := mulestatus.ClassifyStock(names, counts, st.Catalog, time.UnixMilli(updated))
				st.Stock = &stock
			}
		}
		if s.phases != nil {
			from := now.Add(-time.Hour)
			if started := time.UnixMilli(st.StartedMs); st.StartedMs > 0 && started.After(from) {
				from = started
			}
			if split, ok := s.phases.rec.Split(st.Account, from, now); ok {
				st.TimeSplit = split.Ms
			}
			if phase, _, ok := s.phases.rec.Current(st.Account); ok {
				st.Phase = phase
			}
		}
		out.Mules = append(out.Mules, st)
	}
	return out
}

// handleMule answers GET /api/mule: {now, history, mules: [...]}, one entry per MuleReceiver.
// 501 when the Source has no mule status at all; an empty list when it has but none is running.
func (s *Server) handleMule(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if _, ok := s.src.(MuleSource); !ok {
		writeError(w, http.StatusNotImplemented, "this fleet does not report mule status")
		return
	}
	writeJSON(w, http.StatusOK, s.muleStatuses())
}
