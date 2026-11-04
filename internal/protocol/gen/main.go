// Command gen regenerates internal/protocol/clientprot.go and serverprot.go from a
// LostCityRS-family engine checkout, the same way rsb0t-main's tools/gen-prot.ts
// regenerates its own committed ClientProt.ts/ServerProt.ts. Kept as a real, rerunnable
// tool rather than a one-off script specifically so re-deriving against a different
// engine checkout later (or checking for drift again) doesn't mean hand-editing 150
// opcode constants.
//
// Usage: go run ./internal/protocol/gen [-engine <path>] [-out <dir>]
// Defaults: -engine ../engine (this repo's Phase 0 local test engine), -out . (this package).
package main

import (
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// alias mirrors gen-prot.ts's ALIAS table exactly: some opcodes were renamed between the
// engine's internal naming and the client's historical call-site names.
var alias = map[string]string{
	"IF_SETTAB":             "IF_SETICON",
	"IF_SETTAB_ACTIVE":      "IF_SHOWICON",
	"RESET_CLIENT_VARCACHE": "VARP_SYNC",
	"LOC_MERGE":             "P_LOCMERGE",
	"REPORT_ABUSE":          "SEND_SNAPSHOT",
}

type prot struct {
	name   string
	id     int
	length int
}

var protRe = regexp.MustCompile(`static readonly ([A-Z0-9_]+) = new [A-Za-z]+\((-?\d+),\s*(-?\d+)\)`)

func parse(source string) ([]prot, error) {
	var out []prot
	for _, m := range protRe.FindAllStringSubmatch(source, -1) {
		id, err := strconv.Atoi(m[2])
		if err != nil {
			return nil, fmt.Errorf("bad id in %q: %w", m[0], err)
		}
		length, err := strconv.Atoi(m[3])
		if err != nil {
			return nil, fmt.Errorf("bad length in %q: %w", m[0], err)
		}
		name := m[1]
		if renamed, ok := alias[name]; ok {
			name = renamed
		}
		out = append(out, prot{name: name, id: id, length: length})
	}
	return out, nil
}

func parseRevision(worldConfigSource string) (int, error) {
	re := regexp.MustCompile(`revision:\s*(\d+)`)
	m := re.FindStringSubmatch(worldConfigSource)
	if m == nil {
		return 0, fmt.Errorf("could not find revision: <n> in WorldConfig.ts")
	}
	return strconv.Atoi(m[1])
}

func readFile(path string) (string, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return "", err
	}
	return string(data), nil
}

func renderConsts(prots []prot) string {
	var b strings.Builder
	b.WriteString("const (\n")
	for _, p := range prots {
		fmt.Fprintf(&b, "\t%s = %d\n", p.name, p.id)
	}
	b.WriteString(")\n")
	return b.String()
}

// renderMap emits a name->id map alongside the named constants above, so tests (and any
// other code that wants to enumerate opcodes rather than name them one by one) can
// verify or inspect the table at runtime instead of only at compile time.
func renderMap(varName string, prots []prot) string {
	var b strings.Builder
	fmt.Fprintf(&b, "var %s = map[string]int{\n", varName)
	for _, p := range prots {
		fmt.Fprintf(&b, "\t%q: %d,\n", p.name, p.id)
	}
	b.WriteString("}\n")
	return b.String()
}

func renderSizes(prots []prot) string {
	sizes := make([]int, 256)
	for _, p := range prots {
		sizes[p.id] = p.length
	}
	var b strings.Builder
	b.WriteString("// ServerProtSizes[opcode] gives the VARBYTE/VARSHORT/fixed length rule for that\n")
	b.WriteString("// server->client opcode: >=0 is a fixed byte count, -1 means one more length byte\n")
	b.WriteString("// follows (VARBYTE), -2 means two more big-endian length bytes follow (VARSHORT).\n")
	b.WriteString("var ServerProtSizes = [256]int{\n")
	for i := 0; i < 256; i += 10 {
		end := i + 10
		if end > 256 {
			end = 256
		}
		b.WriteString("\t")
		for j := i; j < end; j++ {
			fmt.Fprintf(&b, "%d, ", sizes[j])
		}
		b.WriteString("\n")
	}
	b.WriteString("}\n")
	return b.String()
}

