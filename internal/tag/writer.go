package tag

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/spytify/spytify/internal/proc"
)

// Writer writes tags into an encoded file. MP3 gets ID3v2.4, FLAC gets
// Vorbis comments, AAC gets MP4 atoms, all through an ffmpeg stream copy so
// the audio is untouched; WAV has no standard tag block and gets a sidecar
// JSON file instead.
type Writer struct {
	FFmpeg string
}

// Write tags path (an encoded file of format) in place.
func (w Writer) Write(ctx context.Context, path, format string, t Track, o Options) error {
	if format == "wav" {
		return writeSidecar(path, t)
	}
	muxer := map[string]string{"mp3": "mp3", "flac": "flac", "aac": "ipod"}[format]
	if muxer == "" {
		return fmt.Errorf("tag: unknown format %q", format)
	}
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, ".tagging-*")
	if err != nil {
		return err
	}
	tmp.Close()
	defer os.Remove(tmp.Name())

	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", path}
	var coverPath string
	if o.Cover && len(t.Cover) > 0 {
		cover, mime := fitCover(t.Cover, t.CoverMIME, o.CoverSize)
		ext := ".jpg"
		if mime == "image/png" {
			ext = ".png"
		}
		cf, err := os.CreateTemp(dir, ".cover-*"+ext)
		if err != nil {
			return err
		}
		coverPath = cf.Name()
		defer os.Remove(coverPath)
		if _, err := cf.Write(cover); err != nil {
			cf.Close()
			return err
		}
		cf.Close()
		args = append(args, "-i", coverPath, "-map", "0:a", "-map", "1:0", "-c", "copy", "-disposition:v:0", "attached_pic")
		if format != "aac" {
			args = append(args, "-metadata:s:v", "title=Album cover", "-metadata:s:v", "comment=Cover (front)")
		}
	} else {
		args = append(args, "-map", "0:a", "-c", "copy")
	}
	args = append(args, "-map_metadata", "-1")
	if format == "mp3" {
		args = append(args, "-id3v2_version", "4", "-write_id3v1", "0")
	}
	for _, kv := range metadataPairs(t, format, o.ArtistSeparator) {
		args = append(args, "-metadata", kv)
	}
	args = append(args, "-f", muxer, tmp.Name())
	out, err := proc.CommandContext(ctx, w.FFmpeg, args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("tag: ffmpeg: %v: %s", err, strings.TrimSpace(string(out)))
	}
	return os.Rename(tmp.Name(), path)
}

func metadataPairs(t Track, format, sep string) []string {
	if sep == "" {
		sep = ", "
	}
	var kv []string
	add := func(k, v string) {
		if strings.TrimSpace(v) != "" {
			kv = append(kv, k+"="+v)
		}
	}
	title := t.Title
	if format == "aac" {
		// MP4 has no subtitle tag: the extra part stays in the title.
		join := t.SubtitleSep
		if join == SepNone {
			join = SepDash
		}
		title = JoinTitle(t.Title, t.Subtitle, join)
	}
	add("title", title)
	add("artist", strings.Join(t.Performers, sep))
	add("album_artist", strings.Join(t.AlbumArtists, sep))
	add("album", t.Album)
	if t.Track > 0 {
		add("track", strconv.Itoa(t.Track))
	}
	if t.Disc > 0 {
		add("disc", strconv.Itoa(t.Disc))
	}
	if t.Year > 0 {
		add("date", strconv.Itoa(t.Year))
	}
	switch format {
	case "mp3":
		add("TIT3", t.Subtitle)
	case "flac":
		add("SUBTITLE", t.Subtitle)
	}
	return kv
}

func writeSidecar(path string, t Track) error {
	b, err := json.MarshalIndent(t, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(path+".json", append(b, '\n'), 0o644)
}
