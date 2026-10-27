// Command genico makes the icon of the Windows app from the logo: an .ico
// with a PNG of each size Windows draws it at, which
// scripts/build-windows.sh puts in Spytify.exe:
//
//	go run ./internal/tools/genico -o spytify.ico assets/logo256px.png
package main

import (
	"bytes"
	"encoding/binary"
	"errors"
	"flag"
	"fmt"
	"image"
	"image/png"
	"os"
	"slices"

	"golang.org/x/image/draw"
)

// sizes are the sizes Windows asks an icon for: 16 to 64 for lists, menus
// and the taskbar at each display scale, 256 for Explorer's large views.
var sizes = []int{16, 20, 24, 32, 40, 48, 64, 256}

// dirEntry is an ICONDIRENTRY: where one image of the file is, and its size.
type dirEntry struct {
	Width, Height    uint8 // 0 stands for 256
	Colors, Reserved uint8
	Planes, Bits     uint16
	Size, Offset     uint32
}

func main() {
	out := flag.String("o", "spytify.ico", "output file")
	flag.Parse()
	if flag.NArg() != 1 {
		fail(errors.New("usage: genico -o spytify.ico logo.png"))
	}
	f, err := os.Open(flag.Arg(0))
	if err != nil {
		fail(err)
	}
	src, err := png.Decode(f)
	f.Close()
	if err != nil {
		fail(err)
	}
	ico, err := encode(src, sizes)
	if err != nil {
		fail(err)
	}
	if err := os.WriteFile(*out, ico, 0o644); err != nil {
		fail(err)
	}
}

// encode makes an .ico of src at each size, every image a PNG, which
// Windows reads since Vista. src is square and at least the largest size:
// it is only ever shrunk.
func encode(src image.Image, sizes []int) ([]byte, error) {
	b := src.Bounds()
	if b.Dx() != b.Dy() || b.Dx() < slices.Max(sizes) {
		return nil, fmt.Errorf("the logo is %dx%d: it must be square and at least %d pixels wide", b.Dx(), b.Dy(), slices.Max(sizes))
	}
	enc := png.Encoder{CompressionLevel: png.BestCompression}
	images := make([][]byte, len(sizes))
	for i, size := range sizes {
		img := src
		if size != b.Dx() {
			dst := image.NewRGBA(image.Rect(0, 0, size, size))
			draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
			img = dst
		}
		var buf bytes.Buffer
		if err := enc.Encode(&buf, img); err != nil {
			return nil, err
		}
		images[i] = buf.Bytes()
	}

	// The ICONDIR (reserved, 1 for an icon, the count), an entry per
	// image, then the images.
	var ico bytes.Buffer
	binary.Write(&ico, binary.LittleEndian, [3]uint16{0, 1, uint16(len(sizes))})
	offset := 6 + 16*len(sizes)
	for i, size := range sizes {
		side := uint8(size) // 256 wraps to 0, as the format wants
		binary.Write(&ico, binary.LittleEndian, dirEntry{
			Width: side, Height: side, Planes: 1, Bits: 32,
			Size: uint32(len(images[i])), Offset: uint32(offset),
		})
		offset += len(images[i])
	}
	for _, img := range images {
		ico.Write(img)
	}
	return ico.Bytes(), nil
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "genico:", err)
	os.Exit(1)
}
