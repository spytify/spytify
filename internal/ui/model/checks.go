package model

import (
	"fmt"
	"strings"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

// CheckRecorder is the window's own check, that the recorder answers: it
// runs the others.
const CheckRecorder proto.CheckID = "recorder"

// checkNames name each check when it passes, then when it fails, if that
// reads otherwise.
var checkNames = map[proto.CheckID][2]string{
	CheckRecorder:             {lang.RecorderRunning, lang.CheckRecorderNotRunning},
	proto.CheckTarget:         {lang.CheckTarget, lang.CheckNoTarget},
	proto.CheckPlayerRunning:  {lang.CheckPlayerRunning, lang.CheckPlayerNotRunning},
	proto.CheckFFmpeg:         {lang.CheckFFmpeg, lang.CheckNoFFmpeg},
	proto.CheckEncoder:        {lang.CheckEncoder},
	proto.CheckPlayerHeard:    {lang.CheckPlayerHeard, lang.CheckPlayerNotHeard},
	proto.CheckOutputWritable: {lang.CheckOutputWritable, lang.CheckOutputNotWritable},
	proto.CheckMacOS:          {lang.CheckMacOS},
	proto.CheckWindows:        {lang.CheckWindows},
	proto.CheckHelper:         {lang.CheckHelper},
	proto.CheckTapHelper:      {lang.CheckHelper},
	proto.CheckAppleScript:    {lang.CheckAppleScript},
	proto.CheckPowerShell:     {lang.CheckPowerShell},
	proto.CheckCapture:        {lang.CheckCapture},
	proto.CheckNoCapture:      {lang.CheckCapture},
	proto.CheckSessionBus:     {lang.CheckSessionBus},
	proto.CheckSampleRates:    {lang.CheckSampleRates},
	proto.CheckFakePlayer:     {lang.CheckFakePlayer},
}

// CheckName is a check's name as the window words it, from its ID: a
// failed ffmpeg found reads ffmpeg not found. A check the window does not
// know, from a recorder newer or older than it, keeps the recorder's name.
func CheckName(c proto.Check) string {
	names, ok := checkNames[c.ID]
	if !ok {
		return c.Name
	}
	name := names[0]
	if !c.OK && names[1] != "" {
		name = names[1]
	}
	if c.ID == proto.CheckEncoder {
		name = fmt.Sprintf(name, arg(c, 0))
	}
	return name
}

// CheckDetail is a check's detail as the window words it on goos, from its
// ID and Args. A detail that only holds a fact, as a path, a version or an
// error, is the recorder's, as is that of a check the window does not
// know. The player heard check fails with the only silence warning's words.
func CheckDetail(c proto.Check, goos string) string {
	switch c.ID {
	case proto.CheckTarget:
		return fmt.Sprintf(lang.TargetProcess, arg(c, 0))
	case proto.CheckPlayerRunning:
		if c.OK {
			return fmt.Sprintf(lang.PID, arg(c, 0))
		}
		return lang.NotRunning
	case proto.CheckEncoder:
		return fmt.Sprintf(lang.ForFormat, arg(c, 1))
	case proto.CheckPlayerHeard:
		if c.OK {
			return lang.NoSilenceHeard
		}
		return WarningText(proto.ErrorEvent{Code: proto.ErrorCaptureSilent}, goos)
	case proto.CheckMacOS:
		return fmt.Sprintf(lang.MacOSVersion, arg(c, 0))
	case proto.CheckWindows:
		return fmt.Sprintf(lang.WindowsBuild, arg(c, 0))
	case proto.CheckTapHelper:
		if c.OK {
			return fmt.Sprintf(lang.HelperMacOS, arg(c, 0))
		}
	case proto.CheckAppleScript:
		if c.OK {
			return lang.AppleScriptAsks
		}
	case proto.CheckPowerShell:
		if c.OK {
			return fmt.Sprintf(lang.PowerShellReads, arg(c, 0))
		}
	case proto.CheckCapture:
		switch {
		case !c.OK:
			return lang.InstallCapture
		case arg(c, 0) == "pulseaudio":
			return lang.CapturePulseAudio
		}
		return lang.CapturePipeWire
	case proto.CheckNoCapture:
		return fmt.Sprintf(lang.NoCaptureBackend, arg(c, 0))
	case proto.CheckSampleRates:
		rates := strings.Join(c.Args, ", ")
		switch {
		case rates == "":
			return lang.RatesUnknown
		case c.OK:
			return fmt.Sprintf(lang.Rates, rates)
		}
		return fmt.Sprintf(lang.RatesOnly, rates)
	}
	return c.Detail
}

// arg is the check's i-th fact, empty when it has fewer.
func arg(c proto.Check, i int) string {
	if i < len(c.Args) {
		return c.Args[i]
	}
	return ""
}
