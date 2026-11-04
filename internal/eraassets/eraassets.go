// Package eraassets opens an operator-supplied directory of game assets for an era other
// than the fleet's own 2004 one, and answers two questions about it: is this era's asset
// set actually here, and what is in it.
//
// WHY THIS IS A DISK ROOT AND NOT A DIRECTORY IN THIS REPOSITORY. Three reasons, in
// descending order of how much they matter:
//
//  1. It is what this repo already does for 2004. The Lost City cache is never in Git; it
//     is a path the operator passes with -capture-cache. Era assets are the same kind of
//     thing and get the same treatment, so there is one rule rather than two.
//  2. Size. A build-530 (2009) filestore is about 86 MB and a 2011 one 148-205 MB. Binary
//     blobs of that size in Git cost every clone, every worktree and every CI run forever,
//     and they do not diff.
//  3. Provenance. The upstream projects that publish these caches license their own CODE
//     (AGPL-3.0 for Open-RSC and 2009scape, BSD-3-Clause for 2011Scape); none of them owns
//     the game art, so none of them can grant a licence to it. Keeping the art on an
//     operator's disk, with its PROVENANCE.txt beside it, keeps that question where it
//     belongs — with whoever chose to put the files there — instead of baking an answer
//     into this repository. docs/stream_client_panel.md records the per-source position.
//
// WHAT THIS PACKAGE DOES NOT DO. It does not render, decode models, or map an entity from
// one era's ids onto another's. It reads containers and counts what is inside them, so
// that "this build can offer the Classic skin" is a check against real bytes on disk
// rather than a claim. Everything that draws lives in a provider in the browser.
package eraassets

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"rsb0t-go/internal/archive"
)

// Era ids, matching the stream client's skin ids (internal/controlpanel/streamskin.go).
// They are deliberately the same strings: a skin is offerable only when the provider
// declares it AND this package finds its assets, and two different vocabularies for the
// same four eras would make that join a translation table nobody maintains.
const (
	EraClassic = "classic"
	Era2009    = "2009"
)

// Known lists the eras this package can inspect, in a fixed order.
//
// 2011 is absent on purpose and this is the place to say why, because it will look like an
// omission otherwise: the owner dropped it (2026-09-20). The two repositories he originally
// named for it, 2011Scape/file-server and 2011Scape/rs-client, are both 404 — the project
// rewrote around GregHib/void and moved from revision 667 to 634 — and the 667 cache that
// remains has only 83.67% of its XTEA map keys recovered, so part of its map data cannot be
// decrypted at all. Adding it later means adding a case here; nothing else assumes two eras.
var Known = []string{EraClassic, Era2009}

// Info is what an era's asset root turned out to hold. Every field is measured from the
// files themselves when Open runs; none of it is configuration.
type Info struct {
	// Era is one of Known.
	Era string
	// Present is whether a usable asset set was found. False carries a Problem saying
	// what was looked for and what was there instead — an operator who pointed the flag
	// at the wrong directory must be able to see that from the panel, not guess.
	Present bool
	// Problem is why Present is false, in a whole sentence. Empty when Present.
	Problem string
	// Detail is one line about what was found, from the files: entry counts for Classic,
	// index entry counts for a JS5 filestore. Shown in the panel verbatim.
	Detail string
	// Bytes is the total size of the files that make up this era's asset set.
	Bytes int64
	// Counts are the individual measurements behind Detail, for tests and diagnostics —
	// e.g. "models36.jag" -> 829, or "idx7" -> 45468.
	Counts map[string]int
}

// Store is an opened era asset root: the directory an operator passed, with one
// subdirectory per era.
type Store struct {
	root string
	eras map[string]Info
}

