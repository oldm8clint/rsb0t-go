package config

import (
	"bytes"
	"fmt"
	"math"
	"math/big"
	"net"
	"net/url"
	"os"
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

var (
	reflectAccount     = reflect.TypeOf(Account{})
	profileNamePattern = regexp.MustCompile(`^[A-Za-z0-9_-]{1,32}$`)
	scriptNamePattern  = regexp.MustCompile(`^[A-Za-z][A-Za-z0-9_-]{0,63}$`)
	envNamePattern     = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)
	validLogLevels     = map[string]bool{"debug": true, "info": true, "warn": true, "error": true}
)

const (
	// liveProfile is the one profile that needs no login key of its own.
	liveProfile    = "live"
	minMemoryLimit = 8 * MiB
)

// isLoopbackHost is true for localhost and any loopback IP. An empty host means every interface.
func isLoopbackHost(host string) bool {
	if strings.EqualFold(host, "localhost") {
		return true
	}
	ip := net.ParseIP(host)
	return ip != nil && ip.IsLoopback()
}

func (r reporter) intRange(field string, v, lo, hi int) {
	if v < lo || v > hi {
		r.add(field, "must be between %d and %d, got %d", lo, hi, v)
	}
}

func (r reporter) durRange(field string, v Duration, lo, hi time.Duration) {
	if v.D() < lo || v.D() > hi {
		r.add(field, "must be between %s and %s, got %s", formatDuration(lo), formatDuration(hi), v)
	}
}

func (g *Global) normalise(r reporter, base string, lookup func(string) (string, bool)) {
	g.Server.normalise(r, base)
	g.Data.Collision = checkFile(r, "data.collision", base, g.Data.Collision)

	l := &g.Login
	r.intRange("login.maxSessions", l.MaxSessions, 0, 100000)
	r.durRange("login.loginDelay", l.LoginDelay, 0, 10*time.Minute)
	l.CredentialsFile = checkFile(r, "login.credentialsFile", base, l.CredentialsFile)

	rt := &g.Runtime
	if rt.MemoryLimit == "off" {
		rt.MemoryLimitBytes = math.MaxInt64
	} else if size, err := ParseByteSize(rt.MemoryLimit); err != nil {
		r.add("runtime.memoryLimit", "want \"off\" or a size such as \"512MiB\": %s", err.Error())
	} else if size < minMemoryLimit {
		r.add("runtime.memoryLimit", "must be \"off\" or at least %s, got %s", minMemoryLimit, size)
	} else {
		rt.MemoryLimitBytes = int64(size)
	}
	if rt.GCPercent != -1 && (rt.GCPercent < 10 || rt.GCPercent > 1000) {
		r.add("runtime.gcPercent", "must be -1 (off) or between 10 and 1000, got %d", rt.GCPercent)
	}
	if rt.GCPercent == -1 && rt.MemoryLimit == "off" {
		r.add("runtime.gcPercent", "-1 turns the collector off, which needs runtime.memoryLimit to be a size")
	}
	r.intRange("runtime.maxProcs", rt.MaxProcs, 0, 1024)

	lg := &g.Log
	if !validLogLevels[lg.Level] {
		r.add("log.level", "want debug, info, warn or error, got %q", lg.Level)
	}
	switch lg.Output {
	case "shared":
	case "per-account":
		if lg.Dir == "" {
			r.add("log.dir", "is required when log.output is \"per-account\"")
		}
	default:
		r.add("log.output", "want \"shared\" or \"per-account\", got %q", lg.Output)
	}
	lg.File = resolvePath(base, lg.File)
	lg.Dir = resolvePath(base, lg.Dir)

	t := &g.Telemetry
	if m := t.MemReportEvery.D(); m != 0 && (m < 10*time.Second || m > 24*time.Hour) {
		r.add("telemetry.memReportEvery", "must be 0s (off) or between 10s and 24h, got %s", t.MemReportEvery)
	}

	g.ControlPanel.normalise(r, base, lookup)
}

