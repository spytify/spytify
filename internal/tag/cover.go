package tag

import (
	"bytes"
	"image"
	"image/jpeg"
	"image/png"

	"golang.org/x/image/draw"
)

// coverSides is the longest side of each tags.cover_size, in pixels. Large
// keeps the cover as it came; medium matches the usual player artwork.
var coverSides = map[string]int{"small": 300, "medium": 640}

// fitCover shrinks a cover to the longest side its size allows; it never
// enlarges one, and keeps one it cannot decode as it came.
func fitCover(data []byte, mime, size string) ([]byte, string) {
	if size == "" {
		size = "medium"
	}
	side := coverSides[size]
	if side == 0 {
		return data, mime
	}
	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil || (cfg.Width <= side && cfg.Height <= side) {
		return data, mime
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return data, mime
	}
	w, h := side, (cfg.Height*side+cfg.Width/2)/cfg.Width
	if cfg.Height > cfg.Width {
		w, h = (cfg.Width*side+cfg.Height/2)/cfg.Height, side
	}
	dst := image.NewRGBA(image.Rect(0, 0, max(w, 1), max(h, 1)))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, src.Bounds(), draw.Src, nil)
	var out bytes.Buffer
	if format == "png" {
		err = png.Encode(&out, dst)
		mime = "image/png"
	} else {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 90})
		mime = "image/jpeg"
	}
	if err != nil {
		return data, mime
	}
	return out.Bytes(), mime
}
