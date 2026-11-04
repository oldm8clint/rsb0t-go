package controlpanel

import (
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"rsb0t-go/internal/gamestate"
)

type RenderSource interface {
	RenderSnapshot(string) (gamestate.RenderSnapshot, error)
}

func (f *FleetSource) RenderSnapshot(name string) (gamestate.RenderSnapshot, error) {
	if f.manager == nil {
		return gamestate.RenderSnapshot{}, errors.New("account capture is unavailable")
	}
	return f.manager.RenderSnapshot(name)
}

func (s *Server) handleCapture(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		methodNotAllowed(w, "POST")
		return
	}
	src, ok := s.src.(RenderSource)
	if !ok {
		writeError(w, http.StatusNotImplemented, "this fleet cannot capture account scenes")
		return
	}
	var req struct {
		Account string `json:"account"`
	}
	if !readJSONBody(w, r, 1024, &req) {
		return
	}
	if req.Account == "" || len(req.Account) > maxNameBytes {
		writeError(w, http.StatusBadRequest, "account is required")
		return
	}
	view, err := src.RenderSnapshot(req.Account)
	if err != nil {
		writeError(w, http.StatusConflict, err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	writeJSON(w, http.StatusOK, struct {
		Account    string                   `json:"account"`
		CapturedAt int64                    `json:"capturedAt"`
		Snapshot   gamestate.RenderSnapshot `json:"snapshot"`
	}{req.Account, s.opts.now().UnixMilli(), view})
}

func (s *Server) handleCaptureRenderer(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.cfg.CaptureRenderer == "" {
		writeError(w, http.StatusNotImplemented, "build and configure the optional game renderer first")
		return
	}
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	http.ServeFile(w, r, s.cfg.CaptureRenderer)
}

func (s *Server) handleCaptureCache(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	if s.cfg.CaptureCacheDir == "" {
		writeError(w, http.StatusNotImplemented, "game graphics cache is not configured")
		return
	}
	parts := strings.Split(strings.TrimPrefix(r.URL.Path, "/capture/cache/"), "/")
	if len(parts) != 2 {
		http.NotFound(w, r)
		return
	}
	archive, e1 := strconv.Atoi(parts[0])
	file, e2 := strconv.Atoi(parts[1])
	if e1 != nil || e2 != nil || archive < 0 || archive > 4 || file < 0 || file > 65535 {
		http.NotFound(w, r)
		return
	}
	data, err := readCaptureCache(s.cfg.CaptureCacheDir, archive, file)
	if err != nil {
		http.NotFound(w, r)
		return
	}
	w.Header().Set("Content-Type", "application/octet-stream")
	w.Header().Set("Cache-Control", "private, max-age=3600")
	w.Write(data)
}

// Only fixed cache filenames and numeric entries are read. Every sector is validated
// before use; truncated/cyclic/cross-file chains cannot produce a partial asset.
func readCaptureCache(dir string, archive, file int) ([]byte, error) {
	if archive < 0 || archive > 4 || file < 0 || file > 65535 {
		return nil, errors.New("invalid cache entry")
	}
	idx, err := os.Open(filepath.Join(dir, fmt.Sprintf("main_file_cache.idx%d", archive)))
	if err != nil {
		return nil, err
	}
	defer idx.Close()
	var header [6]byte
	if _, err = idx.ReadAt(header[:], int64(file)*6); err != nil {
		return nil, err
	}
	u24 := func(b []byte) int { return int(b[0])<<16 | int(b[1])<<8 | int(b[2]) }
	size, sector := u24(header[:3]), u24(header[3:])
	if size <= 0 || size > 2000000 || sector == 0 {
		return nil, errors.New("invalid cache header")
	}
	dat, err := os.Open(filepath.Join(dir, "main_file_cache.dat"))
	if err != nil {
		return nil, err
	}
	defer dat.Close()
	out := make([]byte, size)
	for pos, part := 0, 0; pos < size; part++ {
		if sector == 0 {
			return nil, io.ErrUnexpectedEOF
		}
		count := min(512, size-pos)
		buf := make([]byte, 8+count)
		if _, err = dat.ReadAt(buf, int64(sector)*520); err != nil {
			return nil, err
		}
		if int(binary.BigEndian.Uint16(buf[:2])) != file || int(binary.BigEndian.Uint16(buf[2:4])) != part || int(buf[7]) != archive+1 {
			return nil, errors.New("invalid cache sector")
		}
		copy(out[pos:], buf[8:])
		pos += count
		sector = u24(buf[4:7])
		if pos == size && sector != 0 {
			return nil, errors.New("cache chain exceeds entry")
		}
	}
	return out, nil
}

// min shadows the Go 1.21 builtin of the same name, which this repo cannot use: CLAUDE.md
// pins the build to Go 1.19.3, where it does not exist.
func min(a, b int) int {
	if a < b {
		return a
	}
	return b
}
