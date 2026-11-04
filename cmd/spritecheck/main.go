// Command spritecheck is a throwaway verification tool for internal/spritepack: load a saved
// media.jag and report every frame's size for a named sprite family, plus optionally save each
// frame as a PNG so it can be eyeballed. Not part of the product; used once during
// internal/spritepack's own development to confirm the decode against a real archive fetched
// from a running local engine before wiring it into cmd/controlpanel.
package main

import (
	"flag"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"log"
	"os"

	"rsb0t-go/internal/archive"
	"rsb0t-go/internal/spritepack"
)

func main() {
	path := flag.String("media", "", "path to a saved media.jag")
	name := flag.String("name", "staticons", "sprite family name")
	saveDir := flag.String("save", "", "directory to save each frame as name_N.png (optional)")
	contact := flag.String("contact", "", "save one upscaled, labelled contact-sheet PNG here (optional)")
	scale := flag.Int("scale", 6, "nearest-neighbour upscale factor for -contact")
	flag.Parse()

	data, err := os.ReadFile(*path)
	if err != nil {
		log.Fatal(err)
	}
	jag, err := archive.ParseJagFile(data)
	if err != nil {
		log.Fatal(err)
	}
	frames, err := spritepack.DecodeFamily(jag, *name)
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("%s: %d frames\n", *name, len(frames))
	for i, f := range frames {
		fmt.Printf("  frame %d: %dx%d\n", i, f.Width, f.Height)
		if *saveDir != "" {
			if err := os.MkdirAll(*saveDir, 0o755); err != nil {
				log.Fatal(err)
			}
			out, err := os.Create(fmt.Sprintf("%s/%s_%d.png", *saveDir, *name, i))
			if err != nil {
				log.Fatal(err)
			}
			if err := png.Encode(out, f.Image()); err != nil {
				log.Fatal(err)
			}
			out.Close()
		}
	}

	if *contact != "" && len(frames) > 0 {
		cols := 6
		rows := (len(frames) + cols - 1) / cols
		cell := frames[0].Width * *scale
		labelH := 14
		sheet := image.NewRGBA(image.Rect(0, 0, cols*cell, rows*(cell+labelH)))
		draw.Draw(sheet, sheet.Bounds(), image.NewUniform(image.Black), image.Point{}, draw.Src)
		for i, f := range frames {
			col := i % cols
			row := i / cols
			ox := col * cell
			oy := row * (cell + labelH)
			src := f.Image()
			for y := 0; y < f.Height; y++ {
				for x := 0; x < f.Width; x++ {
					c := src.At(x, y)
					for sy := 0; sy < *scale; sy++ {
						for sx := 0; sx < *scale; sx++ {
							sheet.Set(ox+x*(*scale)+sx, oy+y*(*scale)+sy, c)
						}
					}
				}
			}
			// crude index label: a strip of white ticks equal to i, in the label band.
			for t := 0; t <= i; t++ {
				for ly := 0; ly < labelH-4; ly++ {
					sheet.Set(ox+2+t*3, oy+cell+2+ly, image.White)
				}
			}
		}
		out, err := os.Create(*contact)
		if err != nil {
			log.Fatal(err)
		}
		defer out.Close()
		if err := png.Encode(out, sheet); err != nil {
			log.Fatal(err)
		}
		fmt.Println("wrote contact sheet:", *contact)
	}
}