func (cp *ControlPanel) normalise(r reporter, base string, lookup func(string) (string, bool)) {
	tokenConfigured := cp.TokenEnv != "" || cp.TokenFile != ""
	if cp.TokenEnv != "" && cp.TokenFile != "" {
		r.add("controlPanel.tokenFile", "set controlPanel.tokenEnv or controlPanel.tokenFile, not both")
	}
	if cp.TokenEnv != "" && !envNamePattern.MatchString(cp.TokenEnv) {
		r.add("controlPanel.tokenEnv", "want an environment variable name, got %q", cp.TokenEnv)
	}
	if host, port, err := net.SplitHostPort(cp.Listen); err != nil {
		r.add("controlPanel.listen", "want host:port such as \"127.0.0.1:7274\", got %q", cp.Listen)
	} else {
		if n, err := strconv.Atoi(port); err != nil || n < 1 || n > 65535 {
			r.add("controlPanel.listen", "want a port between 1 and 65535, got %q", port)
		}
		if !isLoopbackHost(host) && !tokenConfigured {
			r.add("controlPanel.listen", "host %q is reachable from other machines; listen on 127.0.0.1 or set controlPanel.tokenEnv or controlPanel.tokenFile", host)
		}
	}
	cp.LayoutFile = resolvePath(base, cp.LayoutFile)
	r.intRange("controlPanel.eventHistory", cp.EventHistory, 16, 2048)
	r.intRange("controlPanel.logHistory", cp.LogHistory, 16, 2048)
	r.intRange("controlPanel.memoryHistory", cp.MemoryHistory, 10, 3600)

	if !cp.Enabled {
		cp.TokenFile = resolvePath(base, cp.TokenFile)
		return
	}
	setToken := func(field, v string) {
		if len(v) < 16 {
			r.add(field, "the access token must be at least 16 characters")
			return
		}
		cp.Token = Secret(v)
	}
	switch {
	case cp.TokenEnv != "" && cp.TokenFile == "":
		if v, ok := lookup(cp.TokenEnv); ok && v != "" {
			setToken("controlPanel.tokenEnv", v)
		} else {
			r.add("controlPanel.tokenEnv", "environment variable %s is unset or empty", cp.TokenEnv)
		}
	case cp.TokenFile != "" && cp.TokenEnv == "":
		cp.TokenFile = checkFile(r, "controlPanel.tokenFile", base, cp.TokenFile)
		if b, err := os.ReadFile(cp.TokenFile); err == nil {
			setToken("controlPanel.tokenFile", strings.TrimSpace(string(bytes.TrimPrefix(b, utf8BOM))))
		}
	}
}

func (s *Server) normalise(r reporter, base string) {
	r.intRange("server.world", s.World, 1, 255)
	r.durRange("server.dialTimeout", s.DialTimeout, time.Second, 2*time.Minute)
	r.durRange("server.readTimeout", s.ReadTimeout, 5*time.Second, 10*time.Minute)

	names := make([]string, 0, len(s.Profiles))
	for name := range s.Profiles {
		names = append(names, name)
	}
	sort.Strings(names)
	for _, name := range names {
		field := "server.profiles." + name
		if !profileNamePattern.MatchString(name) {
			r.add(field, "a profile name may hold only letters, digits, _ and -, at most 32 characters")
			continue
		}
		p := s.Profiles[name]
		p.normalise(r, field, base, name, name == s.Profile)
		s.Profiles[name] = p
	}
	active, ok := s.Profiles[s.Profile]
	if !ok {
		r.add("server.profile", "no profile named %q; have %s", s.Profile, strings.Join(names, ", "))
		return
	}
	s.Active = active
}

