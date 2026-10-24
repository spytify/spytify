// Command gencovers draws the ten album covers the fake player hands out
// (testdata/covers), so tests and screenshots carry art like a real player:
//
//	go run ./internal/tools/gencovers -out testdata/covers
package main

import (
	"flag"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"math"
	"os"
	"path/filepath"
)

const size = 400

type rgb struct{ r, g, b float64 }

func hex(v uint32) rgb {
	return rgb{float64(v >> 16 & 0xff), float64(v >> 8 & 0xff), float64(v & 0xff)}
}

func mix(a, b rgb, t float64) rgb {
	t = math.Max(0, math.Min(1, t))
	return rgb{a.r + (b.r-a.r)*t, a.g + (b.g-a.g)*t, a.b + (b.b-a.b)*t}
}

// edge is 1 inside a shape whose signed distance is d (negative inside),
// with a pixel of soft border.
func edge(d float64) float64 { return math.Max(0, math.Min(1, 0.5-d)) }

// A cover is a background gradient and a motif, both from x, y in [0, 1).
type cover struct {
	from, to, ink, accent rgb
	motif                 func(x, y float64, c cover) rgb
}

func covers() []cover {
	return []cover{
		{hex(0x2b1055), hex(0xff7e5f), hex(0xffd36e), hex(0x1b0b33), sunset},
		{hex(0x0f2027), hex(0x2c5364), hex(0x7fe7dc), hex(0xffffff), rings},
		{hex(0xf6d365), hex(0xfda085), hex(0x3a1c71), hex(0xffffff), bands},
		{hex(0x141e30), hex(0x243b55), hex(0xff5f6d), hex(0xffc371), dots},
		{hex(0x00c6ff), hex(0x0072ff), hex(0xffffff), hex(0x001f54), waves},
		{hex(0xffecd2), hex(0xfcb69f), hex(0x5b3256), hex(0x8e4e6f), mountains},
		{hex(0x11998e), hex(0x38ef7d), hex(0xfff9c4), hex(0x0b5345), burst},
		{hex(0x1f1c2c), hex(0x928dab), hex(0xf72585), hex(0x4cc9f0), circles},
		{hex(0x000000), hex(0x434343), hex(0x1ed65d), hex(0xb7ffcf), equalizer},
		{hex(0xe0eafc), hex(0xcfdef3), hex(0xe63946), hex(0x1d3557), grid},
	}
}

func sunset(x, y float64, c cover) rgb {
	bg := mix(c.from, c.to, y)
	sun := edge((math.Hypot(x-0.5, y-0.62)-0.22)*size) * edge((y-0.62)*size)
	px := mix(bg, c.ink, sun)
	if y > 0.62 {
		// Stripes over the sea, thinner towards the horizon.
		stripe := math.Mod((y-0.62)*40, 1)
		px = mix(mix(c.to, c.accent, (y-0.62)*2.2), px, 0.25*edge((stripe-0.3)*8))
	}
	return px
}

func rings(x, y float64, c cover) rgb {
	r := math.Hypot(x-0.5, y-0.5)
	band := math.Abs(math.Mod(r*14, 1)-0.5) - 0.18
	return mix(mix(c.from, c.to, r*1.4), c.ink, edge(band*size/14)*(1-r*1.2))
}

func bands(x, y float64, c cover) rgb {
	t := math.Mod((x+y)*5, 1)
	px := mix(c.from, c.to, (x+y)/2)
	if int((x+y)*5)%2 == 0 {
		px = mix(px, c.ink, edge((t-0.75)*size/5)*0.85)
	}
	return px
}

func dots(x, y float64, c cover) rgb {
	gx, gy := math.Mod(x*8, 1)-0.5, math.Mod(y*8, 1)-0.5
	rad := 0.12 + 0.3*x*y
	return mix(mix(c.from, c.to, y), mix(c.ink, c.accent, x), edge((math.Hypot(gx, gy)-rad)*size/8))
}

