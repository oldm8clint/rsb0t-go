// Package proxytest runs small loopback-only SOCKS5 and HTTP CONNECT proxies for tests and lab
// tools. They are deliberately minimal: CONNECT only, one optional username/password, and
// counters a test can assert on (how many tunnels were opened, how many are open now, how many
// handshakes were refused). They listen on 127.0.0.1 only and are never meant to be exposed.
package proxytest

import (
	"bufio"
	"encoding/base64"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"strconv"
	"sync"
	"sync/atomic"
	"time"
)

// Server is one running proxy.
type Server struct {
	ln       net.Listener
	kind     string
	username string
	password string

	// Target, when set, replaces every requested target (host:port) before dialling, so a
	// test can point a real client at a stub server.
	Target func(requested string) string

	// Delay, when set, is how long the proxy waits before answering a CONNECT from a client
	// that authenticated as username ("" without authentication) — a stand-in for a slow
	// circuit, per isolation identity the way Tor's IsolateSOCKSAuth builds one per
	// username/password pair.
	Delay func(username string) time.Duration

	// AcceptAny accepts every username/password pair rather than only the fixed one, as Tor
	// does (it uses the pair only to isolate circuits). Needs a non-empty username at start
	// so the proxy asks for authentication at all.
	AcceptAny bool

	// RefuseDomainTargets closes the connection on a CONNECT naming a host rather than an IP,
	// as Cloudflare WARP's local proxy was measured doing (0/10 remote-DNS CONNECTs).
	RefuseDomainTargets bool

	opened   atomic.Int64 // tunnels successfully opened
	active   atomic.Int64 // tunnels open right now
	refused  atomic.Int64 // handshakes refused (bad credentials, bad target)
	requests sync.Map     // requested target -> *atomic.Int64

	mu     sync.Mutex
	conns  map[net.Conn]struct{}
	closed bool
	wg     sync.WaitGroup
}

// NewSOCKS5 starts a SOCKS5 proxy on a random loopback port. A non-empty username requires
// RFC 1929 username/password authentication; an empty one accepts "no authentication" only.
func NewSOCKS5(username, password string) (*Server, error) {
	return start("socks5", username, password)
}

// NewHTTP starts an HTTP CONNECT proxy on a random loopback port, requiring Basic
// Proxy-Authorization when username is non-empty.
func NewHTTP(username, password string) (*Server, error) {
	return start("http", username, password)
}

func start(kind, username, password string) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{ln: ln, kind: kind, username: username, password: password, conns: make(map[net.Conn]struct{})}
	s.wg.Add(1)
	go s.serve()
	return s, nil
}

// Addr is the proxy's host:port.
func (s *Server) Addr() string { return s.ln.Addr().String() }

// Opened is how many tunnels have been opened since start.
func (s *Server) Opened() int64 { return s.opened.Load() }

// Active is how many tunnels are open right now.
func (s *Server) Active() int64 { return s.active.Load() }

// Refused is how many handshakes were refused.
func (s *Server) Refused() int64 { return s.refused.Load() }

// Requests is how many CONNECTs asked for target.
func (s *Server) Requests(target string) int64 {
	if v, ok := s.requests.Load(target); ok {
		return v.(*atomic.Int64).Load()
	}
	return 0
}

// DropAll closes every open client connection (and so every tunnel), leaving the listener up —
// a proxy that dropped its sessions.
func (s *Server) DropAll() {
	s.mu.Lock()
	for c := range s.conns {
		_ = c.Close()
	}
	s.mu.Unlock()
}

// Close stops the listener and every tunnel.
func (s *Server) Close() error {
	s.mu.Lock()
	s.closed = true
	s.mu.Unlock()
	err := s.ln.Close()
	s.DropAll()
	s.wg.Wait()
	return err
}

func (s *Server) track(c net.Conn) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.closed {
		return false
	}
	s.conns[c] = struct{}{}
	return true
}

func (s *Server) untrack(c net.Conn) {
	s.mu.Lock()
	delete(s.conns, c)
	s.mu.Unlock()
}

func (s *Server) serve() {
	defer s.wg.Done()
	for {
		c, err := s.ln.Accept()
		if err != nil {
			return
		}
		if !s.track(c) {
			_ = c.Close()
			return
		}
		s.wg.Add(1)
		go func() {
			defer s.wg.Done()
			defer s.untrack(c)
			defer c.Close()
			if s.kind == "socks5" {
				s.socks(c)
			} else {
				s.http(c)
			}
		}()
	}
}

func (s *Server) count(target string) {
	v, _ := s.requests.LoadOrStore(target, new(atomic.Int64))
	v.(*atomic.Int64).Add(1)
}

