package ui

import (
	"fmt"
	"testing"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/model"
)

// A file's size counts megabytes as the bitrate's note in Settings does, a
// million bytes each: a minute at a bitrate weighs on its row what the note
// promised.
func TestSizesCountMegabytesAsTheBitrateNote(t *testing.T) {
	def, ok := settings.Lookup(settings.EncodeBitrateKbps.String())
	if !ok {
		t.Fatal("no bitrate setting")
	}
	for i, kbps := range def.Ints {
		// Under a megabyte the row counts kilobytes: 128 kbps is "960 kB".
		minute := kbps * 1000 / 8 * 60
		if minute < 1e6 {
			continue
		}
		if want, got := fmt.Sprintf("%d kbps · ~ %s/min", kbps, fmtBytes(minute)), model.SettingWords(*def).Choices[i]; got != want {
			t.Errorf("the note says %q, a minute's file %q", got, fmtBytes(minute))
		}
	}
	for n, want := range map[int64]string{999: "999 B", 1000: "1 kB", 31_000_000: "31.0 MB", 1_500_000_000: "1.5 GB"} {
		if got := fmtBytes(n); got != want {
			t.Errorf("fmtBytes(%d) = %q, want %q", n, got, want)
		}
	}
}
