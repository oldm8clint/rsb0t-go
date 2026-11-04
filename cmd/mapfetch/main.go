// Command mapfetch downloads a server's own map files for some mapsquares the way the web client
// does (the versionlist archive's map_index, then the ondemand protocol on the game WebSocket:
// byte 15, eight bytes back, then [archive, file hi, file lo, priority] requests answered in
// 500-byte chunks) and lists the locs whose names match -match. Read-only: it never logs in.
//
// It exists because the server's maps can differ from the 289 content the collision pack was baked
// from: on 3 Oct a summon left accounts in squares the pack holds no tiles for.
//
//	go run ./cmd/mapfetch -http https://w1.example.invalid/ -ws wss://w1.example.invalid/ -squares 47,161,48,160 -config <config.jag>
package main

import (
	"bytes"
	"compress/gzip"
	"encoding/binary"
	"flag"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/cachecfg"
	"rsb0t-go/internal/locindex"
)

func main() {
	httpBase := flag.String("http", "https://w1.example.invalid/", "the server's web root")
	wsURL := flag.String("ws", "wss://w1.example.invalid/", "the server's game WebSocket")
	squares := flag.String("squares", "47,161", "mapsquares as x,z pairs, comma separated")
	configPath := flag.String("config", "", "config archive (loc names); empty: the server's own")
	match := flag.String("match", "ladder,stair,lever,portal,trapdoor,rope,gate,door,cave,hole,exit,gangplank,boat,tunnel,passage", "loc name substrings to list")
	flag.Parse()

	crc0 := get(*httpBase + "crc0")
	var vl *archive.JagFile
	for i := 0; i+4 <= len(crc0) && i < 9*4; i += 4 {
		crc := int32(binary.BigEndian.Uint32(crc0[i:]))
		raw, err := tryGet(fmt.Sprintf("%sversionlist%d", *httpBase, crc))
		if err != nil {
			continue
		}
		if vl, err = archive.ParseJagFile(raw); err == nil {
			break
		}
	}
	if vl == nil {
		log.Fatal("mapfetch: no versionlist archive found from /crc0")
	}
	idx, err := vl.Read("map_index")
	if err != nil || len(idx)%7 != 0 {
		log.Fatalf("mapfetch: map_index: %v (len %d)", err, len(idx))
	}
	type entry struct{ land, loc int }
	index := map[int]entry{}
	for i := 0; i < len(idx); i += 7 {
		sq := int(binary.BigEndian.Uint16(idx[i:]))
		index[sq] = entry{int(binary.BigEndian.Uint16(idx[i+2:])), int(binary.BigEndian.Uint16(idx[i+4:]))}
	}

	var cfg *cachecfg.Config
	if *configPath != "" {
		if cfg, err = cachecfg.LoadFile(*configPath); err != nil {
			log.Fatal(err)
		}
	} else {
		for i := 0; i+4 <= len(crc0) && i < 9*4; i += 4 {
			crc := int32(binary.BigEndian.Uint32(crc0[i:]))
			raw, err := tryGet(fmt.Sprintf("%sconfig%d", *httpBase, crc))
			if err != nil {
				continue
			}
			if cfg, err = cachecfg.Parse(raw); err == nil {
				break
			}
		}
	}

	dialer := websocket.Dialer{Subprotocols: []string{"binary"}, HandshakeTimeout: 20 * time.Second}
	conn, _, err := dialer.Dial(*wsURL, http.Header{"Origin": {strings.TrimSuffix(*httpBase, "/")}})
	if err != nil {
		log.Fatalf("mapfetch: dial: %v", err)
	}
	defer conn.Close()
	od := &ondemand{conn: conn}
	if err := od.hello(); err != nil {
		log.Fatalf("mapfetch: ondemand hello: %v", err)
	}

	words := strings.Split(*match, ",")
	parts := strings.Split(*squares, ",")
	for i := 0; i+1 < len(parts); i += 2 {
		mx, _ := strconv.Atoi(strings.TrimSpace(parts[i]))
		mz, _ := strconv.Atoi(strings.TrimSpace(parts[i+1]))
		e, ok := index[mx<<8|mz]
		if !ok {
			fmt.Printf("square %d,%d: not in the server's map_index (no map: void)\n", mx, mz)
			continue
		}
		land, err1 := od.fetch(3, e.land)
		locs, err2 := od.fetch(3, e.loc)
		if err1 != nil || err2 != nil {
			fmt.Printf("square %d,%d: fetch land %v loc %v\n", mx, mz, err1, err2)
			continue
		}
		land, locs = gunzip(land), gunzip(locs)
		fmt.Printf("square %d,%d: land file %d (%d bytes), loc file %d (%d bytes)\n", mx, mz, e.land, len(land), e.loc, len(locs))
		baked, err := locindex.BakeSquare(locindex.MapFile{MX: mx, MZ: mz, Land: land, Loc: locs}, func(int) bool { return true })
		if err != nil {
			fmt.Printf("  bake: %v\n", err)
			continue
		}
		for _, l := range baked {
			name := strconv.Itoa(l.ID)
			var ops [5]string
			if cfg != nil {
				if lt, ok := cfg.Loc(l.ID); ok {
					name, ops = lt.Name, lt.Ops
				}
			}
			ln := strings.ToLower(name)
			hit := false
			for _, w := range words {
				if w != "" && strings.Contains(ln, w) {
					hit = true
				}
			}
			if hit {
				fmt.Printf("  %-16s id=%-5d at %d,%d,%d shape=%d ops=%q\n", name, l.ID, l.X, l.Z, l.Level, l.Shape, strings.Join(ops[:], "|"))
			}
		}
	}
}