func (s *Server) dialTarget(target string) (net.Conn, error) {
	s.count(target)
	if s.Target != nil {
		target = s.Target(target)
	}
	return net.Dial("tcp", target)
}

func (s *Server) socks(c net.Conn) {
	var head [2]byte
	if _, err := io.ReadFull(c, head[:]); err != nil || head[0] != 5 {
		s.refused.Add(1)
		return
	}
	methods := make([]byte, head[1])
	if _, err := io.ReadFull(c, methods); err != nil {
		return
	}
	want := byte(0x00)
	if s.username != "" {
		want = 0x02
	}
	offered := false
	for _, m := range methods {
		offered = offered || m == want
	}
	if !offered {
		s.refused.Add(1)
		_, _ = c.Write([]byte{5, 0xFF})
		return
	}
	_, _ = c.Write([]byte{5, want})
	authed := ""
	if want == 0x02 {
		var ver [2]byte
		if _, err := io.ReadFull(c, ver[:]); err != nil {
			return
		}
		user := make([]byte, ver[1])
		if _, err := io.ReadFull(c, user); err != nil {
			return
		}
		var plen [1]byte
		if _, err := io.ReadFull(c, plen[:]); err != nil {
			return
		}
		pass := make([]byte, plen[0])
		if _, err := io.ReadFull(c, pass); err != nil {
			return
		}
		ok := ver[0] == 1 && (s.AcceptAny || string(user) == s.username && string(pass) == s.password)
		if !ok {
			s.refused.Add(1)
			_, _ = c.Write([]byte{1, 1})
			return
		}
		_, _ = c.Write([]byte{1, 0})
		authed = string(user)
	}
	var req [4]byte
	if _, err := io.ReadFull(c, req[:]); err != nil {
		return
	}
	var host string
	switch req[3] {
	case 1:
		b := make([]byte, 4)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 4:
		b := make([]byte, 16)
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = net.IP(b).String()
	case 3:
		var n [1]byte
		if _, err := io.ReadFull(c, n[:]); err != nil {
			return
		}
		b := make([]byte, n[0])
		if _, err := io.ReadFull(c, b); err != nil {
			return
		}
		host = string(b)
		if s.RefuseDomainTargets {
			s.refused.Add(1)
			return
		}
	default:
		_, _ = c.Write([]byte{5, 8, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	var pb [2]byte
	if _, err := io.ReadFull(c, pb[:]); err != nil {
		return
	}
	target := net.JoinHostPort(host, strconv.Itoa(int(binary.BigEndian.Uint16(pb[:]))))
	if req[1] != 1 {
		_, _ = c.Write([]byte{5, 7, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	if s.Delay != nil {
		if d := s.Delay(authed); d > 0 {
			time.Sleep(d)
		}
	}
	up, err := s.dialTarget(target)
	if err != nil {
		s.refused.Add(1)
		_, _ = c.Write([]byte{5, 5, 0, 1, 0, 0, 0, 0, 0, 0})
		return
	}
	defer up.Close()
	_, _ = c.Write([]byte{5, 0, 0, 1, 127, 0, 0, 1, 0, 0})
	s.pipe(c, up, nil)
}

func (s *Server) http(c net.Conn) {
	br := bufio.NewReader(c)
	req, err := http.ReadRequest(br)
	if err != nil || req.Method != http.MethodConnect {
		s.refused.Add(1)
		_, _ = io.WriteString(c, "HTTP/1.1 405 Method Not Allowed\r\n\r\n")
		return
	}
	if s.username != "" {
		want := "Basic " + base64.StdEncoding.EncodeToString([]byte(s.username+":"+s.password))
		if req.Header.Get("Proxy-Authorization") != want {
			s.refused.Add(1)
			_, _ = io.WriteString(c, "HTTP/1.1 407 Proxy Authentication Required\r\n\r\n")
			return
		}
	}
	up, err := s.dialTarget(req.Host)
	if err != nil {
		s.refused.Add(1)
		_, _ = io.WriteString(c, "HTTP/1.1 502 Bad Gateway\r\n\r\n")
		return
	}
	defer up.Close()
	_, _ = io.WriteString(c, "HTTP/1.1 200 Connection established\r\n\r\n")
	s.pipe(c, up, br)
}

func (s *Server) pipe(c, up net.Conn, buffered *bufio.Reader) {
	s.opened.Add(1)
	s.active.Add(1)
	defer s.active.Add(-1)
	var src io.Reader = c
	if buffered != nil {
		src = buffered
	}
	done := make(chan struct{}, 2)
	go func() { _, _ = io.Copy(up, src); _ = up.Close(); done <- struct{}{} }()
	go func() { _, _ = io.Copy(c, up); _ = c.Close(); done <- struct{}{} }()
	<-done
	_ = c.Close()
	_ = up.Close()
	<-done
}
