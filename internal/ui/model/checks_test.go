package model

import (
	"testing"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

// ffmpeg that Windows' application control refused to run reads ffmpeg
// blocked, with what refused it and how to turn it off, not ffmpeg not
// found (#935). ffmpeg that could not be run otherwise still reads not
// found, with the recorder's reason.
func TestFFmpegWindowsRefusedReadsBlocked(t *testing.T) {
	blocked := proto.Check{ID: proto.CheckFFmpeg, Name: "ffmpeg found", Args: []string{"blocked"},
		Detail: `ffmpeg blocked by Windows' application control: C:\Spytify\ffmpeg.exe -version: fork/exec C:\Spytify\ffmpeg.exe: An Application Control policy has blocked this file.`}
	if got := CheckName(blocked); got != "ffmpeg blocked" {
		t.Errorf("name %q", got)
	}
	want := "Windows would not let ffmpeg.exe run: Smart App Control, or a policy set by whoever manages this computer, blocked it. " +
		"Smart App Control cannot allow a single app; it can only be turned off, in Windows Security > App & browser control > Smart App Control settings, " +
		"and only a reinstall of Windows turns it back on."
	if got := CheckDetail(blocked, "windows"); got != want {
		t.Errorf("detail %q, want %q", got, want)
	}

	missing := proto.Check{ID: proto.CheckFFmpeg, Name: "ffmpeg found", Detail: "ffmpeg not found; install it or set SPYTIFY_FFMPEG (ffmpeg)"}
	if got := CheckName(missing); got != lang.CheckNoFFmpeg {
		t.Errorf("not found: name %q", got)
	}
	if got := CheckDetail(missing, "windows"); got != missing.Detail {
		t.Errorf("not found: detail %q", got)
	}
}
