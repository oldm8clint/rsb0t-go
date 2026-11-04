package eraassets

import (
	"fmt"
	"image"
	"image/color"

	"rsb0t-go/internal/archive"
)

// RuneScape Classic's sprite format, ported from the reference decoder rather than
// inferred from the bytes.
//
// PROVENANCE, because this repo's rule is that a format is ported from the code that
// really reads it and never guessed at: this is a port of `parseSprite` in
// 2003scape/rsc-sprites `src/sprite-archive.js` (AGPL-3.0), which is itself a
// transliteration of mudclient's own sprite loader, together with that project's
// `src/media-sprites.js` for how many frames each media entry holds. Only the decoding
// rules are ported; none of that project's code is vendored.
//
// The format, in one place so the code below reads as a checklist:
//
//	the sprite file (<name>.dat) starts with a u16: an OFFSET into index.dat, where this
//	sprite's metadata begins. Everything after those two bytes is pixel data.
//
//	index.dat, from that offset:
//	  u16 fullWidth, u16 fullHeight     the plane every frame is placed on
//	  u8  paletteCount                  entry 0 is always transparent (0xff00ff)
//	  (paletteCount-1) x RGB triples    the palette
//	  then per frame:
//	    u8  offsetX, u8 offsetY         where the frame sits on that plane
//	    u16 width,  u16 height          the frame's own size
//	    u8  indexOrder                  0 = row-major, otherwise column-major
//
//	the pixel data is width*height palette indices, read in that order. Palette entry 0
//	(magenta, 0xff00ff) is transparent and is left unwritten.
//
// What this is FOR. It is what makes "the Classic skin is available" mean something
// checkable: a build can decode the operator's Classic archives into real images and say
// how big they are, rather than trusting that the files are what their names claim.

// classicMedia58 is how many frames each media58.jag entry holds. The counts are the
// reference decoder's own table; a wrong count would read another sprite's metadata as
// this one's and produce plausible-looking nonsense, which is exactly the failure mode
// this table exists to prevent.
var classicMedia58 = []struct {
	name   string
	frames int
}{
	{"inv1", 1}, {"inv2", 6}, {"bubble", 1}, {"runescape", 1}, {"splat", 3},
	{"icon", 8}, {"hbar", 1}, {"hbar2", 1}, {"compass", 1}, {"buttons", 2},
	{"scrollbar", 2}, {"corners", 4}, {"arrows", 2},
}

// ClassicSprite is one decoded frame, with the plane it belongs on. Offset/plane are kept
// rather than baked in because Classic's own interface code positions frames by them, and
// a caller drawing the game's chrome needs the same numbers.
type ClassicSprite struct {
	Name        string
	Frame       int
	Width       int // the frame's own pixels
	Height      int
	PlaneWidth  int // the fullWidth/fullHeight plane the frame sits on
	PlaneHeight int
	OffsetX     int
	OffsetY     int
	Image       *image.NRGBA // the frame at its own size; transparent where the palette said so
}

// reader is a minimal big-endian byte cursor. The repo's internal/packet reader is for the
// 2004 wire protocol and carries ISAAC/bit-access machinery that has nothing to do with a
// Classic cache file, so this stays local and obvious.
type reader struct {
	data []byte
	pos  int
}

func (r *reader) u8() (int, error) {
	if r.pos >= len(r.data) {
		return 0, fmt.Errorf("read past the end of a %d-byte buffer", len(r.data))
	}
	v := int(r.data[r.pos])
	r.pos++
	return v, nil
}

func (r *reader) u16() (int, error) {
	hi, err := r.u8()
	if err != nil {
		return 0, err
	}
	lo, err := r.u8()
	if err != nil {
		return 0, err
	}
	return hi<<8 | lo, nil
}

// DecodeClassicMedia decodes every interface sprite sheet in a Classic media58.jag.
//
// It returns one ClassicSprite per FRAME, not per entry: "inv2" is six frames and each is
// a separate image, which is how the game uses them.
func DecodeClassicMedia(media58 []byte) ([]ClassicSprite, error) {
	jf, err := archive.ParseJagFile(media58)
	if err != nil {
		return nil, fmt.Errorf("media58.jag: %w", err)
	}
	index, err := jf.Read("index.dat")
	if err != nil {
		return nil, fmt.Errorf("media58.jag index.dat: %w", err)
	}
	if len(index) == 0 {
		return nil, fmt.Errorf("media58.jag has no index.dat, so no sprite in it can be placed or coloured")
	}
	out := make([]ClassicSprite, 0, 32)
	for _, entry := range classicMedia58 {
		body, err := jf.Read(entry.name + ".dat")
		if err != nil {
			return nil, fmt.Errorf("media58.jag %s.dat: %w", entry.name, err)
		}
		if len(body) == 0 {
			return nil, fmt.Errorf("media58.jag has no %s.dat", entry.name)
		}
		frames, err := decodeClassicSprite(entry.name, body, index, entry.frames)
		if err != nil {
			return nil, err
		}
		out = append(out, frames...)
	}
	return out, nil
}

