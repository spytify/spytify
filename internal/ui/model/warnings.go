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
var warnings = map[proto.ErrorCode]warning{
	proto.ErrorCaptureStalled:    {lang.NoAudio, perSystem(lang.NoAudioMacOS, lang.NoAudioWindows, lang.NoAudioLinux)},
	proto.ErrorCaptureSilent:     {lang.OnlySilence, perSystem(lang.OnlySilenceMacOS, lang.OnlySilenceWindows, lang.OnlySilenceLinux)},
	proto.ErrorCaptureLowQuality: {lang.PhoneCall, perSystem(lang.PhoneCallMacOS, lang.PhoneCallWindows, lang.PhoneCallLinux)},
	proto.ErrorDiskFull:          {found: lang.DiskFull},
	proto.ErrorPlayerVolumeLow:   {found: lang.VolumeLow},
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
	case proto.ErrorCaptureLowQuality:
		if e.Source == nil {
			return e.Msg
		}
		rate := lang.DeviceRate
		if e.Source.Channels < 2 {
			rate = lang.DeviceRateMono
		}
		found = fmt.Sprintf(found, fmt.Sprintf(rate, float64(e.Source.SampleRate)/1000))
	case proto.ErrorDiskFull:
		if e.FreeMB == nil {
			return e.Msg
		}
		found = fmt.Sprintf(found, *e.FreeMB)
	case proto.ErrorPlayerVolumeLow:
		switch {
		case e.Volume == nil:
			return e.Msg
		case *e.Volume == 0:
			found = lang.VolumeZero
		default:
			found = fmt.Sprintf(found, *e.Volume)
		}
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
