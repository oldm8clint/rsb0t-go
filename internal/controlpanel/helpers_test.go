package controlpanel

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func testOptions() options {
	o := defaultOptions()
	o.tick = 20 * time.Millisecond
	o.writeTimeout = 2 * time.Second
	o.keepAlive = time.Hour
	o.shutdownGrace = time.Second
	return o
}

// staticSource reports a fixed fleet and counts how often it is sampled.
type staticSource struct {
	accounts []AccountStatus
	logins   LoginStatus
	mem      MemStats
	sampled  int64

	// The rest back botControlSource below, which wraps a staticSource to add BotControl
	// only where a test opts in — a plain staticSource must stay a Source without it.
	scripts  []string
	startErr error
	stopErr  error
	starts   []botCall
	stops    []string

	// The rest backs connectionControlSource below, the same wrap-to-opt-in pattern
	// botControlSource uses for BotControl.
	disconnectErr error
	connectErr    error
	addErr        error
	disconnects   []string
	connects      []string
	adds          []addCall
}

type addCall struct {
	Account  string
	Password string
	Script   string
	Settings map[string]interface{}
}

type botCall struct {
	Account  string
	Script   string
	Settings map[string]interface{}
}

func (s *staticSource) Accounts(dst []AccountStatus) []AccountStatus {
	atomic.AddInt64(&s.sampled, 1)
	return append(dst, s.accounts...)
}
func (s *staticSource) Logins() LoginStatus           { return s.logins }
func (s *staticSource) MemStats() MemStats            { return s.mem }
func (s *staticSource) Subscribe(chan<- Event) func() { return func() {} }
func (s *staticSource) samples() int64                { return atomic.LoadInt64(&s.sampled) }

// botControlSource wraps a staticSource so it implements BotControl only when opted in — a
// plain staticSource must stay a Source without BotControl for the tests that check 501.
type botControlSource struct{ *staticSource }

func (s botControlSource) BotScripts() []string { return s.scripts }
func (s botControlSource) StartBot(account, script string, settings map[string]interface{}) error {
	if s.startErr != nil {
		return s.startErr
	}
	s.starts = append(s.starts, botCall{account, script, settings})
	return nil
}
func (s botControlSource) StopBot(account string) error {
	if s.stopErr != nil {
		return s.stopErr
	}
	s.stops = append(s.stops, account)
	return nil
}

// connectionControlSource wraps a staticSource so it implements ConnectionControl only
// when opted in — a plain staticSource must stay without it for the tests that check 501.
type connectionControlSource struct{ *staticSource }

func (s connectionControlSource) Disconnect(account string) error {
	if s.disconnectErr != nil {
		return s.disconnectErr
	}
	s.disconnects = append(s.disconnects, account)
	return nil
}
func (s connectionControlSource) ConnectExisting(account string) error {
	if s.connectErr != nil {
		return s.connectErr
	}
	s.connects = append(s.connects, account)
	return nil
}
func (s connectionControlSource) AddAccount(account, password, script string, settings map[string]interface{}) error {
	if s.addErr != nil {
		return s.addErr
	}
	s.adds = append(s.adds, addCall{account, password, script, settings})
	return nil
}

type harness struct {
	t   *testing.T
	srv *Server
	ts  *httptest.Server
}

// newHarness serves the dashboard from an httptest server. A nil src is a six-account FakeSource.
func newHarness(t *testing.T, cfg Config, opts options, src Source) *harness {
	t.Helper()
	if src == nil {
		src = NewFakeSource(6, 1)
	}
	cfg.Enabled = true
	if cfg.Listen == "" {
		cfg.Listen = "127.0.0.1:0"
	}
	srv, err := newServer(cfg, src, opts)
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewUnstartedServer(srv.handler())
	ts.Config.ConnContext = rememberConn
	ts.Start()
	srv.hub.start()
	t.Cleanup(ts.Close)
	t.Cleanup(srv.hub.close)
	return &harness{t: t, srv: srv, ts: ts}
}

// request never follows redirects. A "Host" entry in header sets the request's Host.
func (h *harness) request(method, path string, body io.Reader, header http.Header) *http.Response {
	h.t.Helper()
	req, err := http.NewRequest(method, h.ts.URL+path, body)
	if err != nil {
		h.t.Fatal(err)
	}
	for k, v := range header {
		if k == "Host" {
			req.Host = v[0]
			continue
		}
		req.Header[k] = v
	}
	client := &http.Client{
		Transport:     h.ts.Client().Transport,
		CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
	}
	res, err := client.Do(req)
	if err != nil {
		h.t.Fatal(err)
	}
	h.t.Cleanup(func() { res.Body.Close() })
	return res
}

func (h *harness) get(path string) *http.Response {
	return h.request(http.MethodGet, path, nil, nil)
}

func decode(t *testing.T, res *http.Response, v interface{}) {
	t.Helper()
	if err := json.NewDecoder(res.Body).Decode(v); err != nil {
		t.Fatalf("decoding %s: %v", res.Request.URL.Path, err)
	}
}

func bodyText(t *testing.T, res *http.Response) string {
	t.Helper()
	data, err := io.ReadAll(res.Body)
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func heapAfterGC() uint64 {
	runtime.GC()
	runtime.GC()
	var m runtime.MemStats
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

type sseMessage struct{ id, event, data string }

type sseReader struct {
	t  *testing.T
	br *bufio.Reader
}

func (h *harness) openStream(header http.Header) (*sseReader, *http.Response, func()) {
	h.t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, h.ts.URL+"/api/stream", nil)
	if err != nil {
		cancel()
		h.t.Fatal(err)
	}
	for k, v := range header {
		req.Header[k] = v
	}
	res, err := h.ts.Client().Do(req)
	if err != nil {
		cancel()
		h.t.Fatal(err)
	}
	stop := func() {
		cancel()
		res.Body.Close()
	}
	h.t.Cleanup(stop)
	return &sseReader{t: h.t, br: bufio.NewReader(res.Body)}, res, stop
}

// next reads one message, skipping comments and field-only frames such as retry.
func (r *sseReader) next() sseMessage {
	r.t.Helper()
	var m sseMessage
	var data []string
	for {
		line, err := r.br.ReadString('\n')
		if err != nil {
			r.t.Fatalf("stream ended: %v", err)
		}
		line = strings.TrimRight(line, "\r\n")
		if line == "" {
			if m.event == "" && m.id == "" && data == nil {
				continue
			}
			m.data = strings.Join(data, "\n")
			return m
		}
		if strings.HasPrefix(line, ":") {
			continue
		}
		field, value, _ := strings.Cut(line, ":")
		value = strings.TrimPrefix(value, " ")
		switch field {
		case "id":
			m.id = value
		case "event":
			m.event = value
		case "data":
			data = append(data, value)
		}
	}
}

func (r *sseReader) until(event string) sseMessage {
	r.t.Helper()
	for {
		if m := r.next(); m.event == event {
			return m
		}
	}
}
