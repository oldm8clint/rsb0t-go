package crowdplay

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/gorilla/websocket"

	"rsb0t-go/internal/twitchplays"
)

// A minimal Discord bot: the Gateway (v10, JSON) for MESSAGE_CREATE events from one channel,
// and the REST API for short replies in that channel. Only what CrowdPlay needs; no SDK.

// DefaultAPIBase is Discord's REST API. The lab points APIBase at a local stub instead.
const DefaultAPIBase = "https://discord.com/api/v10"

// Gateway intents: guild messages, and their content (a privileged intent the bot must have
// enabled in the Discord developer portal, or the gateway closes with 4014).
const (
	intentGuildMessages  = 1 << 9
	intentMessageContent = 1 << 15
)

// DiscordConfig configures the Discord side.
type DiscordConfig struct {
	Token     Token
	ChannelID string // the ONE channel commands are read from and replies go to
	APIBase   string // default DefaultAPIBase
	// GatewayURL overrides the gateway address (default: asked of GET {APIBase}/gateway/bot).
	GatewayURL string
	HTTP       *http.Client
	Log        func(string)
	// ReplyInterval is the least time between two replies; replies in between are dropped
	// (default 2s), so a busy channel can never make the bot spam it.
	ReplyInterval time.Duration
}

// Discord is the bot's connection. Run it with Run; Reply posts in the configured channel.
type Discord struct {
	cfg DiscordConfig

	mu        sync.Mutex
	lastReply time.Time
	dropped   int
	user      string // the bot's own user id, once READY
}

// NewDiscord checks the configuration. It connects nothing.
func NewDiscord(cfg DiscordConfig) (*Discord, error) {
	if cfg.Token.Empty() {
		return nil, errors.New("crowdplay: no Discord token")
	}
	if cfg.ChannelID == "" {
		return nil, errors.New("crowdplay: no Discord channel configured")
	}
	for _, r := range cfg.ChannelID {
		if r < '0' || r > '9' {
			return nil, errors.New("crowdplay: the Discord channel must be its numeric id")
		}
	}
	if cfg.APIBase == "" {
		cfg.APIBase = DefaultAPIBase
	}
	cfg.APIBase = strings.TrimRight(cfg.APIBase, "/")
	if cfg.HTTP == nil {
		cfg.HTTP = &http.Client{Timeout: 15 * time.Second}
	}
	if cfg.Log == nil {
		cfg.Log = func(string) {}
	}
	if cfg.ReplyInterval <= 0 {
		cfg.ReplyInterval = 2 * time.Second
	}
	return &Discord{cfg: cfg}, nil
}

type gatewayPayload struct {
	Op int             `json:"op"`
	D  json.RawMessage `json:"d,omitempty"`
	S  *int64          `json:"s,omitempty"`
	T  string          `json:"t,omitempty"`
}

type messageCreate struct {
	ID        string `json:"id"`
	ChannelID string `json:"channel_id"`
	Content   string `json:"content"`
	Author    struct {
		ID       string `json:"id"`
		Username string `json:"username"`
		Bot      bool   `json:"bot"`
	} `json:"author"`
	WebhookID string `json:"webhook_id"`
}

// errFatal ends Run: the token was refused or the bot lacks an intent. Retrying cannot help.
type errFatal struct{ msg string }

func (e errFatal) Error() string { return e.msg }

// Run connects to the gateway and calls onMessage for every message in the configured
// channel from a person (not a bot or webhook, and not this bot). It reconnects with backoff
// until ctx ends, or returns at once when Discord refuses the token or the intents.
func (d *Discord) Run(ctx context.Context, onMessage func(twitchplays.DiscordMessage)) error {
	backoff := time.Second
	for ctx.Err() == nil {
		started := time.Now()
		err := d.session(ctx, onMessage)
		var fatal errFatal
		if errors.As(err, &fatal) {
			return err
		}
		if ctx.Err() != nil {
			break
		}
		if time.Since(started) > time.Minute {
			backoff = time.Second
		}
		d.cfg.Log(fmt.Sprintf("crowdplay: Discord connection ended (%v); reconnecting in %s", err, backoff))
		select {
		case <-ctx.Done():
		case <-time.After(backoff):
		}
		if backoff *= 2; backoff > time.Minute {
			backoff = time.Minute
		}
	}
	return ctx.Err()
}

