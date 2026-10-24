// Command gate1 runs the plan's Gate 1 against a real player: ten tracks
// played in a row, and each recording made of its track's audio and nothing
// of its neighbours, checked by cross-correlation against the source.
//
//	go run ./internal/tools/gate1 export -dir ~/Gate1
//
// writes the ten items of testdata/timelines/jitter.json as tagged files.
// Add the folder to the player's local files, start a Spytify session with
// encode.format = wav (or flac), recorder.normalize = off and
// recorder.min_seconds = 1, turn the player's own volume normalisation off,
// and play the ten files in order. Then
//
//	go run ./internal/tools/gate1 check -dir ~/Gate1 -out ~/Music/Spytify
//
// compares each recording with its file and exits non-zero if one fails.
package main

import (
	"bytes"
	"errors"
	"flag"
	"fmt"
	"io/fs"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/spytify/spytify/internal/encode"
	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/internal/pcm"
)

func main() {
	if len(os.Args) < 2 {
		usage()
	}
	fl := flag.NewFlagSet(os.Args[1], flag.ExitOnError)
	dir := fl.String("dir", "", "folder of the exported tracks")
	timeline := fl.String("timeline", "testdata/timelines/jitter.json", "timeline whose items are exported")
	format := fl.String("format", "flac", "export format: flac, mp3, m4a or wav (a player that plays no FLAC takes mp3)")
	out := fl.String("out", "", "check: Spytify's output folder")
	_ = fl.Parse(os.Args[2:])
	if *dir == "" {
		usage()
	}
	ff, err := encode.Find(ffmpegName())
	if err != nil {
		fail(err)
	}
	switch os.Args[1] {
	case "export":
		tl, err := fake.Load(*timeline)
		if err != nil {
			fail(err)
		}
		if err := export(ff.Path, tl, *dir, *format); err != nil {
			fail(err)
		}
		fmt.Printf("wrote %d tracks to %s; play them in order, then run gate1 check\n", len(tl.Items), *dir)
	case "check":
		if *out == "" {
			usage()
		}
		results, err := check(ff.Path, *dir, *out)
		if err != nil {
			fail(err)
		}
		failed := 0
		for _, r := range results {
			fmt.Println(r)
			if !r.ok() {
				failed++
			}
		}
		if failed > 0 {
			fmt.Printf("Gate 1 failed: %d of %d tracks\n", failed, len(results))
			os.Exit(1)
		}
		fmt.Printf("Gate 1 passed: %d tracks, no neighbour's audio in any\n", len(results))
	default:
		usage()
	}
}

func usage() {
	fmt.Fprintln(os.Stderr, "usage: gate1 export -dir <folder> [-timeline <file>] [-format flac|mp3|m4a|wav]\n       gate1 check -dir <folder> -out <Spytify output folder>")
	os.Exit(2)
}

func fail(err error) {
	fmt.Fprintln(os.Stderr, "gate1:", err)
	os.Exit(1)
}

func ffmpegName() string {
	if n := os.Getenv("SPYTIFY_FFMPEG"); n != "" {
		return n
	}
	return "ffmpeg"
}

// export writes each item of tl as "NN Title.<format>", tagged so the
// player shows its title and artist.
func export(ffmpeg string, tl *fake.Timeline, dir, format string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	for i, it := range tl.Items {
		f, audio, err := tl.ItemAudio(i)
		if err != nil {
			return err
		}
		wav := filepath.Join(dir, fmt.Sprintf(".item-%02d.wav", i+1))
		w, err := pcm.CreateWAV(wav, f)
		if err != nil {
			return err
		}
		if _, err := w.Write(audio); err != nil {
			return err
		}
		if err := w.Close(); err != nil {
			return err
		}
		name := filepath.Join(dir, fmt.Sprintf("%02d %s.%s", i+1, it.Title, format))
		cmd := exec.Command(ffmpeg, "-v", "error", "-nostdin", "-y", "-i", wav,
			"-metadata", "title="+it.Title, "-metadata", "artist="+strings.Join(it.Artists, ", "), "-metadata", "album="+it.Album,
			"-metadata", fmt.Sprintf("track=%d", i+1), name)
		if out, err := cmd.CombinedOutput(); err != nil {
			return fmt.Errorf("ffmpeg %s: %v %s", name, err, out)
		}
		_ = os.Remove(wav)
	}
	return nil
}

