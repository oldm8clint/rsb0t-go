package controlpanel

import (
	"fmt"
	"log"
	"net/http"
	"strconv"
)

// maxHeatResetBytes bounds a POST /api/heatmap/reset body ({"account": "..."}).
const maxHeatResetBytes = 1 << 10

// handleHeatmap answers GET /api/heatmap: the travel heat map's counts for one account (or
// all), one floor (or all), one time window, aggregated to a grid of cell tiles and
// optionally clipped to a tile box. Query parameters, all optional:
//
//	account  an account name; empty means every account
//	level    0-3, or "all" (default) to sum every floor
//	window   "hour" (last ~60 minutes), "day" (last 24 hours) or "all" (default)
//	cell     output cell size in tiles, a power of two from 1 to 1024 (default 1); the
//	         answer may use a coarser grid (see Cell) when the store is coarser or the answer
//	         would be too large
//	x0,z0,x1,z1  an inclusive tile box; all four or none
func (s *Server) handleHeatmap(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.heat == nil {
		writeError(w, http.StatusNotImplemented, "this fleet has no travel heat map (switched off, or its source cannot report positions)")
		return
	}
	q, msg := parseHeatQuery(r)
	if msg != "" {
		writeError(w, http.StatusBadRequest, msg)
		return
	}
	writeJSON(w, http.StatusOK, s.heat.query(q))
}

func parseHeatQuery(r *http.Request) (heatQuery, string) {
	v := r.URL.Query()
	q := heatQuery{account: v.Get("account"), level: -1, window: "all", cell: 1}
	if len(q.account) > maxNameBytes {
		return q, fmt.Sprintf("account must be at most %d bytes", maxNameBytes)
	}
	if l := v.Get("level"); l != "" && l != "all" {
		n, err := strconv.Atoi(l)
		if err != nil || n < 0 || n > heatMaxLevel {
			return q, fmt.Sprintf("level must be 0 to %d or all", heatMaxLevel)
		}
		q.level = n
	}
	switch w := v.Get("window"); w {
	case "":
	case "hour", "day", "all":
		q.window = w
	default:
		return q, "window must be hour, day or all"
	}
	if c := v.Get("cell"); c != "" {
		n, err := strconv.Atoi(c)
		if err != nil || n < 1 || n > 1024 || n&(n-1) != 0 {
			return q, "cell must be a power of two from 1 to 1024"
		}
		q.cell = n
	}
	box := [4]string{v.Get("x0"), v.Get("z0"), v.Get("x1"), v.Get("z1")}
	given := 0
	for _, b := range box {
		if b != "" {
			given++
		}
	}
	switch given {
	case 0:
	case 4:
		var n [4]int
		for i, b := range box {
			var err error
			if n[i], err = strconv.Atoi(b); err != nil {
				return q, "x0, z0, x1 and z1 must be whole numbers"
			}
		}
		if n[0] > n[2] || n[1] > n[3] {
			return q, "the box needs x0 <= x1 and z0 <= z1"
		}
		q.bbox, q.x0, q.z0, q.x1, q.z1 = true, n[0], n[1], n[2], n[3]
	default:
		return q, "give all of x0, z0, x1 and z1, or none"
	}
	return q, ""
}

type heatResetRequest struct {
	Account string `json:"account"`
}

type heatResetBody struct {
	Cleared bool   `json:"cleared"`
	Account string `json:"account"`
}

// handleHeatmapReset answers POST /api/heatmap/reset: clear one account's heat map, or
// every account's when account is empty, in memory and in the saved file. Protected like
// every other panel POST: the guard refuses a cross-origin request and checks the access
// token, and readJSONBody insists on application/json (which a plain HTML form cannot send).
func (s *Server) handleHeatmapReset(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	if s.heat == nil {
		writeError(w, http.StatusNotImplemented, "this fleet has no travel heat map")
		return
	}
	var req heatResetRequest
	if !readJSONBody(w, r, maxHeatResetBytes, &req) {
		return
	}
	if len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, fmt.Sprintf("account must be at most %d bytes", maxNameBytes))
		return
	}
	target := "every account"
	if req.Account != "" {
		target = "account " + strconv.Quote(req.Account)
	}
	cleared, err := s.heat.reset(req.Account)
	if !cleared {
		writeError(w, http.StatusNotFound, "the heat map has nothing recorded for that account")
		return
	}
	if err != nil {
		log.Printf("controlpanel: heat map reset for %s from %s, but saving it failed: %v", target, r.RemoteAddr, err)
		writeError(w, http.StatusInternalServerError, "cleared in memory, but the saved file could not be rewritten: "+err.Error())
		return
	}
	log.Printf("controlpanel: heat map reset for %s from %s", target, r.RemoteAddr)
	writeJSON(w, http.StatusOK, heatResetBody{Cleared: true, Account: req.Account})
}