func (d *Discord) gatewayURL(ctx context.Context) (string, error) {
	u := d.cfg.GatewayURL
	if u == "" {
		var body struct {
			URL string `json:"url"`
		}
		if err := d.rest(ctx, http.MethodGet, "/gateway/bot", nil, &body); err != nil {
			return "", err
		}
		u = body.URL
	}
	parsed, err := url.Parse(u)
	if err != nil || (parsed.Scheme != "wss" && parsed.Scheme != "ws") {
		return "", fmt.Errorf("crowdplay: bad gateway address")
	}
	q := parsed.Query()
	q.Set("v", "10")
	q.Set("encoding", "json")
	parsed.RawQuery = q.Encode()
	return parsed.String(), nil
}

// session is one gateway connection: HELLO, IDENTIFY, heartbeats, dispatches.
func (d *Discord) session(ctx context.Context, onMessage func(twitchplays.DiscordMessage)) error {
	gw, err := d.gatewayURL(ctx)
	if err != nil {
		return err
	}
	dialer := websocket.Dialer{HandshakeTimeout: 15 * time.Second}
	conn, _, err := dialer.DialContext(ctx, gw, nil)
	if err != nil {
		return fmt.Errorf("gateway dial: %w", err)
	}
	defer conn.Close()
	conn.SetReadLimit(1 << 20)
	sctx, cancel := context.WithCancel(ctx)
	defer cancel()
	go func() { <-sctx.Done(); _ = conn.Close() }()

	var writeMu sync.Mutex
	send := func(p interface{}) error {
		writeMu.Lock()
		defer writeMu.Unlock()
		_ = conn.SetWriteDeadline(time.Now().Add(10 * time.Second))
		return conn.WriteJSON(p)
	}
	var seqMu sync.Mutex
	var seq *int64
	heartbeat := func() error {
		seqMu.Lock()
		s := seq
		seqMu.Unlock()
		return send(map[string]interface{}{"op": 1, "d": s})
	}

	// HELLO first.
	var hello gatewayPayload
	_ = conn.SetReadDeadline(time.Now().Add(30 * time.Second))
	if err := conn.ReadJSON(&hello); err != nil || hello.Op != 10 {
		return fmt.Errorf("gateway: no HELLO (%v)", err)
	}
	var hd struct {
		HeartbeatInterval int64 `json:"heartbeat_interval"`
	}
	if err := json.Unmarshal(hello.D, &hd); err != nil || hd.HeartbeatInterval <= 0 {
		return fmt.Errorf("gateway: bad HELLO")
	}
	interval := time.Duration(hd.HeartbeatInterval) * time.Millisecond
	identify := map[string]interface{}{"op": 2, "d": map[string]interface{}{
		"token":      d.cfg.Token.v,
		"intents":    intentGuildMessages | intentMessageContent,
		"properties": map[string]string{"os": "windows", "browser": "rsb0t-crowdplay", "device": "rsb0t-crowdplay"},
	}}
	if err := send(identify); err != nil {
		return fmt.Errorf("gateway identify: %w", err)
	}
	acked := make(chan struct{}, 1)
	go func() {
		t := time.NewTicker(interval)
		defer t.Stop()
		waiting := false
		for {
			select {
			case <-sctx.Done():
				return
			case <-acked:
				waiting = false
			case <-t.C:
				if waiting { // no ACK for a whole interval: a zombie connection
					cancel()
					return
				}
				if heartbeat() != nil {
					cancel()
					return
				}
				waiting = true
			}
		}
	}()

	for {
		_ = conn.SetReadDeadline(time.Now().Add(interval + 30*time.Second))
		var p gatewayPayload
		if err := conn.ReadJSON(&p); err != nil {
			var ce *websocket.CloseError
			if errors.As(err, &ce) {
				switch ce.Code {
				case 4004:
					return errFatal{"crowdplay: Discord refused the bot token (4004)"}
				case 4013, 4014:
					return errFatal{fmt.Sprintf("crowdplay: Discord refused the intents (%d): enable MESSAGE CONTENT for the bot in the developer portal", ce.Code)}
				case 4010, 4011, 4012:
					return errFatal{fmt.Sprintf("crowdplay: Discord closed the gateway (%d)", ce.Code)}
				}
			}
			return err
		}
		if p.S != nil {
			seqMu.Lock()
			s := *p.S
			seq = &s
			seqMu.Unlock()
		}
		switch p.Op {
		case 11:
			select {
			case acked <- struct{}{}:
			default:
			}
		case 1:
			if err := heartbeat(); err != nil {
				return err
			}
		case 7:
			return errors.New("gateway asked to reconnect")
		case 9:
			return errors.New("gateway invalidated the session")
		case 0:
			switch p.T {
			case "READY":
				var r struct {
					User struct {
						ID       string `json:"id"`
						Username string `json:"username"`
					} `json:"user"`
				}
				_ = json.Unmarshal(p.D, &r)
				d.mu.Lock()
				d.user = r.User.ID
				d.mu.Unlock()
				d.cfg.Log(fmt.Sprintf("crowdplay: connected to Discord as %s, reading channel %s", r.User.Username, d.cfg.ChannelID))
			case "MESSAGE_CREATE":
				var m messageCreate
				if json.Unmarshal(p.D, &m) != nil || m.ChannelID != d.cfg.ChannelID || m.Author.Bot || m.WebhookID != "" {
					continue
				}
				d.mu.Lock()
				self := d.user
				d.mu.Unlock()
				if m.Author.ID == "" || m.Author.ID == self {
					continue
				}
				onMessage(twitchplays.DiscordMessage{ID: m.ID, AuthorID: m.Author.ID, AuthorName: m.Author.Username, Content: m.Content, ChannelID: m.ChannelID, Received: time.Now()})
			}
		}
	}
}