type ondemand struct {
	conn *websocket.Conn
	buf  []byte
}

func (o *ondemand) need(n int) error {
	for len(o.buf) < n {
		o.conn.SetReadDeadline(time.Now().Add(20 * time.Second))
		_, b, err := o.conn.ReadMessage()
		if err != nil {
			return err
		}
		o.buf = append(o.buf, b...)
	}
	return nil
}

func (o *ondemand) take(n int) []byte {
	b := o.buf[:n:n]
	o.buf = o.buf[n:]
	return b
}

func (o *ondemand) hello() error {
	if err := o.conn.WriteMessage(websocket.BinaryMessage, []byte{15}); err != nil {
		return err
	}
	if err := o.need(8); err != nil {
		return err
	}
	o.take(8)
	return nil
}

// fetch asks for one file and reads its chunks: a 6-byte header (archive, file u16, total size
// u16, chunk index) then up to 500 bytes.
func (o *ondemand) fetch(arch, file int) ([]byte, error) {
	if err := o.conn.WriteMessage(websocket.BinaryMessage, []byte{byte(arch), byte(file >> 8), byte(file), 2}); err != nil {
		return nil, err
	}
	var data []byte
	for {
		if err := o.need(6); err != nil {
			return nil, err
		}
		h := o.take(6)
		a, f, size, chunk := int(h[0]), int(h[1])<<8|int(h[2]), int(h[3])<<8|int(h[4]), int(h[5])
		if size == 0 {
			return nil, fmt.Errorf("server has no file %d/%d", arch, file)
		}
		n := min(500, size-500*chunk)
		if err := o.need(n); err != nil {
			return nil, err
		}
		body := o.take(n)
		if a != arch || f != file {
			continue
		}
		if data == nil {
			data = make([]byte, size)
		}
		copy(data[500*chunk:], body)
		if 500*chunk+n >= size {
			return data, nil
		}
	}
}

// gunzip drops the 2-byte version trailer every ondemand file carries (the worker's validate reads
// the last two bytes as the version) and inflates the rest.
func gunzip(b []byte) []byte {
	if len(b) > 2 && !(b[len(b)-2] == 0 && b[len(b)-1] == 0 && len(b) < 4) {
		b = b[:len(b)-2]
	}
	if len(b) > 2 && b[0] == 0x1f && b[1] == 0x8b {
		if zr, err := gzip.NewReader(bytes.NewReader(b)); err == nil {
			if out, err := io.ReadAll(zr); err == nil {
				return out
			}
		}
	}
	return b
}

func get(url string) []byte {
	b, err := tryGet(url)
	if err != nil {
		log.Fatalf("mapfetch: %s: %v", url, err)
	}
	return b
}

func tryGet(url string) ([]byte, error) {
	resp, err := http.Get(url)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(resp.Body)
}
