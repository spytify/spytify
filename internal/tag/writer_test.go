package tag

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/testkit"
)

// AAC files have no subtitle tag: the extra part stays in the title there,
// as the player showed it, instead of being lost.
func TestAACKeepsTheExtraInItsTitle(t *testing.T) {
	for _, c := range []struct {
		title, aac, base, subtitle string
	}{
		{"Song (Live)", "Song (Live)", "Song", "Live"},
		{"Song (feat. Lumen) (Live)", "Song (feat. Lumen) (Live)", "Song", "feat. Lumen · Live"},
		{"Song - Remastered 2011", "Song - Remastered 2011", "Song", "Remastered 2011"},
		{"Song", "Song", "Song", ""},
	} {
		tr := Track{Title: c.title, Performers: []string{"Artist"}}.withSubtitle(Options{ExtraToSubtitle: true})
		if got := metadataPairs(tr, "aac", ""); !slices.Contains(got, "title="+c.aac) || slices.ContainsFunc(got, func(kv string) bool { return kv == "title="+c.base && c.base != c.aac }) {
			t.Errorf("%q: aac tags %v", c.title, got)
		}
		mp3 := metadataPairs(tr, "mp3", "")
		if !slices.Contains(mp3, "title="+c.base) {
			t.Errorf("%q: mp3 tags %v", c.title, mp3)
		}
		if c.subtitle != "" && !slices.Contains(mp3, "TIT3="+c.subtitle) {
			t.Errorf("%q: mp3 subtitle missing from %v", c.title, mp3)
		}
	}
	// A subtitle a provider gave, with no separator known, joins with a dash
	// as in file names.
	tr := Track{Title: "Song", Subtitle: "Live"}
	if got := metadataPairs(tr, "aac", ""); !slices.Contains(got, "title=Song - Live") {
		t.Errorf("provider subtitle: aac tags %v", got)
	}
}

// Windows' Explorer and Media Player show the cover of an ID3v2.3 tag but
// not of an ID3v2.4 one, so MP3s are tagged in v2.3. Accents, the year and
// the cover make it through the older version.
func TestMP3TagsAreID3v23ForWindows(t *testing.T) {
	ffmpeg := testkit.Need(t, "ffmpeg", exec.LookPath)
	ffprobe := testkit.Need(t, "ffprobe", exec.LookPath)
	dir := t.TempDir()
	f := pcm.Format{Rate: 44100, Channels: 2, Sample: pcm.S16LE}
	in := testkit.WriteWAV(t, filepath.Join(dir, "in.wav"), f, pcm.Synth{Seed: 1}.Render(f, 22050))
	out := filepath.Join(dir, "out.mp3")
	if b, err := exec.Command(ffmpeg, "-loglevel", "error", "-i", in, "-c:a", "libmp3lame", out).CombinedOutput(); err != nil {
		t.Skipf("cannot encode mp3: %v %s", err, b)
	}
	cover := jpegCover(t, 300, 300)
	track := Track{Title: "Le matin", Performers: []string{"Artiste"}, Album: "Présence d'esprit", Year: 2009, Cover: cover, CoverMIME: "image/jpeg"}
	if err := (Writer{FFmpeg: ffmpeg}).Write(context.Background(), out, "mp3", track, Options{Cover: true}); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(out)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.HasPrefix(data, []byte("ID3")) || data[3] != 3 {
		t.Fatalf("tag header % x, want ID3v2.3", data[:min(len(data), 5)])
	}
	if !bytes.Contains(data, []byte("APIC")) || !bytes.Contains(data, cover) {
		t.Error("the cover is not in an APIC frame")
	}
	probe, err := exec.Command(ffprobe, "-v", "error", "-show_entries", "format_tags=album,date", "-of", "default=nw=1", out).Output()
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"TAG:album=Présence d'esprit", "TAG:date=2009"} {
		if !strings.Contains(string(probe), want) {
			t.Errorf("tags %q, want %s", probe, want)
		}
	}
}
