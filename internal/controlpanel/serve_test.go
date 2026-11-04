package controlpanel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestStartServesOverLoopback starts the real listener and fetches the page and an endpoint.
func TestStartServesOverLoopback(t *testing.T) {
	src := NewFakeSource(6, 1)
	for i := 0; i < 20; i++ {
		src.Step()
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	srv, err := Start(ctx, Config{Enabled: true, Listen: "127.0.0.1:0"}, src)
	if err != nil {
		t.Fatal(err)
	}
	base := "http://" + srv.Addr()

	res, err := http.Get(base + "/")
	if err != nil {
		t.Fatal(err)
	}
	page, _ := io.ReadAll(res.Body)
	res.Body.Close()
	if res.StatusCode != http.StatusOK || !strings.Contains(string(page), `<script type="module" src="app.js">`) {
		t.Fatalf("GET /: %d, %d bytes", res.StatusCode, len(page))
	}

	res, err = http.Get(base + "/api/status")
	if err != nil {
		t.Fatal(err)
	}
	var body statusBody
	err = json.NewDecoder(res.Body).Decode(&body)
	res.Body.Close()
	if err != nil || res.StatusCode != http.StatusOK || len(body.Accounts) != 6 {
		t.Fatalf("GET /api/status: %d, %d accounts, %v", res.StatusCode, len(body.Accounts), err)
	}
	t.Logf("served %s: page %d bytes, status %d accounts", base, len(page), len(body.Accounts))

	cancel()
	select {
	case <-srv.Done():
	case <-time.After(10 * time.Second):
		t.Fatal("cancelling the context did not stop the server")
	}
	http.DefaultClient.CloseIdleConnections()
	if _, err := http.Get(base + "/"); err == nil {
		t.Fatal("still serving after shutdown")
	}
}

func TestShutdownEndsOpenStreams(t *testing.T) {
	srv, err := start(context.Background(), Config{Enabled: true, Listen: "127.0.0.1:0"}, NewFakeSource(3, 1), testOptions())
	if err != nil {
		t.Fatal(err)
	}
	res, err := http.Get("http://" + srv.Addr() + "/api/stream")
	if err != nil {
		t.Fatal(err)
	}
	defer res.Body.Close()
	br := bufio.NewReader(res.Body)
	if _, err := br.ReadString('\n'); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	began := time.Now()
	if err := srv.Shutdown(ctx); err != nil {
		t.Fatalf("shutdown with a stream open: %v", err)
	}
	if took := time.Since(began); took > 2*time.Second {
		t.Errorf("shutdown took %s", took)
	}
	if _, err := io.ReadAll(br); err != nil && !strings.Contains(err.Error(), "EOF") {
		t.Logf("stream ended with %v", err)
	}
	if err := srv.Shutdown(ctx); err != nil {
		t.Errorf("second shutdown: %v", err)
	}
}

func TestStartDisabledIsNil(t *testing.T) {
	srv, err := Start(context.Background(), Config{}, nil)
	if srv != nil || err != nil {
		t.Fatalf("disabled start returned %v, %v", srv, err)
	}
	if srv.Addr() != "" || srv.Shutdown(context.Background()) != nil {
		t.Error("nil server methods are not safe")
	}
	select {
	case <-srv.Done():
	default:
		t.Error("a nil server's Done is not closed")
	}
}

func TestStartRejectsBadConfig(t *testing.T) {
	if _, err := Start(context.Background(), Config{Enabled: true, Listen: "127.0.0.1:0"}, nil); err == nil {
		t.Error("started with no source")
	}
	if _, err := Start(context.Background(), Config{Enabled: true, Listen: "0.0.0.0:0"}, NewFakeSource(1, 1)); err == nil {
		t.Error("started on every interface with no token")
	}
}
