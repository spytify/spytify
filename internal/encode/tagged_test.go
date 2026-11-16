package encode

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/proc"
	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/tag"
	"github.com/spytify/spytify/internal/testkit"
)

// A file tagged in its encode run carries what tag.Writer wrote into it
// in a second run: the same tags, the same cover with its stream's title
// and comment, MP3's ID3v2.3 header, and the same audio, normalized alike.
func TestTagsWrittenWithTheAudioAreTheTagWritersOwn(t *testing.T) {
	ff := testkit.Need(t, "ffmpeg", Find)
	ffprobe := testkit.Need(t, "ffprobe", exec.LookPath)
	ctx := context.Background()
	dir := t.TempDir()
	f := pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.F32LE}
	in := testkit.WriteWAV(t, filepath.Join(dir, "in.wav"), f, pcm.Synth{Seed: 4}.Render(f, 44100))
	track := tag.Track{
		Title: "Ça plane", Subtitle: "Live", SubtitleSep: tag.SepParenthesis,
		Performers: []string{"Zoë Ångström", "Guest"}, AlbumArtists: []string{"Zoë Ångström"},
		Album: "Présence d'esprit", Disc: 1, Track: 7, Year: 1977,
		Cover: coverJPEG(t, 800, 600), CoverMIME: "image/jpeg",
	}
	opts := tag.Options{ExtraToSubtitle: true, ArtistSeparator: " & ", Cover: true, CoverSize: settings.CoverSizeSmall}
	for _, format := range []string{"mp3", "aac", "flac"} {
		for _, normalize := range []bool{false, true} {
			job := Job{In: in, Format: format, BitrateKbps: 192, Source: f, TrimStart: 441, TrimEnd: 40000, Normalize: normalize, TargetLUFS: -14}
			name := format
			if normalize {
				name += " normalized"
			}
			t.Run(name, func(t *testing.T) {
				before := job
				before.Out = filepath.Join(dir, name+" before.part")
				if _, err := ff.Encode(ctx, before); err != nil {
					t.Fatal(err)
				}
				if err := (tag.Writer{FFmpeg: ff.Path}).Write(ctx, before.Out, format, track, opts); err != nil {
					t.Fatal(err)
				}
				now := job
				now.Out = filepath.Join(dir, name+" now.part")
				pairs, cover, mime := tag.Embedded(track, format, opts)
				now.Tags = &Tags{Metadata: pairs, Cover: cover, CoverMIME: mime}
				if _, err := ff.Encode(ctx, now); err != nil {
					t.Fatal(err)
				}

				if a, b := metadataOf(t, ff, before.Out), metadataOf(t, ff, now.Out); a != b || !strings.Contains(b, "album=Présence d'esprit") {
					t.Errorf("tags:\n%s\nwant the tag writer's:\n%s", b, a)
				}
				if a, b := streamsOf(t, ffprobe, before.Out), streamsOf(t, ffprobe, now.Out); a != b || !strings.Contains(b, "attached_pic=1") {
					t.Errorf("streams:\n%s\nwant the tag writer's:\n%s", b, a)
				}
				a, err := ff.Cover(ctx, before.Out)
				if err != nil {
					t.Fatal(err)
				}
				if b, err := ff.Cover(ctx, now.Out); err != nil || !bytes.Equal(a, b) {
					t.Errorf("cover of %d bytes (%v), want the tag writer's %d bytes", len(b), err, len(a))
				}
				if _, a := testkit.Decode(t, ff.Path, before.Out, pcm.F32LE); true {
					if _, b := testkit.Decode(t, ff.Path, now.Out, pcm.F32LE); !bytes.Equal(a, b) {
						t.Errorf("audio of %d bytes differs from the tag writer's %d", len(b), len(a))
					}
				}
				if format == "mp3" {
					head, err := os.ReadFile(now.Out)
					if err != nil {
						t.Fatal(err)
					}
					if !bytes.HasPrefix(head, []byte("ID3")) || head[3] != 3 {
						t.Errorf("tag header % x, want ID3v2.3", head[:min(len(head), 5)])
					}
				}
			})
		}
	}
	// WAV takes no tags: its sidecar is tag.Writer's.
	wav := Job{In: in, Out: filepath.Join(dir, "wav.part"), Format: "wav", Source: f, Tags: &Tags{Metadata: []string{"title=Song"}, Cover: track.Cover, CoverMIME: "image/jpeg"}}
	if _, err := ff.Encode(ctx, wav); err != nil {
		t.Fatal(err)
	}
	if m := metadataOf(t, ff, wav.Out); strings.Contains(m, "title=Song") || strings.Contains(m, "[STREAM]") {
		t.Errorf("a WAV with tags or a cover:\n%s", m)
	}
}

// streamsOf is each of path's streams: its codec, format, the cover's
// size, whether it is an attached picture, and its tags, the cover's title
// and comment among them. The encoder an MP3's LAME header names is left
// out: it names the library that wrote the frames, Lavc, where a stream
// copy wrote Lavf.
func streamsOf(t *testing.T, ffprobe, path string) string {
	t.Helper()
	out, err := proc.Command(ffprobe, "-v", "error", "-show_entries", "stream=codec_name,sample_fmt,sample_rate,bits_per_raw_sample,width,height:stream_disposition=attached_pic:stream_tags", "-of", "compact", path).Output()
	if err != nil {
		t.Fatal(err)
	}
	return lameEncoder.ReplaceAllString(string(out), "")
}

var lameEncoder = regexp.MustCompile(`\|tag:encoder=Lav[cf][^|\n]*`)

// metadataOf is the file's tags as ffmpeg reads them.
func metadataOf(t *testing.T, ff *FFmpeg, path string) string {
	t.Helper()
	out, err := proc.Command(ff.Path, "-v", "error", "-nostdin", "-i", path, "-f", "ffmetadata", "-").Output()
	if err != nil {
		t.Fatal(err)
	}
	return string(out)
}

func coverJPEG(t *testing.T, w, h int) []byte {
	t.Helper()
	img := image.NewRGBA(image.Rect(0, 0, w, h))
	for y := 0; y < h; y++ {
		for x := 0; x < w; x++ {
			img.Set(x, y, color.RGBA{uint8(x), uint8(y), 128, 255})
		}
	}
	var b bytes.Buffer
	if err := jpeg.Encode(&b, img, nil); err != nil {
		t.Fatal(err)
	}
	return b.Bytes()
}
