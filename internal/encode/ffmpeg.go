// Package encode runs ffmpeg on a closed track: trim, loudness
// normalisation and encoding to the output format. It is the only place
// spytify shells out to ffmpeg for audio.
package encode

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/proc"
)

// FFmpeg is a located ffmpeg binary.
type FFmpeg struct {
	Path    string
	Version string

	// encoders is the build's encoder list once read: core.info asks at
	// every connection, and the list never changes (#845).
	mu       sync.Mutex
	encoders map[string]bool
}

// ErrMissing means ffmpeg could not be run; session.start is refused.
var ErrMissing = errors.New("ffmpeg not found; install it or set SPYTIFY_FFMPEG")

// besideProgram is the copy the macOS app and the Windows zip ship, in
// the folder of spytifyd and the app.
func besideProgram(goos string) string {
	self, err := os.Executable()
	if err != nil {
		return ""
	}
	if goos == "windows" {
		return filepath.Join(filepath.Dir(self), "ffmpeg.exe")
	}
	return filepath.Join(filepath.Dir(self), "ffmpeg")
}

// wellKnown are tried when ffmpeg is neither beside the program nor on
// PATH, as for an app started from a desktop launcher with a minimal PATH,
// or on Windows, where PATH rarely has it: where package managers and
// people put it.
func wellKnown(goos string, getenv func(string) string) []string {
	if goos != "windows" {
		return []string{"/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/usr/bin/ffmpeg", "/snap/bin/ffmpeg"}
	}
	var c []string
	exe := "ffmpeg.exe"
	join := func(env string, parts ...string) {
		if base := getenv(env); base != "" {
			c = append(c, filepath.Join(append([]string{base}, parts...)...))
		}
	}
	join("LOCALAPPDATA", "Microsoft", "WinGet", "Links", exe)
	if base := getenv("LOCALAPPDATA"); base != "" {
		// winget's Gyan.FFmpeg packages, whose link PATH only sees after a
		// new sign-in.
		found, _ := filepath.Glob(filepath.Join(base, "Microsoft", "WinGet", "Packages", "Gyan.FFmpeg*", "*", "bin", exe))
		c = append(c, found...)
	}
	join("ProgramData", "chocolatey", "bin", exe)
	join("USERPROFILE", "scoop", "shims", exe)
	join("ProgramFiles", "ffmpeg", "bin", exe)
	join("SystemDrive", string(filepath.Separator)+"ffmpeg", "bin", exe)
	return c
}

// Find resolves name and reads its version. A name other than "ffmpeg",
// from SPYTIFY_FFMPEG, is taken as given: a path, or a command on PATH.
// "ffmpeg" is the copy beside the program first, so a program of that
// name earlier on PATH cannot stand in for it, then PATH, then wellKnown.
func Find(name string) (*FFmpeg, error) {
	path, err := locate(name)
	if err != nil {
		return nil, fmt.Errorf("%w (%s)", ErrMissing, name)
	}
	probe := proc.Command(path, "-hide_banner", "-version")
	out, err := probe.Output()
	if err != nil {
		return nil, fmt.Errorf("%w: %s -version: %v", ErrMissing, path, err)
	}
	version := strings.SplitN(string(out), "\n", 2)[0]
	return &FFmpeg{Path: path, Version: strings.TrimSpace(version)}, nil
}

