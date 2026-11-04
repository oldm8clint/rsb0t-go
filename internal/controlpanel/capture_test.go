package controlpanel

import (
	"bytes"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCaptureRendererRevalidatesAfterUpgrade(t *testing.T) {
	file := filepath.Join(t.TempDir(), "client.js")
	if err := os.WriteFile(file, []byte("export const version = 2;"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: Config{CaptureRenderer: file}}
	w := httptest.NewRecorder()
	s.handleCaptureRenderer(w, httptest.NewRequest("GET", "/capture/client.js", nil))
	if w.Code != 200 || w.Header().Get("Cache-Control") != "no-cache" {
		t.Fatalf("renderer must revalidate: status=%d cache=%q", w.Code, w.Header().Get("Cache-Control"))
	}
}

func TestCaptureCacheValidatesSectorChain(t *testing.T) {
	dir := t.TempDir()
	data := bytes.Repeat([]byte{42}, 600)
	idx := []byte{0, 2, 88, 0, 0, 1}
	dat := make([]byte, 1560)
	copy(dat[520:], []byte{0, 0, 0, 0, 0, 0, 2, 1})
	copy(dat[528:], data[:512])
	copy(dat[1040:], []byte{0, 0, 0, 1, 0, 0, 0, 1})
	copy(dat[1048:], data[512:])
	if err := os.WriteFile(filepath.Join(dir, "main_file_cache.idx0"), idx, 0600); err != nil {
		t.Fatal(err)
	}
	write := func() {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, "main_file_cache.dat"), dat, 0600); err != nil {
			t.Fatal(err)
		}
	}
	write()
	got, err := readCaptureCache(dir, 0, 0)
	if err != nil || !bytes.Equal(got, data) {
		t.Fatalf("valid chain: %v, length %d", err, len(got))
	}
	dat[1047] = 2
	write()
	if _, err := readCaptureCache(dir, 0, 0); err == nil {
		t.Fatal("cross-archive sector accepted")
	}
	dat[1047] = 1
	dat[1043] = 0
	write()
	if _, err := readCaptureCache(dir, 0, 0); err == nil {
		t.Fatal("repeated/incorrect part accepted")
	}
	if _, err := readCaptureCache(dir, -1, 0); err == nil {
		t.Fatal("invalid archive accepted")
	}
}