// Open inspects dir, which is expected to hold one subdirectory per era (classic/, 2009/).
// A missing era is reported as not present rather than as an error: an operator with only
// the 1.35 MB Classic set and not the 86 MB 2009 one has a perfectly reasonable setup, and
// the panel should offer exactly what is there.
//
// An empty dir returns a nil Store, which every method below tolerates — that is the
// default build, with no -era-assets flag.
func Open(dir string) (*Store, error) {
	if strings.TrimSpace(dir) == "" {
		return nil, nil
	}
	info, err := os.Stat(dir)
	if err != nil {
		return nil, fmt.Errorf("era assets %s: %w", dir, err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("era assets %s is a file, not a directory holding one subdirectory per era (%v)", dir, Known)
	}
	s := &Store{root: dir, eras: make(map[string]Info, len(Known))}
	for _, era := range Known {
		s.eras[era] = inspect(era, filepath.Join(dir, era))
	}
	// A root with nothing recognisable in it is almost certainly the wrong directory, and
	// silently offering no skins would leave the operator with no way to tell. It is a
	// startup error for the same reason an unreadable provider manifest is.
	found := false
	for _, era := range Known {
		if s.eras[era].Present {
			found = true
		}
	}
	if !found {
		reasons := make([]string, 0, len(Known))
		for _, era := range Known {
			reasons = append(reasons, era+": "+s.eras[era].Problem)
		}
		return nil, fmt.Errorf("era assets %s holds no usable era asset set (%s)", dir, strings.Join(reasons, "; "))
	}
	return s, nil
}

// Root is the directory Open was given, or "" for a nil Store.
func (s *Store) Root() string {
	if s == nil {
		return ""
	}
	return s.root
}

// Era reports what was found for one era. A nil Store reports every era as absent, which
// is what a build with no -era-assets flag should say.
func (s *Store) Era(era string) Info {
	if s == nil {
		return Info{Era: era, Problem: "this build was started without -era-assets"}
	}
	if info, ok := s.eras[era]; ok {
		return info
	}
	return Info{Era: era, Problem: "not an era this build knows: " + strings.Join(Known, ", ")}
}

// ClassicCacheDir is the directory the Classic .jag archives were actually found in —
// <root>/classic/cache, or <root>/classic when an operator unpacked them one level up.
// Empty when the Classic set is not present, so a caller never has to guess which layout
// inspectClassic accepted.
func (s *Store) ClassicCacheDir() string {
	if s == nil || !s.Available(EraClassic) {
		return ""
	}
	era := filepath.Join(s.root, EraClassic)
	cache := filepath.Join(era, "cache")
	if _, err := os.Stat(filepath.Join(cache, classicArchives[0].name)); err == nil {
		return cache
	}
	return era
}

// ClassicArchive reads one named .jag out of the Classic cache directory. name must be a
// bare file name from classicArchives — a caller cannot reach outside the cache directory
// with it, which matters because the HTTP layer serves what this returns.
func (s *Store) ClassicArchive(name string) ([]byte, error) {
	known := false
	for _, a := range classicArchives {
		if a.name == name {
			known = true
		}
	}
	if !known {
		return nil, fmt.Errorf("%q is not one of the Classic cache archives this build reads", name)
	}
	dir := s.ClassicCacheDir()
	if dir == "" {
		return nil, fmt.Errorf("the Classic assets are not present: %s", s.Era(EraClassic).Problem)
	}
	return os.ReadFile(filepath.Join(dir, name))
}

// Available reports whether this era's assets are really on disk.
func (s *Store) Available(era string) bool { return s.Era(era).Present }

// classicArchives are the containers a Classic asset root must have, with the minimum
// entry count each must hold to be the real thing rather than a truncated download. The
// numbers are the mudclient204 cache's own, measured from 2003scape/rsc-c: models36.jag
// holds 829 models, entity24.jag 109 entity sprite sets, textures17.jag 53 textures,
// media58.jag 30 interface sprite sheets and config85.jag its two definition tables
// (string.dat and integer.dat). A file that parses but holds fewer entries than this is
// not a Classic cache and must not be offered as one.
var classicArchives = []struct {
	name     string
	minCount int
}{
	{"models36.jag", 829},
	{"entity24.jag", 109},
	{"textures17.jag", 53},
	{"media58.jag", 30},
	{"config85.jag", 2},
}

func inspect(era, dir string) Info {
	switch era {
	case EraClassic:
		return inspectClassic(dir)
	case Era2009:
		return inspect2009(dir)
	}
	return Info{Era: era, Problem: "no inspection is implemented for this era"}
}

// inspectClassic opens each required .jag with this repo's OWN 2004 JagFile decoder
// (internal/archive). That is not a coincidence or a shortcut: RuneScape Classic's .jag
// containers are the same format — a 3-byte unpacked size, a 3-byte packed size, and
// headerless bzip2 the reader restores a "BZh1" header onto, exactly the gotcha CLAUDE.md
// already documents for the 2004 cache. Measured: every archive in the mudclient204 cache
// parses with it unchanged, and every named media entry decompresses.
func inspectClassic(dir string) Info {
	out := Info{Era: EraClassic, Counts: map[string]int{}}
	cache := filepath.Join(dir, "cache")
	if _, err := os.Stat(cache); err != nil {
		// Tolerate the archives sitting directly in the era directory too: an operator
		// who unpacked them one level up has not made a mistake worth refusing over.
		if _, err2 := os.Stat(filepath.Join(dir, classicArchives[0].name)); err2 != nil {
			out.Problem = fmt.Sprintf("no %s: expected the mudclient204 .jag archives in %s or %s", classicArchives[0].name, cache, dir)
			return out
		}
		cache = dir
	}
	for _, want := range classicArchives {
		path := filepath.Join(cache, want.name)
		body, err := os.ReadFile(path)
		if err != nil {
			out.Problem = fmt.Sprintf("%s could not be read: %v", want.name, err)
			return out
		}
		jf, err := archive.ParseJagFile(body)
		if err != nil {
			out.Problem = fmt.Sprintf("%s is not a readable .jag container: %v", want.name, err)
			return out
		}
		if jf.Len() < want.minCount {
			out.Problem = fmt.Sprintf("%s holds %d entries, fewer than the %d a mudclient204 cache has — a truncated or wrong-revision file", want.name, jf.Len(), want.minCount)
			return out
		}
		out.Counts[want.name] = jf.Len()
		out.Bytes += int64(len(body))
	}
	out.Present = true
	out.Detail = fmt.Sprintf("mudclient204 .jag cache: %d models, %d entity sprite sets, %d textures, %d interface sheets",
		out.Counts["models36.jag"], out.Counts["entity24.jag"], out.Counts["textures17.jag"], out.Counts["media58.jag"])
	return out
}

// js5Indexes are the JS5 filestore indexes a 2009 (build 530) cache is checked on, with
// the entry count each holds in a genuine one. Each .idx entry is exactly 6 bytes, so the
// file's size divided by 6 IS the entry count — no parsing needed, and no way for a
// plausible-looking wrong-revision cache to pass by accident. The expected numbers are
// OpenRS2's own for cache id 254 (build 530, 2009-02-01): models 45,468, sprites 1,707,
// textures 680. A 2011 cache would answer 62,362 / 4,509 / 897 here and be rejected.
var js5Indexes = []struct {
	idx      int
	what     string
	expected int
}{
	{7, "models", 45468},
	{8, "sprites", 1707},
	{9, "textures", 680},
}

// tolerance is how far an index's entry count may sit from the reference build's before
// the cache is refused. It is not zero because a server project's own cache legitimately
// adds custom content: the 2009scape repo's copy measures 45,473 models against OpenRS2's
// 45,468. It is small because the point of the check is to catch a DIFFERENT REVISION,
// and the nearest ones differ by tens of thousands, not by five.
const tolerance = 512

func inspect2009(dir string) Info {
	out := Info{Era: Era2009, Counts: map[string]int{}}
	cache := filepath.Join(dir, "cache")
	if _, err := os.Stat(filepath.Join(cache, "main_file_cache.dat2")); err != nil {
		if _, err2 := os.Stat(filepath.Join(dir, "main_file_cache.dat2")); err2 != nil {
			out.Problem = fmt.Sprintf("no main_file_cache.dat2: expected a JS5 filestore in %s or %s", cache, dir)
			return out
		}
		cache = dir
	}
	dat, err := os.Stat(filepath.Join(cache, "main_file_cache.dat2"))
	if err != nil {
		out.Problem = fmt.Sprintf("main_file_cache.dat2 could not be read: %v", err)
		return out
	}
	out.Bytes = dat.Size()
	for _, want := range js5Indexes {
		name := fmt.Sprintf("main_file_cache.idx%d", want.idx)
		info, err := os.Stat(filepath.Join(cache, name))
		if err != nil {
			out.Problem = fmt.Sprintf("no %s (the %s index): %v", name, want.what, err)
			return out
		}
		if info.Size()%6 != 0 {
			out.Problem = fmt.Sprintf("%s is %d bytes, not a whole number of 6-byte JS5 index entries", name, info.Size())
			return out
		}
		count := int(info.Size() / 6)
		out.Counts[fmt.Sprintf("idx%d", want.idx)] = count
		out.Bytes += info.Size()
		if diff := count - want.expected; diff > tolerance || diff < -tolerance {
			out.Problem = fmt.Sprintf("%s holds %d %s, not the %d a build-530 cache has (within %d) — this looks like a different revision", name, count, want.what, want.expected, tolerance)
			return out
		}
	}
	out.Present = true
	out.Detail = fmt.Sprintf("build-530 JS5 filestore: %d model groups, %d sprite groups, %d textures",
		out.Counts["idx7"], out.Counts["idx8"], out.Counts["idx9"])
	return out
}

// AvailableEras lists the eras whose assets are really present, in Known's order.
func (s *Store) AvailableEras() []string {
	out := []string{}
	for _, era := range Known {
		if s.Available(era) {
			out = append(out, era)
		}
	}
	sort.Strings(out)
	return out
}
