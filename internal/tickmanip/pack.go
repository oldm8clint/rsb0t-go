package tickmanip

import (
	"fmt"
	"strconv"
	"strings"
)

// ParsePack reads a content/pack/*.pack file (one "id=name" per line: obj.pack, npc.pack, loc.pack,
// seq.pack, interface.pack) into name -> id. A repeated name keeps its first id. Blank lines and lines
// without '=' are skipped; a line whose id is not a number is an error, so a corrupt pack cannot
// silently shift every id.
func ParsePack(text string) (map[string]int, error) {
	out := map[string]int{}
	for n, line := range strings.Split(text, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		eq := strings.IndexByte(line, '=')
		if eq < 0 {
			continue
		}
		id, err := strconv.Atoi(line[:eq])
		if err != nil {
			return nil, fmt.Errorf("pack line %d %q: id is not a number", n+1, line)
		}
		name := line[eq+1:]
		if _, dup := out[name]; !dup {
			out[name] = id
		}
	}
	return out, nil
}

// Reverse turns name -> id into id -> name.
func Reverse(m map[string]int) map[int]string {
	out := make(map[int]string, len(m))
	for name, id := range m {
		if _, dup := out[id]; !dup {
			out[id] = name
		}
	}
	return out
}