func waves(x, y float64, c cover) rgb {
	px := mix(c.from, c.to, y)
	for i := 0; i < 5; i++ {
		base := 0.3 + float64(i)*0.13
		w := base + 0.04*math.Sin(x*math.Pi*4+float64(i))
		if y > w {
			px = mix(px, mix(c.ink, c.accent, float64(i)/4), 0.35)
		}
	}
	return px
}

func mountains(x, y float64, c cover) rgb {
	px := mix(c.from, c.to, y)
	px = mix(px, c.to, edge((math.Hypot(x-0.72, y-0.28)-0.09)*size)*0.9)
	for i, peak := range []struct{ x, h float64 }{{0.25, 0.42}, {0.6, 0.5}, {0.9, 0.6}} {
		ridge := peak.h + math.Abs(x-peak.x)*1.3
		if y > ridge {
			px = mix(px, mix(c.ink, c.accent, float64(i)/2), edge((ridge-y)*size)*1)
		}
	}
	return px
}

func burst(x, y float64, c cover) rgb {
	a := math.Atan2(y-0.5, x-0.5)
	ray := math.Abs(math.Mod(a/(2*math.Pi)*24+24, 1)-0.5) - 0.25
	px := mix(c.from, c.to, math.Hypot(x-0.5, y-0.5)*1.6)
	px = mix(px, c.accent, edge(ray*12)*0.35)
	return mix(px, c.ink, edge((math.Hypot(x-0.5, y-0.5)-0.16)*size))
}

func circles(x, y float64, c cover) rgb {
	px := mix(c.from, c.to, y)
	a := edge((math.Hypot(x-0.4, y-0.45)-0.25)*size)
	b := edge((math.Hypot(x-0.6, y-0.55)-0.25)*size)
	px = mix(px, c.ink, a*0.85)
	px = mix(px, c.accent, b*0.6)
	return px
}

func equalizer(x, y float64, c cover) rgb {
	px := mix(c.from, c.to, y)
	bar := int(x * 12)
	height := 0.25 + 0.55*math.Abs(math.Sin(float64(bar)*1.7+0.4))
	inBar := math.Mod(x*12, 1) > 0.18 && math.Mod(x*12, 1) < 0.82
	if inBar && y > 1-height-0.08 && y < 0.92 {
		px = mix(c.ink, c.accent, (1-y)/height)
	}
	return px
}

func grid(x, y float64, c cover) rgb {
	px := mix(c.from, c.to, x)
	gx, gy := math.Mod(x*6, 1), math.Mod(y*6, 1)
	line := math.Min(math.Min(gx, 1-gx), math.Min(gy, 1-gy))
	px = mix(px, c.accent, edge((line-0.03)*size/6)*0.8)
	if int(x*6) == int(y*6) || int(x*6)+int(y*6) == 5 {
		px = mix(px, c.ink, 0.75*edge((0.08-line)*size/6))
	}
	return px
}

func main() {
	out := flag.String("out", "testdata/covers", "folder that receives cover-01.jpg to cover-10.jpg")
	flag.Parse()
	if err := os.MkdirAll(*out, 0o755); err != nil {
		fail(err)
	}
	for i, c := range covers() {
		img := image.NewRGBA(image.Rect(0, 0, size, size))
		for py := 0; py < size; py++ {
			for px := 0; px < size; px++ {
				v := c.motif(float64(px)/size, float64(py)/size, c)
				img.Set(px, py, color.RGBA{uint8(v.r), uint8(v.g), uint8(v.b), 0xff})
			}
		}
		f, err := os.Create(filepath.Join(*out, fmt.Sprintf("cover-%02d.jpg", i+1)))
		if err != nil {
			fail(err)
		}
		if err := jpeg.Encode(f, img, &jpeg.Options{Quality: 85}); err != nil {
			fail(err)
		}
		if err := f.Close(); err != nil {
			fail(err)
		}
	}
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gencovers:", err)
	os.Exit(1)
}
