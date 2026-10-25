// Command mkicon generates the downscaled basket icon embedded by
// internal/fbchart (a ~96px copy of branding/presyo-bantay-icon.png; the
// 780 KB original is never embedded). Run once:
//
//	go run ./tools/mkicon
package main

import (
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"os"

	"golang.org/x/image/draw"
)

const (
	srcPath = "../branding/presyo-bantay-icon.png"
	outPath = "internal/fbchart/icon.png"
	size    = 96
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "mkicon:", err)
		os.Exit(1)
	}
}

func run() error {
	f, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer f.Close()
	src, _, err := image.Decode(f)
	if err != nil {
		return err
	}
	dst := image.NewRGBA(image.Rect(0, 0, size, size))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Over, nil)
	out, err := os.Create(outPath)
	if err != nil {
		return err
	}
	defer out.Close()
	if err := png.Encode(out, dst); err != nil {
		return err
	}
	fmt.Println(outPath)
	return nil
}
