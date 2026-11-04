package controlpanel

import (
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"
)

var worldImages = struct {
	sync.Mutex
	data map[string][]byte
}{data: make(map[string][]byte)}

// Fixed-source image proxy lets WebGL upload the existing map art without relying
// on the upstream site's CORS headers. Downloads happen only on a browser request
// and successful assets are shared for this process's lifetime.
func (s *Server) handleMapImage(w http.ResponseWriter, r *http.Request) {
	if !getOnly(w, r) {
		return
	}
	area := r.URL.Query().Get("area")
	if area != "surface" && area != "underground" {
		http.NotFound(w, r)
		return
	}
	worldImages.Lock()
	data := worldImages.data[area]
	if data == nil {
		client := &http.Client{Timeout: 15 * time.Second}
		response, err := client.Get(fmt.Sprintf("https://example.invalid/img/map/world-overview-%s.webp", area))
		if err == nil {
			if response.StatusCode == http.StatusOK {
				data, err = io.ReadAll(io.LimitReader(response.Body, 8<<20+1))
				if len(data) > 8<<20 || len(data) < 12 || string(data[:4]) != "RIFF" || string(data[8:12]) != "WEBP" {
					data = nil
				}
			}
			response.Body.Close()
		}
		if err != nil {
			data = nil
		}
		if data != nil {
			worldImages.data[area] = data
		}
	}
	worldImages.Unlock()
	if data == nil {
		writeError(w, http.StatusBadGateway, "world map image unavailable")
		return
	}
	w.Header().Set("Content-Type", "image/webp")
	w.Header().Set("Cache-Control", "private, max-age=86400")
	w.Write(data)
}