func (p *ServerProfile) normalise(r reporter, field, base, name string, active bool) {
	if p.URL == "" {
		r.add(field+".url", "is required")
	} else if u, err := url.Parse(p.URL); err != nil || (u.Scheme != "ws" && u.Scheme != "wss") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
		r.add(field+".url", "want ws://host[:port]/ or wss://host[:port]/, got %q", p.URL)
	}
	if p.HTTPBase != "" {
		if u, err := url.Parse(p.HTTPBase); err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" {
			r.add(field+".httpBase", "want http://host[:port]/ or https://host[:port]/, got %q", p.HTTPBase)
		} else if !strings.HasSuffix(p.HTTPBase, "/") {
			p.HTTPBase += "/"
		}
	}

	switch n := len(p.CRCTable); n {
	case 0:
		p.CRCTable = nil
	case 9:
		p.CRC = make([]uint32, n)
		for i, v := range p.CRCTable {
			if v < -1<<31 || v > 1<<32-1 {
				r.add(fmt.Sprintf("%s.crcTable[%d]", field, i), "want a 32-bit checksum, got %d", v)
				continue
			}
			p.CRC[i] = uint32(v)
		}
	default:
		r.add(field+".crcTable", "want the 9 archive checksums or none, got %d", n)
	}

	switch {
	case p.RSAExponent == "" && name == liveProfile:
		p.RSAExponent = "65537"
	case p.RSAExponent == "" && active:
		r.add(field+".rsaExponent", "is required: a local engine's exponent is usually not 65537, so read it from its data/config/private.pem")
	case p.RSAExponent != "":
		if e, ok := new(big.Int).SetString(p.RSAExponent, 10); !ok || !isDigits(p.RSAExponent) || e.Cmp(big.NewInt(3)) < 0 || e.Bit(0) == 0 {
			r.add(field+".rsaExponent", "want an odd decimal exponent of at least 3, got %q", p.RSAExponent)
		}
	}

	modulusField := field + ".rsaModulus"
	if p.RSAModulusFile != "" && p.RSAModulus != "" {
		r.add(field+".rsaModulusFile", "set rsaModulus or rsaModulusFile, not both")
	}
	if !active {
		p.RSAModulusFile = resolvePath(base, p.RSAModulusFile)
	} else {
		if p.RSAModulusFile != "" && p.RSAModulus == "" {
			modulusField = field + ".rsaModulusFile"
			p.RSAModulusFile = checkFile(r, modulusField, base, p.RSAModulusFile)
			if b, err := os.ReadFile(p.RSAModulusFile); err == nil {
				p.RSAModulus = strings.TrimSpace(string(bytes.TrimPrefix(b, utf8BOM)))
			}
		}
		keySet := p.RSAModulus != "" || p.RSAModulusFile != ""
		if name != liveProfile && !keySet {
			r.add(field, "needs its own login key: set rsaModulus or rsaModulusFile, and rsaExponent, because only %q works without one", liveProfile)
		}
		if p.HTTPBase == "" && (p.CRCTable == nil || !keySet) {
			r.add(field+".httpBase", "is required unless crcTable and rsaModulus (or rsaModulusFile) are both set, because the login checksums and key are otherwise fetched from it")
		}
	}
	// Why: the TS client's parseLoginModulus accepts only a run of at least 250 decimal digits.
	if p.RSAModulus != "" && (len(p.RSAModulus) < 250 || !isDigits(p.RSAModulus)) {
		r.add(modulusField, "want the login modulus as at least 250 decimal digits")
	}
}

func isDigits(s string) bool {
	if s == "" {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}

// validate checks an account's own fields. identity is false for the defaults section, which has
// no username.
func (a *Account) validate(r reporter, identity bool) {
	user := "X"
	if identity {
		if msg := checkUsername(a.Username); msg != "" {
			r.add("username", "%s", msg)
		} else {
			user = a.Username
		}
	}
	if a.Script != "" && !scriptNamePattern.MatchString(a.Script) {
		r.add("script", "want a letter followed by letters, digits, _ or -, at most 64 characters, got %q", a.Script)
	}
	if a.PasswordEnv != "" && !envNamePattern.MatchString(expandUser(a.PasswordEnv, user)) {
		r.add("passwordEnv", "want an environment variable name, optionally containing %s, got %q", userPlaceholder, a.PasswordEnv)
	}
	if a.LogLevel != "" && !validLogLevels[a.LogLevel] {
		r.add("logLevel", "want debug, info, warn or error, got %q", a.LogLevel)
	}

	r.durRange("startDelay", a.StartDelay, 0, 24*time.Hour)
	r.durRange("startJitter", a.StartJitter, 0, time.Hour)
	if a.MaxRuntime < 0 {
		r.add("maxRuntime", "must be 0s (no limit) or longer, got %s", a.MaxRuntime)
	}
	if s := bytes.TrimSpace(a.Settings); len(s) > 0 && s[0] != '{' {
		r.add("settings", "want an object, got %s", jsonKind(s))
	}

	r.intRange("state.viewRadius", a.State.ViewRadius, 1, 15)

	rc := &a.Reconnect
	r.durRange("reconnect.initial", rc.Initial, time.Second, time.Hour)
	if rc.Max.D() < rc.Initial.D() || rc.Max.D() > time.Hour {
		r.add("reconnect.max", "must be at least reconnect.initial (%s) and at most 1h, got %s", rc.Initial, rc.Max)
	}
	if rc.Multiplier < 1 || rc.Multiplier > 10 {
		r.add("reconnect.multiplier", "must be between 1 and 10, got %g", rc.Multiplier)
	}
	if rc.Jitter < 0 || rc.Jitter > 1 {
		r.add("reconnect.jitter", "must be between 0 and 1, got %g", rc.Jitter)
	}
	r.intRange("reconnect.maxFailures", rc.MaxFailures, 0, 10000)
}