func main() {
	engineDir := flag.String("engine", "../engine", "path to the LostCityRS Engine-TS checkout")
	outDir := flag.String("out", ".", "directory to write clientprot.go/serverprot.go into")
	flag.Parse()

	clientSrc, err := readFile(filepath.Join(*engineDir, "src/network/game/client/ClientGameProt.ts"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading ClientGameProt.ts:", err)
		os.Exit(1)
	}
	serverSrc, err := readFile(filepath.Join(*engineDir, "src/network/game/server/ServerGameProt.ts"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading ServerGameProt.ts:", err)
		os.Exit(1)
	}
	serverZoneSrc, err := readFile(filepath.Join(*engineDir, "src/network/game/server/ServerGameZoneProt.ts"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading ServerGameZoneProt.ts:", err)
		os.Exit(1)
	}
	worldConfigSrc, err := readFile(filepath.Join(*engineDir, "src/util/WorldConfig.ts"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "reading WorldConfig.ts:", err)
		os.Exit(1)
	}

	revision, err := parseRevision(worldConfigSrc)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}

	clientProts, err := parse(clientSrc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parsing client opcodes:", err)
		os.Exit(1)
	}
	serverProts, err := parse(serverSrc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parsing server opcodes:", err)
		os.Exit(1)
	}
	serverZoneProts, err := parse(serverZoneSrc)
	if err != nil {
		fmt.Fprintln(os.Stderr, "parsing server zone opcodes:", err)
		os.Exit(1)
	}
	allServerProts := append(serverProts, serverZoneProts...)

	// Two sub-packages, not one: ClientProt and ServerProt are separate TS enums and a
	// few names (e.g. MESSAGE_PRIVATE) collide between them — Go has no per-enum
	// namespacing for plain consts, so this mirrors the TS separation with packages
	// instead (clientprot.MESSAGE_PRIVATE vs serverprot.MESSAGE_PRIVATE).
	header := "// Code generated by internal/protocol/gen from the engine at %s (revision %d). DO NOT EDIT.\n// See rsb0t-main's tools/gen-prot.ts, which this mirrors, and README.md's Phase 0\n// note: this engine's opcode tables were verified to match rsb0t-main's committed\n// ClientProt.ts/ServerProt.ts exactly (zero drift) before this generator was trusted.\n\npackage %s\n\n"

	clientOut := fmt.Sprintf(header, *engineDir, revision, "clientprot") + fmt.Sprintf("const ClientVersion = %d\n\n", revision) + renderConsts(clientProts) + "\n" + renderMap("ByName", clientProts)
	serverOut := fmt.Sprintf(header, *engineDir, revision, "serverprot") + renderConsts(allServerProts) + "\n" + renderMap("ByName", allServerProts) + "\n" + renderSizes(allServerProts)

	clientDir := filepath.Join(*outDir, "clientprot")
	serverDir := filepath.Join(*outDir, "serverprot")
	if err := os.MkdirAll(clientDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "creating clientprot dir:", err)
		os.Exit(1)
	}
	if err := os.MkdirAll(serverDir, 0755); err != nil {
		fmt.Fprintln(os.Stderr, "creating serverprot dir:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(clientDir, "clientprot.go"), []byte(clientOut), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "writing clientprot.go:", err)
		os.Exit(1)
	}
	if err := os.WriteFile(filepath.Join(serverDir, "serverprot.go"), []byte(serverOut), 0644); err != nil {
		fmt.Fprintln(os.Stderr, "writing serverprot.go:", err)
		os.Exit(1)
	}

	names := make([]string, 0, len(clientProts)+len(allServerProts))
	for _, p := range clientProts {
		names = append(names, p.name)
	}
	sort.Strings(names)
	fmt.Printf("wrote clientprot.go (%d opcodes, revision %d) and serverprot.go (%d opcodes) to %s\n", len(clientProts), revision, len(allServerProts), *outDir)
}
