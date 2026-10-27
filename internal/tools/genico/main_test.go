package main

import (
	"bytes"
	"encoding/binary"
	"image"
	"image/png"
	"os"
	"slices"
	"testing"
)

func TestIconHasEverySizeAsAPNG(t *testing.T) {
	f, err := os.Open("../../../assets/logo256px.png")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	logo, err := png.Decode(f)
	if err != nil {
		t.Fatal(err)
	}
	ico, err := encode(logo, sizes)
	if err != nil {
		t.Fatal(err)
	}

	r := bytes.NewReader(ico)
	var dir [3]uint16
	if err := binary.Read(r, binary.LittleEndian, &dir); err != nil {
		t.Fatal(err)
	}
	if dir != [3]uint16{0, 1, uint16(len(sizes))} {
		t.Fatalf("ICONDIR = %v, want reserved 0, type 1 (icon), %d images", dir, len(sizes))
	}
	entries := make([]dirEntry, dir[2])
	if err := binary.Read(r, binary.LittleEndian, entries); err != nil {
		t.Fatal(err)
	}
	var got []int
	for _, e := range entries {
		side := int(e.Width)
		if side == 0 {
			side = 256
		}
		got = append(got, side)
		if e.Height != e.Width || e.Planes != 1 || e.Bits != 32 {
			t.Errorf("%d px: entry %+v, want a square, 1 plane, 32 bits", side, e)
		}
		end := int(e.Offset) + int(e.Size)
		if int(e.Offset) < 6+16*len(entries) || end > len(ico) {
			t.Errorf("%d px: image at %d..%d, outside the file's %d bytes after the directory", side, e.Offset, end, len(ico))
			continue
		}
		data := ico[e.Offset:end]
		if !bytes.HasPrefix(data, []byte("\x89PNG\r\n\x1a\n")) {
			t.Errorf("%d px: the image is not a PNG", side)
			continue
		}
		img, err := png.Decode(bytes.NewReader(data))
		if err != nil {
			t.Errorf("%d px: %v", side, err)
			continue
		}
		if b := img.Bounds(); b.Dx() != side || b.Dy() != side {
			t.Errorf("%d px: the PNG is %dx%d", side, b.Dx(), b.Dy())
		}
		// The logo is a disc: its corners stay transparent at every size.
		if _, _, _, a := img.At(0, 0).RGBA(); a > 0x4000 {
			t.Errorf("%d px: corner alpha %#x, want transparent", side, a)
		}
	}
	if !slices.Equal(got, sizes) {
		t.Errorf("sizes %v, want %v", got, sizes)
	}
}

func TestEncodeRefusesALogoItWouldStretch(t *testing.T) {
	for _, r := range []image.Rectangle{image.Rect(0, 0, 128, 128), image.Rect(0, 0, 300, 256)} {
		if _, err := encode(image.NewNRGBA(r), sizes); err == nil {
			t.Errorf("a %dx%d logo was accepted", r.Dx(), r.Dy())
		}
	}
}
