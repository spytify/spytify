//go:build linux

package capture

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/pcm"
)

// The end of pw-record's help in PipeWire 1.0.5 (Ubuntu 24.04) and 1.4.0.
const (
	pwRecordHelp10 = `      --format                          Sample format s16 (req. for rec) (default s16)
      --volume                          Stream volume 0-1.0 (default 1.000)
  -q  --quality                         Resampler quality (0 - 15) (default 4)`
	pwRecordHelp14 = pwRecordHelp10 + `
  -a, --raw                             RAW mode`
)

// fakePWRecord puts on PATH a pw-record that prints help for --help.
func fakePWRecord(t *testing.T, help string) {
	dir := t.TempDir()
	script := "#!/bin/sh\n[ \"$1\" = --help ] && printf '%s\\n' '" + help + "'\n"
	if err := os.WriteFile(filepath.Join(dir, "pw-record"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", dir)
}

// pw-record before PipeWire 1.4 exits at once on --raw, an unknown option
// there, so nothing was ever recorded (#790); "-" alone writes raw samples.
// From 1.4, "-" alone writes a sound file, so --raw is needed.
func TestPWRecordGetsRawOnlyWhenItTakesIt(t *testing.T) {
	for _, c := range []struct {
		name string
		help string
		raw  bool
	}{
		{"pipewire-1.0", pwRecordHelp10, false},
		{"pipewire-1.4", pwRecordHelp14, true},
	} {
		t.Run(c.name, func(t *testing.T) {
			fakePWRecord(t, c.help)
			n := streamNode{key: "64257"}
			f := pcm.Format{Rate: 48000, Channels: 2, Sample: pcm.F32LE}
			args := pipewireTools{}.record(context.Background(), n, f).Args
			if got := slices.Contains(args, "--raw"); got != c.raw {
				t.Errorf("--raw passed: %v, want %v: %s", got, c.raw, strings.Join(args, " "))
			}
			if args[len(args)-1] != "-" {
				t.Errorf("records to %q, want stdout: %s", args[len(args)-1], strings.Join(args, " "))
			}
		})
	}
}
