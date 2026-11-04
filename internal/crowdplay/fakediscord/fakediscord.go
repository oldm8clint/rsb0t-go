// Package fakediscord is a local stand-in for Discord, for tests and the CrowdPlay lab: a
// loopback HTTP server with the two REST routes the bot uses (GET /api/v10/gateway/bot, POST
// /api/v10/channels/{id}/messages) and a Gateway WebSocket (/gateway) that speaks the v10
// JSON handshake (HELLO, IDENTIFY, heartbeats, READY) and lets the test push MESSAGE_CREATE
// events. It checks the bot presents the token it was given. Nothing here talks to Discord.
package fakediscord

import (
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/gorilla/websocket"
)

// Reply is one message the bot posted.
type Reply struct {
	Channel string
	Content string
	// Mentions is false when the bot disabled mentions (allowed_mentions.parse empty).
	Mentions bool
}

// Server is a running fake Discord.
type Server struct {
	Token string // the token the bot must present
	URL   string // http://127.0.0.1:port

	ln        net.Listener
	srv       *http.Server
	mu        sync.Mutex
	conns     []*fconn
	replies   []Reply
	identify  []map[string]interface{}
	badAuth   int
	heartbeat atomic.Int64
	seq       atomic.Int64
	ready     chan struct{}
	readyOnce sync.Once
}

type fconn struct {
	c  *websocket.Conn
	mu sync.Mutex
}

func (f *fconn) write(v interface{}) error { f.mu.Lock(); defer f.mu.Unlock(); return f.c.WriteJSON(v) }

// Start listens on a loopback port.
func Start(token string) (*Server, error) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	s := &Server{Token: token, URL: "http://" + ln.Addr().String(), ln: ln, ready: make(chan struct{})}
	mux := http.NewServeMux()
	mux.HandleFunc("/api/v10/gateway/bot", s.gatewayBot)
	mux.HandleFunc("/api/v10/channels/", s.messages)
	mux.HandleFunc("/gateway", s.gateway)
	s.srv = &http.Server{Handler: mux, ReadHeaderTimeout: 5 * time.Second}
	go func() { _ = s.srv.Serve(ln) }()
	return s, nil
}

// APIBase is the REST base the bot should be given.
func (s *Server) APIBase() string { return s.URL + "/api/v10" }

// Close stops the server.
func (s *Server) Close() {
	s.mu.Lock()
	for _, c := range s.conns {
		_ = c.c.Close()
	}
	s.mu.Unlock()
	_ = s.srv.Close()
}

func (s *Server) authorized(r *http.Request) bool {
	if r.Header.Get("Authorization") == "Bot "+s.Token {
		return true
	}
	s.mu.Lock()
	s.badAuth++
	s.mu.Unlock()
	return false
}

func (s *Server) gatewayBot(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, `{"message":"401: Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(map[string]interface{}{"url": "ws" + strings.TrimPrefix(s.URL, "http") + "/gateway", "shards": 1})
}

func (s *Server) messages(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost || !strings.HasSuffix(r.URL.Path, "/messages") {
		http.NotFound(w, r)
		return
	}
	if !s.authorized(r) {
		http.Error(w, `{"message":"401: Unauthorized"}`, http.StatusUnauthorized)
		return
	}
	var body struct {
		Content         string `json:"content"`
		AllowedMentions *struct {
			Parse []string `json:"parse"`
		} `json:"allowed_mentions"`
	}
	_ = json.NewDecoder(r.Body).Decode(&body)
	channel := strings.TrimSuffix(strings.TrimPrefix(r.URL.Path, "/api/v10/channels/"), "/messages")
	s.mu.Lock()
	s.replies = append(s.replies, Reply{Channel: channel, Content: body.Content, Mentions: body.AllowedMentions == nil || len(body.AllowedMentions.Parse) > 0})
	s.mu.Unlock()
	w.Header().Set("Content-Type", "application/json")
	_, _ = w.Write([]byte(`{"id":"1"}`))
}

var upgrader = websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}

func (s *Server) gateway(w http.ResponseWriter, r *http.Request) {
	c, err := upgrader.Upgrade(w, r, nil)
	if err != nil {
		return
	}
	fc := &fconn{c: c}
	s.mu.Lock()
	s.conns = append(s.conns, fc)
	s.mu.Unlock()
	defer func() {
		s.mu.Lock()
		for i, x := range s.conns {
			if x == fc {
				s.conns = append(s.conns[:i], s.conns[i+1:]...)
				break
			}
		}
		s.mu.Unlock()
		_ = c.Close()
	}()
	write := fc.write
	_ = write(map[string]interface{}{"op": 10, "d": map[string]interface{}{"heartbeat_interval": 1000}})
	for {
		var p struct {
			Op int             `json:"op"`
			D  json.RawMessage `json:"d"`
		}
		if err := c.ReadJSON(&p); err != nil {
			return
		}
		switch p.Op {
		case 1:
			s.heartbeat.Add(1)
			_ = write(map[string]interface{}{"op": 11})
		case 2:
			var id map[string]interface{}
			_ = json.Unmarshal(p.D, &id)
			if id["token"] != s.Token {
				fc.mu.Lock()
				_ = c.WriteMessage(websocket.CloseMessage, websocket.FormatCloseMessage(4004, "Authentication failed."))
				fc.mu.Unlock()
				return
			}
			s.mu.Lock()
			delete(id, "token")
			s.identify = append(s.identify, id)
			s.mu.Unlock()
			_ = write(map[string]interface{}{"op": 0, "t": "READY", "s": s.seq.Add(1), "d": map[string]interface{}{
				"v": 10, "user": map[string]interface{}{"id": "900", "username": "crowd play decider", "bot": true}, "session_id": "fake"}})
			s.readyOnce.Do(func() { close(s.ready) })
		}
	}
}

// WaitReady waits for a bot to have identified.
func (s *Server) WaitReady(timeout time.Duration) bool {
	select {
	case <-s.ready:
		return true
	case <-time.After(timeout):
		return false
	}
}

// Message is a chat line the fake sends as MESSAGE_CREATE.
type Message struct {
	ID, Channel, AuthorID, Author, Content string
	Bot                                    bool
}

// Send pushes a MESSAGE_CREATE to every connected gateway client.
func (s *Server) Send(m Message) error {
	payload := map[string]interface{}{"op": 0, "t": "MESSAGE_CREATE", "s": s.seq.Add(1), "d": map[string]interface{}{
		"id": m.ID, "channel_id": m.Channel, "content": m.Content,
		"author": map[string]interface{}{"id": m.AuthorID, "username": m.Author, "bot": m.Bot}}}
	s.mu.Lock()
	conns := append([]*fconn(nil), s.conns...)
	s.mu.Unlock()
	if len(conns) == 0 {
		return fmt.Errorf("fakediscord: no gateway client connected")
	}
	for _, c := range conns {
		if err := c.write(payload); err != nil {
			return err
		}
	}
	return nil
}

// Replies returns what the bot posted so far.
func (s *Server) Replies() []Reply {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Reply(nil), s.replies...)
}

// Identified returns each IDENTIFY's payload (token removed).
func (s *Server) Identified() []map[string]interface{} {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]map[string]interface{}(nil), s.identify...)
}

// BadAuth counts REST requests with the wrong Authorization header.
func (s *Server) BadAuth() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.badAuth
}

// Heartbeats counts heartbeats received.
func (s *Server) Heartbeats() int64 { return s.heartbeat.Load() }
