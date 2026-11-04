// Command cachecfgtest loads the config archive and resolves type names both ways. With
// -ws it also logs one account in and prints the NPCs in view by name and options, the
// live proof that gamestate's raw type ids and internal/cachecfg agree.
//
// Usage:
//
//	go run ./cmd/cachecfgtest -config config.jag -find "loc:Bank booth,obj:Flax,npc:Man"
//	go run ./cmd/cachecfgtest -http http://localhost:8900/ -save config.jag
//	go run ./cmd/cachecfgtest -http http://localhost:8900/ -ws ws://localhost:8900/ \
//	    -mod <modulus> -exp <exponent> -user cfgtest1
//
// -http fetches GET /crc0, then GET /config<crc> with crc = index 2. Use a local engine and
// a throwaway username for -ws; local engines auto-register and ignore the password.
package main

import (
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"
	"sort"
	"strings"
	"time"

	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/client"
)

func main() {
	configPath := flag.String("config", "", "config archive on disk")
	httpBase := flag.String("http", "", "engine HTTP base to fetch the config archive from")
	save := flag.String("save", "", "write the fetched archive here")
	find := flag.String("find", "loc:Bank booth,loc:Spinning wheel,obj:Flax,obj:Bow string,npc:Banker", "comma-separated kind:name lookups")
	wsURL := flag.String("ws", "", "engine WebSocket URL; logs in and lists visible NPCs by name")
	modStr := flag.String("mod", "", "RSA modulus (decimal)")
	expStr := flag.String("exp", "65537", "RSA exponent (decimal)")
	user := flag.String("user", "cfgtest1", "username (use a throwaway account on a local engine)")
	pass := flag.String("pass", "cfgtest", "password")
	wait := flag.Duration("wait", 4*time.Second, "how long to read packets before listing NPCs")
	flag.Parse()

	raw, err := archiveBytes(*configPath, *httpBase)
	if err != nil {
		fail(err)
	}
	if *save != "" {
		if err := os.WriteFile(*save, raw, 0o644); err != nil {
			fail(err)
		}
		fmt.Printf("saved %d bytes to %s\n", len(raw), *save)
	}

	t0 := time.Now()
	cfg, err := cachecfg.Parse(raw)
	if err != nil {
		fail(err)
	}
	objs, npcs, locs := cfg.Counts()
	fmt.Printf("decoded %d objs, %d npcs, %d locs in %v\n", objs, npcs, locs, time.Since(t0).Round(time.Millisecond))

	for _, q := range strings.Split(*find, ",") {
		kind, name, ok := strings.Cut(strings.TrimSpace(q), ":")
		if !ok {
			continue
		}
		switch kind {
		case "obj":
			for _, id := range cfg.ObjIDs(name) {
				o, _ := cfg.Obj(id)
				fmt.Printf("  obj %5d %-20q noted=%v members=%v ops=%q iops=%q\n", id, o.Name, o.Noted(), o.Members, o.Ops, o.IOps)
			}
		case "npc":
			for _, id := range cfg.NpcIDs(name) {
				n, _ := cfg.Npc(id)
				fmt.Printf("  npc %5d %-20q level=%d ops=%q\n", id, n.Name, n.Level, n.Ops)
			}
		case "loc":
			for _, id := range cfg.LocIDs(name) {
				l, _ := cfg.Loc(id)
				fmt.Printf("  loc %5d %-20q %dx%d active=%v ops=%q\n", id, l.Name, l.Width, l.Length, l.Active, l.Ops)
			}
		}
	}

	if *wsURL == "" {
		return
	}
	if *httpBase == "" || *modStr == "" {
		fail(fmt.Errorf("-ws needs -http and -mod"))
	}
	mod, ok1 := new(big.Int).SetString(*modStr, 10)
	exp, ok2 := new(big.Int).SetString(*expStr, 10)
	if !ok1 || !ok2 {
		fail(fmt.Errorf("-mod and -exp must be decimal integers"))
	}
	s, err := client.Connect(client.Config{WSURL: *wsURL, HTTPBase: *httpBase, Username: *user, Password: *pass, Modulus: mod, Exponent: exp, LowMem: true})
	if err != nil {
		fail(err)
	}
	defer s.Close()
	stop := make(chan struct{})
	defer close(stop)
	go func() { _ = s.ReadLoop(stop, nil) }()
	time.Sleep(*wait)

	x, z, level, haveTile := s.State().Tile()
	fmt.Printf("logged in as %s (response %d); tile %d,%d,%d known=%v\n", *user, s.LoginResponse, x, z, level, haveTile)
	visible := s.State().NPCs()
	sort.Slice(visible, func(i, j int) bool { return visible[i].Index < visible[j].Index })
	fmt.Printf("%d NPCs in view:\n", len(visible))
	for i, n := range visible {
		if i == 25 {
			fmt.Printf("  ... %d more\n", len(visible)-i)
			break
		}
		t, ok := cfg.Npc(n.Type)
		if !ok {
			fmt.Printf("  #%-5d type %d is outside the archive\n", n.Index, n.Type)
			continue
		}
		fmt.Printf("  #%-5d %-24q type %-5d at %d,%d  ops=%q\n", n.Index, t.Name, n.Type, n.X, n.Z, t.Ops)
	}
}

func archiveBytes(path, httpBase string) ([]byte, error) {
	if path != "" {
		return os.ReadFile(path)
	}
	if httpBase == "" {
		return nil, fmt.Errorf("give -config or -http")
	}
	crc, err := client.FetchJagChecksums(httpBase)
	if err != nil {
		return nil, err
	}
	resp, err := http.Get(fmt.Sprintf("%sconfig%d", httpBase, crc[2]))
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("config archive: %s", resp.Status)
	}
	return io.ReadAll(resp.Body)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "cachecfgtest:", err)
	os.Exit(1)
}