// decodeClassicSprite is the port of parseSprite described at the top of this file.
func decodeClassicSprite(name string, spriteData, indexData []byte, frameCount int) ([]ClassicSprite, error) {
	sprite := &reader{data: spriteData}
	start, err := sprite.u16()
	if err != nil {
		return nil, fmt.Errorf("%s.dat: %w", name, err)
	}
	if start < 0 || start > len(indexData) {
		return nil, fmt.Errorf("%s.dat points at offset %d in a %d-byte index.dat", name, start, len(indexData))
	}
	idx := &reader{data: indexData, pos: start}

	planeWidth, err := idx.u16()
	if err != nil {
		return nil, fmt.Errorf("%s.dat plane width: %w", name, err)
	}
	planeHeight, err := idx.u16()
	if err != nil {
		return nil, fmt.Errorf("%s.dat plane height: %w", name, err)
	}
	paletteCount, err := idx.u8()
	if err != nil {
		return nil, fmt.Errorf("%s.dat palette size: %w", name, err)
	}
	if paletteCount == 0 {
		return nil, fmt.Errorf("%s.dat declares an empty palette", name)
	}
	// Entry 0 is magenta and means transparent; the file stores the other N-1 as RGB.
	palette := make([]uint32, paletteCount)
	palette[0] = 0xff00ff
	for i := 1; i < paletteCount; i++ {
		r, err := idx.u8()
		if err != nil {
			return nil, fmt.Errorf("%s.dat palette entry %d: %w", name, i, err)
		}
		g, err := idx.u8()
		if err != nil {
			return nil, fmt.Errorf("%s.dat palette entry %d: %w", name, i, err)
		}
		b, err := idx.u8()
		if err != nil {
			return nil, fmt.Errorf("%s.dat palette entry %d: %w", name, i, err)
		}
		palette[i] = uint32(r)<<16 | uint32(g)<<8 | uint32(b)
	}

	out := make([]ClassicSprite, 0, frameCount)
	for frame := 0; frame < frameCount; frame++ {
		offsetX, err := idx.u8()
		if err != nil {
			return nil, fmt.Errorf("%s.dat frame %d offset: %w", name, frame, err)
		}
		offsetY, err := idx.u8()
		if err != nil {
			return nil, fmt.Errorf("%s.dat frame %d offset: %w", name, frame, err)
		}
		width, err := idx.u16()
		if err != nil {
			return nil, fmt.Errorf("%s.dat frame %d size: %w", name, frame, err)
		}
		height, err := idx.u16()
		if err != nil {
			return nil, fmt.Errorf("%s.dat frame %d size: %w", name, frame, err)
		}
		order, err := idx.u8()
		if err != nil {
			return nil, fmt.Errorf("%s.dat frame %d pixel order: %w", name, frame, err)
		}
		if width <= 0 || height <= 0 {
			return nil, fmt.Errorf("%s.dat frame %d is %dx%d", name, frame, width, height)
		}
		if remaining := len(spriteData) - sprite.pos; remaining < width*height {
			return nil, fmt.Errorf("%s.dat frame %d wants %d pixels but only %d bytes remain — a truncated or misread sprite", name, frame, width*height, remaining)
		}
		indices := make([]uint8, width*height)
		if order == 0 {
			for i := range indices {
				v, err := sprite.u8()
				if err != nil {
					return nil, fmt.Errorf("%s.dat frame %d pixels: %w", name, frame, err)
				}
				indices[i] = uint8(v)
			}
		} else {
			for x := 0; x < width; x++ {
				for y := 0; y < height; y++ {
					v, err := sprite.u8()
					if err != nil {
						return nil, fmt.Errorf("%s.dat frame %d pixels: %w", name, frame, err)
					}
					indices[x+y*width] = uint8(v)
				}
			}
		}
		img := image.NewNRGBA(image.Rect(0, 0, width, height))
		for y := 0; y < height; y++ {
			for x := 0; x < width; x++ {
				entry := int(indices[x+y*width])
				if entry >= len(palette) {
					return nil, fmt.Errorf("%s.dat frame %d uses palette entry %d of %d", name, frame, entry, len(palette))
				}
				rgb := palette[entry]
				if rgb == 0xff00ff {
					continue // transparent, left as the zero NRGBA pixel
				}
				img.SetNRGBA(x, y, color.NRGBA{R: uint8(rgb >> 16), G: uint8(rgb >> 8), B: uint8(rgb), A: 255})
			}
		}
		out = append(out, ClassicSprite{
			Name: name, Frame: frame, Width: width, Height: height,
			PlaneWidth: planeWidth, PlaneHeight: planeHeight, OffsetX: offsetX, OffsetY: offsetY, Image: img,
		})
	}
	return out, nil
}