func locate(name string) (string, error) {
	if name != "ffmpeg" {
		return exec.LookPath(name)
	}
	if p := besideProgram(runtime.GOOS); p != "" {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	for _, p := range wellKnown(runtime.GOOS, os.Getenv) {
		if _, err := os.Stat(p); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// HasEncoder reports whether this ffmpeg build has an encoder. The list is
// read once; a run that fails is tried again at the next call.
func (f *FFmpeg) HasEncoder(name string) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.encoders == nil {
		out, err := proc.Command(f.Path, "-hide_banner", "-encoders").Output()
		if err != nil {
			return false
		}
		f.encoders = map[string]bool{}
		for _, line := range strings.Split(string(out), "\n") {
			if fields := strings.Fields(line); len(fields) >= 2 {
				f.encoders[fields[1]] = true
			}
		}
	}
	return f.encoders[name]
}

// Preset is the container and codec of an output format.
type Preset struct {
	Format    string
	Extension string
	Muxer     string
	Encoder   string
	Lossy     bool
}

var presets = map[string]Preset{
	"mp3":  {Format: "mp3", Extension: "mp3", Muxer: "mp3", Encoder: "libmp3lame", Lossy: true},
	"aac":  {Format: "aac", Extension: "m4a", Muxer: "ipod", Encoder: "aac", Lossy: true},
	"flac": {Format: "flac", Extension: "flac", Muxer: "flac", Encoder: "flac"},
	"wav":  {Format: "wav", Extension: "wav", Muxer: "wav"},
}

// PresetFor returns the preset of encode.format.
func PresetFor(format string) (Preset, bool) {
	p, ok := presets[format]
	return p, ok
}

// Job is one encode: a temp WAV in, the final format out.
type Job struct {
	In          string
	Out         string
	Format      string
	BitrateKbps int
	Source      pcm.Format
	// TrimStart and TrimEnd select frames [TrimStart, TrimEnd) of the
	// input; TrimEnd 0 means to the end.
	TrimStart  int64
	TrimEnd    int64
	Normalize  bool
	TargetLUFS float64
	// Tags, when set, are written in the same run as the audio, as
	// tag.Writer writes them into a file already encoded: MP3 gets ID3v2.3,
	// FLAC Vorbis comments, AAC MP4 atoms. WAV takes none.
	Tags *Tags
}

// Tags are a file's tags, as tag.Embedded gives them.
type Tags struct {
	// Metadata are ffmpeg's -metadata name=value pairs.
	Metadata []string
	// Cover is a picture to attach, of type CoverMIME; nil for none.
	Cover     []byte
	CoverMIME string
}

func (j Job) trimFilter() string {
	if j.TrimStart <= 0 && j.TrimEnd <= 0 {
		return ""
	}
	f := "atrim=start_sample=" + strconv.FormatInt(j.TrimStart, 10)
	if j.TrimEnd > 0 {
		f += ":end_sample=" + strconv.FormatInt(j.TrimEnd, 10)
	}
	return f + ",asetpts=PTS-STARTPTS"
}

// loudness is a track's integrated loudness, in LUFS, and true peak, in
// dBTP.
type loudness struct {
	I, TP float64
}

// TruePeak is the true peak, in dBTP, that Normalize raises a track to at
// most.
const TruePeak = -1.5

// Shortfall is a track whose peaks kept Normalize from raising it to its
// target: its loudness as measured, the gain it got and the target.
type Shortfall struct {
	InputLUFS, GainDB, TargetLUFS float64
}

// Under is how far under its target the track ends, in LU.
func (s Shortfall) Under() float64 { return s.TargetLUFS - s.InputLUFS - s.GainDB }

// reachedLU is how far under its target, in LU, a track still counts as
// raised to it.
const reachedLU = 0.1

// FileRate is the rate a file of a source at rate is written at: the
// source's, up to 48 kHz. Above it, as an output device set to 88.2 or
// 96 kHz delivers, it is the rate the source's divides into: the music
// holds nothing above 22 kHz, MP3 has no higher rate, and the file is
// smaller.
func FileRate(rate int) int {
	switch {
	case rate <= 48000:
		return rate
	case rate%44100 == 0:
		return 44100
	}
	return 48000
}

// resample converts to rate with a longer filter than ffmpeg's default,
// which rolls off from 20 kHz: this one stays flat up to 22 kHz.
func resample(rate int) string {
	return "aresample=" + strconv.Itoa(rate) + ":filter_size=256:phase_shift=20:cutoff=0.99"
}

// Encode runs the job. Normalisation measures the track with loudnorm,
// then applies one gain to the whole of it (see loudness.gain), so it is
// never compressed. The gain is not loudnorm's own: even with linear=true,
// loudnorm compresses and limits a track whose loudness range is over its
// LRA, or whose true peak the gain would take over its TP. A track its
// peaks keep under the target comes back as a Shortfall, nil otherwise.
func (f *FFmpeg) Encode(ctx context.Context, j Job) (*Shortfall, error) {
	p, ok := presets[j.Format]
	if !ok {
		return nil, fmt.Errorf("encode: unknown format %q", j.Format)
	}
	rate := FileRate(j.Source.Rate)
	var filters []string
	if t := j.trimFilter(); t != "" {
		filters = append(filters, t)
	}
	var short *Shortfall
	if j.Normalize {
		ln, err := f.measure(ctx, j, filters)
		if err != nil {
			return nil, err
		}
		if ln != nil {
			g := ln.gain(j.TargetLUFS)
			if g != 0 {
				filters = append(filters, "volume="+num(g)+"dB")
			}
			if s := (Shortfall{InputLUFS: ln.I, GainDB: g, TargetLUFS: j.TargetLUFS}); s.Under() > reachedLU {
				short = &s
			}
		}
	}
	if rate != j.Source.Rate {
		filters = append(filters, resample(rate))
	}

	tags := j.Tags
	if p.Format == "wav" {
		tags = nil
	}
	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", j.In}
	if tags != nil && tags.Cover != nil {
		cover, err := coverFile(filepath.Dir(j.In), tags.Cover, tags.CoverMIME)
		if err != nil {
			return nil, err
		}
		defer os.Remove(cover)
		args = append(args, "-i", cover, "-map_metadata", "-1", "-map", "0:a", "-map", "1:0", "-c:v", "copy", "-disposition:v:0", "attached_pic")
		if p.Format != "aac" {
			args = append(args, "-metadata:s:v", "title=Album cover", "-metadata:s:v", "comment=Cover (front)")
		}
	} else {
		args = append(args, "-map_metadata", "-1", "-vn")
	}
	if len(filters) > 0 {
		args = append(args, "-af", strings.Join(filters, ","))
	}
	args = append(args, "-ar", strconv.Itoa(rate))
	switch p.Format {
	case "mp3":
		// ID3v2.3, whose cover Windows' Explorer and Media Player show,
		// when tagged; no tag otherwise.
		id3 := "0"
		if tags != nil {
			id3 = "3"
		}
		args = append(args, "-c:a", p.Encoder, "-b:a", strconv.Itoa(j.BitrateKbps)+"k", "-write_xing", "1", "-id3v2_version", id3, "-write_id3v1", "0")
	case "aac":
		args = append(args, "-c:a", p.Encoder, "-b:a", strconv.Itoa(j.BitrateKbps)+"k", "-movflags", "+faststart")
	case "flac":
		if j.Source.Sample == pcm.F32LE {
			args = append(args, "-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample:a", "24")
		} else {
			args = append(args, "-c:a", "flac", "-sample_fmt", "s16")
		}
	case "wav":
		if j.Source.Sample == pcm.F32LE {
			args = append(args, "-c:a", "pcm_f32le")
		} else {
			args = append(args, "-c:a", "pcm_s16le")
		}
	}
	if tags != nil {
		for _, kv := range tags.Metadata {
			args = append(args, "-metadata", kv)
		}
	}
	args = append(args, "-f", p.Muxer, j.Out)
	if err := f.run(ctx, args); err != nil {
		return nil, err
	}
	return short, nil
}

// coverFile writes a cover of type mime to a temporary file in dir, for
// ffmpeg to read as its second input.
func coverFile(dir string, data []byte, mime string) (string, error) {
	ext := ".jpg"
	if mime == "image/png" {
		ext = ".png"
	}
	f, err := os.CreateTemp(dir, ".cover-*"+ext)
	if err != nil {
		return "", err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		os.Remove(f.Name())
		return "", err
	}
	if err := f.Close(); err != nil {
		os.Remove(f.Name())
		return "", err
	}
	return f.Name(), nil
}

func (f *FFmpeg) measure(ctx context.Context, j Job, filters []string) (*loudness, error) {
	af := append(append([]string{}, filters...), "loudnorm=I="+num(j.TargetLUFS)+":TP="+num(TruePeak)+":LRA=11:print_format=json")
	cmd := proc.CommandContext(ctx, f.Path, "-nostdin", "-hide_banner", "-i", j.In, "-af", strings.Join(af, ","), "-f", "null", "-")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("encode: loudness pass: %v: %s", err, tail(stderr.String()))
	}
	out := stderr.String()
	start := strings.LastIndex(out, "{")
	end := strings.LastIndex(out, "}")
	if start < 0 || end < start {
		return nil, fmt.Errorf("encode: no loudness report in ffmpeg output")
	}
	var report struct {
		I  string `json:"input_i"`
		TP string `json:"input_tp"`
	}
	if err := json.Unmarshal([]byte(out[start:end+1]), &report); err != nil {
		return nil, fmt.Errorf("encode: loudness report: %w", err)
	}
	i, err := strconv.ParseFloat(report.I, 64)
	if err != nil || math.IsInf(i, 0) || i < -70 {
		// Silence or near silence: nothing to normalise.
		return nil, nil
	}
	tp, err := strconv.ParseFloat(report.TP, 64)
	if err != nil {
		return nil, fmt.Errorf("encode: loudness report: true peak %q", report.TP)
	}
	return &loudness{I: i, TP: tp}, nil
}

// gain is the one gain, in dB, that Normalize applies to a track measured
// as ln. A track louder than target is lowered to it, which lowers its
// peaks too. A quieter one is raised to it as far as its true peak stays
// at TruePeak or under, and is never lowered for its peaks: a track whose
// peaks are loud for its loudness stays below the target.
func (ln loudness) gain(target float64) float64 {
	g := target - ln.I
	if g <= 0 {
		return g
	}
	return max(0, min(g, TruePeak-ln.TP))
}

func (f *FFmpeg) run(ctx context.Context, args []string) error {
	cmd := proc.CommandContext(ctx, f.Path, args...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("ffmpeg: %v: %s", err, tail(stderr.String()))
	}
	return nil
}

func num(v float64) string { return strconv.FormatFloat(v, 'f', -1, 64) }

func tail(s string) string {
	s = strings.TrimSpace(s)
	if len(s) > 600 {
		return "..." + s[len(s)-600:]
	}
	return s
}