// Reply posts a short plain message in the configured channel. At most one per
// ReplyInterval; the rest are dropped (and counted), never queued. Mentions are disabled so a
// reply can never ping anyone.
func (d *Discord) Reply(ctx context.Context, text string) error {
	d.mu.Lock()
	if time.Since(d.lastReply) < d.cfg.ReplyInterval {
		d.dropped++
		d.mu.Unlock()
		return nil
	}
	d.lastReply = time.Now()
	d.mu.Unlock()
	if len(text) > 300 {
		text = text[:300]
	}
	body := map[string]interface{}{"content": text, "allowed_mentions": map[string]interface{}{"parse": []string{}}}
	return d.rest(ctx, http.MethodPost, "/channels/"+d.cfg.ChannelID+"/messages", body, nil)
}

// Dropped is how many replies were dropped by the reply rate limit.
func (d *Discord) Dropped() int {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.dropped
}

func (d *Discord) rest(ctx context.Context, method, path string, in, out interface{}) error {
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, d.cfg.APIBase+path, body)
	if err != nil {
		return err
	}
	req.Header.Set("Authorization", d.cfg.Token.authorization())
	req.Header.Set("User-Agent", "DiscordBot (https://github.com/oldm8clint/rsb0t-go, 1)")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	res, err := d.cfg.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("crowdplay: Discord %s %s: %w", method, path, err)
	}
	defer res.Body.Close()
	if res.StatusCode == http.StatusUnauthorized {
		return errFatal{"crowdplay: Discord refused the bot token (401)"}
	}
	if res.StatusCode/100 != 2 {
		return fmt.Errorf("crowdplay: Discord %s %s: HTTP %d", method, path, res.StatusCode)
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(res.Body, 1<<20)).Decode(out)
	}
	return nil
}
