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

	"github.com/spytify/spytify/internal/pcm"
	"github.com/spytify/spytify/internal/proc"
)

// FFmpeg is a located ffmpeg binary.
type FFmpeg struct {
	Path    string
	Version string
}

// ErrMissing means ffmpeg could not be run; session.start is refused.
var ErrMissing = errors.New("ffmpeg not found; install it or set SPYTIFY_FFMPEG")

// wellKnown are tried when ffmpeg is not on PATH, as for an app started
// from a desktop launcher with a minimal PATH, or on Windows, where PATH
// rarely has it: first the copy shipped beside spytifyd, then where
// package managers and people put it.
func wellKnown(goos string, getenv func(string) string) []string {
	var c []string
	exe := "ffmpeg"
	if goos == "windows" {
		exe = "ffmpeg.exe"
	}
	if self, err := os.Executable(); err == nil {
		c = append(c, filepath.Join(filepath.Dir(self), exe))
	}
	if goos != "windows" {
		return append(c, "/opt/homebrew/bin/ffmpeg", "/usr/local/bin/ffmpeg", "/usr/bin/ffmpeg", "/snap/bin/ffmpeg")
	}
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

// Find resolves name (a path or a command on PATH) and reads its version.
func Find(name string) (*FFmpeg, error) {
	path, err := exec.LookPath(name)
	if err != nil && name == "ffmpeg" {
		for _, p := range wellKnown(runtime.GOOS, os.Getenv) {
			if _, serr := os.Stat(p); serr == nil {
				path, err = p, nil
				break
			}
		}
	}
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

// HasEncoder reports whether this ffmpeg build has an encoder.
func (f *FFmpeg) HasEncoder(name string) bool {
	encoders := proc.Command(f.Path, "-hide_banner", "-encoders")
	out, err := encoders.Output()
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(out), "\n") {
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[1] == name {
			return true
		}
	}
	return false
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

type loudness struct {
	InputI      string `json:"input_i"`
	InputTP     string `json:"input_tp"`
	InputLRA    string `json:"input_lra"`
	InputThresh string `json:"input_thresh"`
	Offset      string `json:"target_offset"`
}

const truePeak = "-1.5"

// fileRate is the rate a file of a source at rate is written at: the
// source's, up to 48 kHz. Above it, as an output device set to 88.2 or
// 96 kHz delivers, it is the rate the source's divides into: the music
// holds nothing above 22 kHz, MP3 has no higher rate, and the file is
// smaller.
func fileRate(rate int) int {
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

// Encode runs the job. Normalisation is ffmpeg's two-pass linear loudnorm,
// so it applies one gain to the whole track instead of compressing it.
func (f *FFmpeg) Encode(ctx context.Context, j Job) error {
	p, ok := presets[j.Format]
	if !ok {
		return fmt.Errorf("encode: unknown format %q", j.Format)
	}
	rate := fileRate(j.Source.Rate)
	var filters []string
	if t := j.trimFilter(); t != "" {
		filters = append(filters, t)
	}
	var ln *loudness
	if j.Normalize {
		var err error
		if ln, err = f.measure(ctx, j, filters); err != nil {
			return err
		}
		if ln != nil {
			filters = append(filters, fmt.Sprintf(
				"loudnorm=I=%s:TP=%s:LRA=11:measured_I=%s:measured_TP=%s:measured_LRA=%s:measured_thresh=%s:offset=%s:linear=true",
				num(j.TargetLUFS), truePeak, ln.InputI, ln.InputTP, ln.InputLRA, ln.InputThresh, ln.Offset))
		}
	}
	// loudnorm outputs 192 kHz when it cannot stay linear.
	if rate != j.Source.Rate || ln != nil {
		filters = append(filters, resample(rate))
	}

	args := []string{"-nostdin", "-hide_banner", "-loglevel", "error", "-y", "-i", j.In, "-map_metadata", "-1", "-vn"}
	if len(filters) > 0 {
		args = append(args, "-af", strings.Join(filters, ","))
	}
	args = append(args, "-ar", strconv.Itoa(rate))
	switch p.Format {
	case "mp3":
		args = append(args, "-c:a", p.Encoder, "-b:a", strconv.Itoa(j.BitrateKbps)+"k", "-write_xing", "1", "-id3v2_version", "0", "-write_id3v1", "0")
	case "aac":
		args = append(args, "-c:a", p.Encoder, "-b:a", strconv.Itoa(j.BitrateKbps)+"k", "-movflags", "+faststart")
	case "flac":
		if j.Source.Sample == pcm.F32LE {
			args = append(args, "-c:a", "flac", "-sample_fmt", "s32", "-bits_per_raw_sample", "24")
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
	args = append(args, "-f", p.Muxer, j.Out)
	return f.run(ctx, args)
}

func (f *FFmpeg) measure(ctx context.Context, j Job, filters []string) (*loudness, error) {
	af := append(append([]string{}, filters...), "loudnorm=I="+num(j.TargetLUFS)+":TP="+truePeak+":LRA=11:print_format=json")
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
	var ln loudness
	if err := json.Unmarshal([]byte(out[start:end+1]), &ln); err != nil {
		return nil, fmt.Errorf("encode: loudness report: %w", err)
	}
	if v, err := strconv.ParseFloat(ln.InputI, 64); err != nil || math.IsInf(v, 0) || v < -70 {
		// Silence or near silence: nothing to normalise.
		return nil, nil
	}
	return &ln, nil
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
