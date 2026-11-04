// Command logintest is Phase 1's actual milestone check: dial the local test engine,
// run the real login handshake, and report the response code. Not a `go test` — this is
// a one-shot manual verification tool for a live server, run deliberately rather than on
// every `go test ./...`.
//
// Usage: go run ./cmd/logintest -user <name> -pass <pass> [-url ws://localhost/] [-mod ...] [-exp ...]
package main

import (
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"os"

	"rsb0t-go/internal/login"
	"rsb0t-go/internal/protocol/serverprot"
	"rsb0t-go/internal/transport"
)

// serverProtName reverse-looks-up an opcode's name for readable logging.
func serverProtName(opcode int) string {
	for name, id := range serverprot.ByName {
		if id == opcode {
			return name
		}
	}
	return "UNKNOWN"
}

// fetchJagChecksums downloads the engine's own CrcBuffer (see cache/CrcTable.ts:
// 9 big-endian uint32 archive CRCs followed by a 10th combined-hash uint32 the server
// does NOT check at login) from /crc<anything> and returns just the first 9 as the
// login packet's JagChecksums — this must be byte-identical to what the server computed
// itself, not independently recomputed, since the server compares raw bytes.
func fetchJagChecksums(baseURL string) ([9]int32, error) {
	var out [9]int32
	resp, err := http.Get(baseURL + "crc0")
	if err != nil {
		return out, err
	}
	defer resp.Body.Close()
	data, err := io.ReadAll(resp.Body)
	if err != nil {
		return out, err
	}
	if len(data) < 36 {
		return out, fmt.Errorf("expected at least 36 bytes from /crc0, got %d", len(data))
	}
	for i := 0; i < 9; i++ {
		out[i] = int32(binary.BigEndian.Uint32(data[i*4:]))
	}
	return out, nil
}

func main() {
	url := flag.String("url", "ws://localhost/", "WebSocket URL of the local test engine")
	httpBase := flag.String("http", "http://localhost/", "HTTP base URL of the same engine, for fetching the real cache checksums")
	user := flag.String("user", "gotestacct", "username to log in with (auto-registered if it doesn't exist, per WEBSITE_REGISTRATION=false)")
	pass := flag.String("pass", "password123", "password")
	modStr := flag.String("mod", "7162900525229798032761816791230527296329313291232324290237849263501208207972894053929065636522363163621000728841182238772712427862772219676577293600221789", "RSA modulus (decimal), from data/config/private.pem")
	expStr := flag.String("exp", "58778699976184461502525193738213253649000149147835990136706041084440742975821", "RSA public exponent (decimal), from data/config/private.pem")
	packets := flag.Int("packets", 15, "how many framed server packets to decode after login; 0 reads until the server goes quiet")
	watchOp := flag.Int("watch-op", serverprot.LAST_LOGIN_INFO, "report whether this server opcode arrived, decoding it when it is 253 (LAST_LOGIN_INFO); -1 disables")
	flag.Parse()

	mod, ok := new(big.Int).SetString(*modStr, 10)
	if !ok {
		fmt.Fprintln(os.Stderr, "bad -mod value")
		os.Exit(1)
	}
	exp, ok := new(big.Int).SetString(*expStr, 10)
	if !ok {
		fmt.Fprintln(os.Stderr, "bad -exp value")
		os.Exit(1)
	}

	checksums, err := fetchJagChecksums(*httpBase)
	if err != nil {
		fmt.Fprintln(os.Stderr, "fetching jag checksums:", err)
		os.Exit(1)
	}
	fmt.Printf("fetched jag checksums: %v\n", checksums)

	fmt.Printf("dialing %s ...\n", *url)
	stream, err := transport.Dial(*url)
	if err != nil {
		fmt.Fprintln(os.Stderr, "dial:", err)
		os.Exit(1)
	}
	defer stream.Close()
	fmt.Println("connected, performing login handshake...")

	result, err := login.PerformLogin(stream, login.Options{
		Username:     *user,
		Password:     *pass,
		Modulus:      mod,
		Exponent:     exp,
		JagChecksums: checksums,
	})
	if err != nil {
		fmt.Fprintln(os.Stderr, "login:", err)
		os.Exit(1)
	}

	fmt.Printf("response code: %d\n", result.Response)
	switch result.Response {
	case 2:
		fmt.Println("SUCCESS: logged in (fresh session)")
	case 15:
		fmt.Println("SUCCESS: logged in (reconnect session)")
	case 3:
		fmt.Println("invalid username or password")
	case 4:
		fmt.Println("account disabled")
	case 5:
		fmt.Println("already logged in")
	case 6:
		fmt.Println("bad/stale RSA key — the engine's key may have changed; re-derive it")
	case 7:
		fmt.Println("world full")
	case 8:
		fmt.Println("login server offline")
	case 9:
		fmt.Println("login limit exceeded")
	case 14:
		fmt.Println("server updating")
	default:
		fmt.Printf("unhandled/unexpected response code %d — check the engine's own log output\n", result.Response)
	}

	if result.OutCipher == nil {
		return
	}
	fmt.Println("ISAAC cipher pair established — reading the post-login bytes and a few game packets to prove the incoming loop out...")

	// performLogin's response==2 branch reads exactly these two more bytes before the
	// real game packet stream begins.
	staffModLevel, err := stream.ReadByte()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading staffModLevel:", err)
		os.Exit(1)
	}
	mouseTrackedByte, err := stream.ReadByte()
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading mouseTracked:", err)
		os.Exit(1)
	}
	fmt.Printf("staffModLevel=%d mouseTracked=%v\n", staffModLevel, mouseTrackedByte == 1)

	// Decode real incoming game packets: ISAAC-decode the opcode byte, look up its framing
	// rule, read exactly that many payload bytes, and print what arrived — the actual "stay
	// connected and correctly length-frame every server packet" check Phase 1 cares about.
	//
	// -packets still defaults to 15, so every existing caller sees exactly what it always
	// saw. Raise it to watch for something the login burst sends LATE: content runs
	// last_login_info at the very end of [login,_] (login.rs2:88), so LAST_LOGIN_INFO lands
	// well past 15 and the old fixed cap could never have observed it either way.
	res := scanPackets(stream, result.InCipher.NextInt, *packets, *watchOp, os.Stdout)

	fmt.Printf("scanned %d packet(s) with no decode desync", res.Scanned)
	switch {
	case res.Conclusive():
		fmt.Printf("; %s, so that was the whole session.\n", whyItEnded(res))
	case res.Err != nil:
		fmt.Printf("; stopped on a read error: %v\n", res.Err)
	default:
		fmt.Printf("; stopped at the -packets limit of %d, so more may follow.\n", *packets)
	}

	if *watchOp >= 0 {
		reportWatch(os.Stdout, res, *watchOp, *packets)
	}
}
