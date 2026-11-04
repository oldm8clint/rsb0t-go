// Package spritepack decodes the real 2004-era "Pix" sprite format the game's own media
// archive (media.jag, GET /media<crc>, crc = index 4 of /crc0 — the same fetch pattern
// internal/cachecfg's caller uses for the config archive at index 2) stores its 2D UI
// graphics in: skill/stat icons, tab icons, minimap dots, and so on.
//
// Ported directly from this engine's own tools/pack/graphics/Pix.ts (unpackJag/packPng) —
// verified byte-for-byte against a real media.jag fetched from a running local engine during
// this package's own development, not guessed from a spec. The format, in short: a Jagfile
// (see internal/archive) holds one shared "index.dat" entry (per-sprite width/height/palette,
// then one 5-byte crop+pixelOrder header per frame) and one "<name>.dat" entry per named
// sprite family (raw palette-indexed pixel bytes for every frame in that family, back to
// back) — a sprite's first two bytes in its own .dat are the offset into index.dat where its
// header starts, which is how one shared index.dat serves every named sprite.
package spritepack

import (
	"fmt"
	"image"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/packet"
)

// Frame is one decoded sprite frame, already expanded to its full (uncropped) width/height —
// pixel index 0 is transparent in the source format, decoded here straight to alpha 0 so
// callers never need to know about crop boxes or palette indices.
type Frame struct {
	Width, Height int
	// RGBA is Width*Height*4 bytes, row-major, straight alpha — ready for image.NRGBA.
	RGBA []byte
}

// Image returns f as a standard library image, ready for png.Encode.
func (f Frame) Image() image.Image {
	return &image.NRGBA{Pix: f.RGBA, Stride: f.Width * 4, Rect: image.Rect(0, 0, f.Width, f.Height)}
}

// DecodeFamily decodes every frame of the named sprite family in jag, in storage order
// (frame 0, 1, 2, ...) — a "family" is what the original client calls one media.jag member;
// most UI graphics are a single frame, but grouped icon sheets like "staticons" store many
// same-sized frames back to back under one name, indexed 0.. by DecodeFamily's own return
// order, exactly the order internal/gamestate.SkillNames expects for the skill icon set.
func DecodeFamily(jag *archive.JagFile, name string) ([]Frame, error) {
	dat, err := jag.Read(name + ".dat")
	if err != nil {
		return nil, fmt.Errorf("spritepack: reading %s.dat: %w", name, err)
	}
	if dat == nil {
		return nil, fmt.Errorf("spritepack: no sprite family named %q in this archive", name)
	}
	idxData, err := jag.Read("index.dat")
	if err != nil {
		return nil, fmt.Errorf("spritepack: reading index.dat: %w", err)
	}
	if idxData == nil {
		return nil, fmt.Errorf("spritepack: this archive has no shared index.dat")
	}

	datR := packet.NewReader(dat)
	idxR := packet.NewReader(idxData)
	idxR.Pos = int(datR.G2())
	if idxR.Pos >= len(idxData) {
		return nil, fmt.Errorf("spritepack: %s: header offset %d is past the end of index.dat", name, idxR.Pos)
	}

	width := int(idxR.G2())
	height := int(idxR.G2())
	paletteCount := int(idxR.G1())
	palette := make([]uint32, paletteCount)
	for i := 0; i < paletteCount-1; i++ {
		palette[i+1] = uint32(idxR.G3())
	}

	var frames []Frame
	for frameIndex := 0; frameIndex < 1000; frameIndex++ {
		if idxR.Pos >= len(idxData) || datR.Pos >= len(dat) {
			break
		}
		cropLeft := int(idxR.G1())
		cropTop := int(idxR.G1())
		cropRight := int(idxR.G2())
		cropBottom := int(idxR.G2())
		pixelOrder := int(idxR.G1())
		if idxR.Pos > len(idxData) {
			break
		}
		n := cropRight * cropBottom
		if datR.Pos+n > len(dat) {
			break
		}
		indices := dat[datR.Pos : datR.Pos+n]
		datR.Pos += n

		rgba := make([]byte, width*height*4)
		putPixel := func(x, y int, idx byte) {
			if idx == 0 {
				return // stays fully transparent
			}
			rgb := palette[idx]
			pos := (y*width + x) * 4
			rgba[pos] = byte(rgb >> 16)
			rgba[pos+1] = byte(rgb >> 8)
			rgba[pos+2] = byte(rgb)
			rgba[pos+3] = 0xff
		}
		if pixelOrder == 0 {
			for i := 0; i < n; i++ {
				x := cropLeft + i%cropRight
				y := cropTop + i/cropRight
				putPixel(x, y, indices[i])
			}
		} else {
			for x := 0; x < cropRight; x++ {
				for y := 0; y < cropBottom; y++ {
					putPixel(cropLeft+x, cropTop+y, indices[y*cropRight+x])
				}
			}
		}
		frames = append(frames, Frame{Width: width, Height: height, RGBA: rgba})
		// No explicit frame count in this format (the original client's unpackJag has
		// none either) — the loop's own top-of-iteration bounds check is what stops it
		// cleanly once index.dat/the .dat file are exhausted.
	}
	return frames, nil
}