type result struct {
	title string
	path  string
	err   error
	c     pcm.Comparison
	gain  float64 // dB applied to the recording to match the source's level
}

func (r result) ok() bool {
	return r.err == nil && r.c.Matches(fake.MaxLagMs, fake.MaxErrorDB)
}

func (r result) String() string {
	if r.err != nil {
		return fmt.Sprintf("FAIL %s: %v", r.title, r.err)
	}
	verdict := "ok  "
	if !r.ok() {
		verdict = "FAIL"
	}
	return fmt.Sprintf("%s %s: lag %.2f ms, residual %.1f dBFS, %+d frames, gain %+.2f dB (%s)",
		verdict, r.title, r.c.LagMs, r.c.ErrorDBFS, r.c.LengthDelta, r.gain, filepath.Base(r.path))
}

// check pairs every exported track with the recording whose name holds its
// title, and compares the two.
func check(ffmpeg, dir, out string) ([]result, error) {
	refs, err := filepath.Glob(filepath.Join(dir, "[0-9][0-9] *"))
	if err != nil || len(refs) == 0 {
		return nil, fmt.Errorf("no exported tracks in %s", dir)
	}
	recordings := map[string]string{}
	_ = filepath.WalkDir(out, func(path string, d fs.DirEntry, err error) error {
		if err == nil && !d.IsDir() {
			switch strings.ToLower(filepath.Ext(path)) {
			case ".wav", ".flac":
				recordings[path] = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
			}
		}
		return nil
	})
	var results []result
	for _, ref := range refs {
		base := strings.TrimSuffix(filepath.Base(ref), filepath.Ext(ref))
		title := strings.TrimSpace(base[2:])
		r := result{title: title}
		for path, name := range recordings {
			if strings.HasSuffix(name, title) {
				r.path = path
			}
		}
		if r.path == "" {
			r.err = errors.New("no WAV or FLAC recording of it in " + out)
		} else {
			r.c, r.gain, r.err = compare(ffmpeg, ref, r.path)
		}
		results = append(results, r)
	}
	return results, nil
}

// compare decodes both files, the source at the recording's rate and
// channels, matches the recording's level to the source's, and compares.
func compare(ffmpeg, ref, rec string) (pcm.Comparison, float64, error) {
	gotF, got, err := decode(ffmpeg, rec, pcm.Format{})
	if err != nil {
		return pcm.Comparison{}, 0, err
	}
	wantF, want, err := decode(ffmpeg, ref, gotF)
	if err != nil {
		return pcm.Comparison{}, 0, err
	}
	g := loud(wantF, want) / loud(gotF, got)
	if math.IsNaN(g) || math.IsInf(g, 0) || g == 0 {
		return pcm.Comparison{}, 0, errors.New("the recording is silent")
	}
	for i := 0; i < len(got)/gotF.Sample.Bytes(); i++ {
		gotF.PutSample(got, i, gotF.SampleAt(got, i)*g)
	}
	return pcm.Compare(wantF, want, gotF, got, fake.TrimDB, fake.SearchMs), 20 * math.Log10(g), nil
}

// decode reads a file as 32-bit float WAV through ffmpeg, at as's rate and
// channels when it is valid.
func decode(ffmpeg, path string, as pcm.Format) (pcm.Format, []byte, error) {
	args := []string{"-v", "error", "-nostdin", "-i", path}
	if as.Valid() {
		args = append(args, "-ar", fmt.Sprint(as.Rate), "-ac", fmt.Sprint(as.Channels))
	}
	args = append(args, "-c:a", "pcm_f32le", "-f", "wav", "-")
	var stdout, stderr bytes.Buffer
	cmd := exec.Command(ffmpeg, args...)
	cmd.Stdout, cmd.Stderr = &stdout, &stderr
	if err := cmd.Run(); err != nil {
		return pcm.Format{}, nil, fmt.Errorf("ffmpeg %s: %v %s", path, err, stderr.Bytes())
	}
	return pcm.DecodeWAV(&stdout)
}

// loud is the RMS of the samples above -60 dBFS, so leading and trailing
// silence of different lengths do not change it.
func loud(f pcm.Format, b []byte) float64 {
	var sum float64
	n := 0
	for i := 0; i < len(b)/f.Sample.Bytes(); i++ {
		if v := f.SampleAt(b, i); math.Abs(v) > 1e-3 {
			sum += v * v
			n++
		}
	}
	return math.Sqrt(sum / float64(n))
}
