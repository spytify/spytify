package model

import (
	"fmt"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

// warning is how the lexicon words one of the recorder's warnings: what it
// found, then what to do about it on each system, none when the same
// everywhere.
type warning struct {
	found  string
	advice map[string]string
}

// warnings are the recorder's warnings the lexicon words, by their code.
var warnings = map[string]warning{
	"capture_stalled":     {lang.NoAudio, perSystem(lang.NoAudioMacOS, lang.NoAudioWindows, lang.NoAudioLinux)},
	"capture_silent":      {lang.OnlySilence, perSystem(lang.OnlySilenceMacOS, lang.OnlySilenceWindows, lang.OnlySilenceLinux)},
	"capture_low_quality": {lang.PhoneCall, perSystem(lang.PhoneCallMacOS, lang.PhoneCallWindows, lang.PhoneCallLinux)},
	"disk_full":           {found: lang.DiskFull},
}

func perSystem(macOS, windows, linux string) map[string]string {
	return map[string]string{"darwin": macOS, "windows": windows, "linux": linux}
}

// WarningText is a recorder warning as the window words it on goos: the
// lexicon's words for its code, with the facts the event carries. A code
// the lexicon does not word, or an event without the facts its words need,
// as from a recorder older than the window, keeps the recorder's message.
// Systems other than macOS and Windows get Linux's advice.
func WarningText(e proto.ErrorEvent, goos string) string {
	w, ok := warnings[e.Code]
	if !ok {
		return e.Msg
	}
	found := w.found
	switch e.Code {
	case "capture_low_quality":
		if e.Source == nil {
			return e.Msg
		}
		rate := lang.DeviceRate
		if e.Source.Channels < 2 {
			rate = lang.DeviceRateMono
		}
		found = fmt.Sprintf(found, fmt.Sprintf(rate, float64(e.Source.SampleRate)/1000))
	case "disk_full":
		if e.FreeMB == nil {
			return e.Msg
		}
		found = fmt.Sprintf(found, *e.FreeMB)
	}
	if w.advice == nil {
		return found
	}
	advice, ok := w.advice[goos]
	if !ok {
		advice = w.advice["linux"]
	}
	return found + " " + advice
}

// CheckDetail is a check's detail as the window words it: the recorder's,
// but for the player heard check, which fails with the only silence
// warning's words.
func CheckDetail(c proto.Check, goos string) string {
	if c.Name == proto.CheckPlayerHeard && !c.OK {
		return WarningText(proto.ErrorEvent{Code: "capture_silent"}, goos)
	}
	return c.Detail
}
