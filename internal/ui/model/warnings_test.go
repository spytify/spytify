package model

import (
	"fmt"
	"os"
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/ui/lang"
	"github.com/spytify/spytify/pkg/proto"
)

// The warnings read as testdata/warnings.golden pins them on each system:
// for the recorder's, the words it wrote itself before the lexicon had them.
func TestPinnedWarnings(t *testing.T) {
	call := &proto.AudioSource{SampleRate: 16000, Channels: 1}
	f8 := &proto.AudioSource{SampleRate: 8000, Channels: 2}
	var b strings.Builder
	for _, goos := range []string{"darwin", "windows", "linux"} {
		fmt.Fprintf(&b, "%s stall: %s\n", goos, WarningText(proto.ErrorEvent{Code: proto.ErrorCaptureStalled}, goos))
		fmt.Fprintf(&b, "%s silent: %s\n", goos, WarningText(proto.ErrorEvent{Code: proto.ErrorCaptureSilent}, goos))
		fmt.Fprintf(&b, "%s call: %s\n", goos, WarningText(proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality, Source: call}, goos))
		fmt.Fprintf(&b, "%s 8 kHz: %s\n", goos, WarningText(proto.ErrorEvent{Code: proto.ErrorCaptureLowQuality, Source: f8}, goos))
	}
	free := int64(80)
	fmt.Fprintf(&b, "disk full: %s\n", WarningText(proto.ErrorEvent{Code: proto.ErrorDiskFull, FreeMB: &free}, "linux"))
	for _, pct := range []int{40, 0} {
		fmt.Fprintf(&b, "volume %d%%: %s\n", pct, WarningText(proto.ErrorEvent{Code: proto.ErrorPlayerVolumeLow, Volume: &pct}, "darwin"))
	}
	golden, err := os.ReadFile("testdata/warnings.golden")
	if err != nil {
		t.Fatal(err)
	}
	// Git may check text out with CRLF on Windows.
	want := strings.ReplaceAll(string(golden), "\r\n", "\n")
	if got := b.String(); got != want {
		t.Errorf("warnings:\n%s\nwant, from testdata/warnings.golden:\n%s", got, want)
	}
}

// A system with no advice of its own gets Linux's.
func TestOtherSystemsGetLinuxsAdvice(t *testing.T) {
	stalled := proto.ErrorEvent{Code: proto.ErrorCaptureStalled}
	if got, want := WarningText(stalled, "freebsd"), WarningText(stalled, "linux"); got != want {
		t.Errorf("freebsd: %q, want %q", got, want)
	}
}

// A code the lexicon does not word, and a warning without the facts its
// words need, as an older recorder sends, keep the recorder's message.
func TestWarningsTheLexiconCannotWordKeepTheRecordersMessage(t *testing.T) {
	for _, e := range []proto.ErrorEvent{
		{Code: proto.ErrorRingOverflow, Msg: "12 frames lost"},
		{Code: proto.ErrorCaptureLowQuality, Msg: "The player's output device runs at 16 kHz mono, not fit for recording"},
		{Code: proto.ErrorDiskFull, Msg: "only 10 MB free in the output folder"},
		{Code: proto.ErrorPlayerVolumeLow, Msg: "the player's volume is at 40%: tracks recorded now are quieter"},
	} {
		if got := WarningText(e, "darwin"); got != e.Msg {
			t.Errorf("%s: %q, want the recorder's %q", e.Code, got, e.Msg)
		}
	}
}

// A failed player heard check says what the only silence warning says;
// passed, and like every other check, it keeps the recorder's detail.
func TestAFailedPlayerHeardCheckSaysTheSilenceWarning(t *testing.T) {
	for _, goos := range []string{"darwin", "windows", "linux"} {
		failed := proto.Check{ID: proto.CheckPlayerHeard, Name: "player heard", Detail: "only silence from the player"}
		if got, want := CheckDetail(failed, goos), WarningText(proto.ErrorEvent{Code: proto.ErrorCaptureSilent}, goos); got != want {
			t.Errorf("%s: %q, want %q", goos, got, want)
		}
	}
	for _, c := range []proto.Check{
		{ID: proto.CheckPlayerHeard, Name: "player heard", OK: true, Detail: "no silence heard"},
		{Name: "ffmpeg found", Detail: "not on PATH"},
	} {
		if got := CheckDetail(c, "darwin"); got != c.Detail {
			t.Errorf("%+v: %q, want the recorder's", c, got)
		}
	}
}

// The silence warning says to check the player's own volume first, then
// the system's: its mixer, or on macOS the System Audio Recording
// permission.
func TestTheSilenceWarningSaysThePlayersVolumeFirst(t *testing.T) {
	for goos, then := range map[string]string{
		"darwin":  "Screen & System Audio Recording",
		"windows": "Volume mixer",
		"linux":   "sound settings",
	} {
		w := WarningText(proto.ErrorEvent{Code: proto.ErrorCaptureSilent}, goos)
		volume := strings.Index(w, "the player is not muted in its own volume")
		if volume < 0 || !strings.Contains(w[volume:], then) {
			t.Errorf("%s: %q, want the player's own volume, then %q", goos, w, then)
		}
	}
}

// A write to the output folder that Windows refused, access denied, says
// after the recorder's reason that ransomware protection may hold the
// folder and how to let Spytify write there; in the check's detail and in
// the session's warnings alike (#887). Elsewhere, and for a write refused
// for another reason, the reason stays alone.
func TestADeniedOutputFolderNamesRansomwareProtectionOnWindows(t *testing.T) {
	const reason = `mkdir C:\Users\u\Music\Spytify: Access is denied.`
	check := proto.Check{ID: proto.CheckOutputWritable, Name: "output folder writable", Detail: reason, Args: []string{"denied"}}
	events := []proto.ErrorEvent{
		{Code: proto.ErrorOutputDir, Msg: reason, Fatal: true, Denied: true},
		{Code: proto.ErrorTempFile, Msg: `open C:\Users\u\Music\Spytify\.spytify\s-1.wav: Access is denied.`, Denied: true},
		{Code: proto.ErrorRename, Msg: "Midnight Ferry: rename: Access is denied.", Denied: true},
	}
	want := reason + " " + lang.OutputDeniedWindows
	if got := CheckDetail(check, "windows"); got != want {
		t.Errorf("check on windows: %q, want %q", got, want)
	}
	for _, e := range events {
		if got := WarningText(e, "windows"); got != e.Msg+" "+lang.OutputDeniedWindows {
			t.Errorf("%s on windows: %q, want the reason then the ransomware protection", e.Code, got)
		}
	}
	for _, goos := range []string{"darwin", "linux"} {
		if got := CheckDetail(check, goos); got != reason {
			t.Errorf("check on %s: %q, want the reason alone", goos, got)
		}
		for _, e := range events {
			if got := WarningText(e, goos); got != e.Msg {
				t.Errorf("%s on %s: %q, want the reason alone", e.Code, goos, got)
			}
		}
	}
	notDenied := check
	notDenied.Args = nil
	notDenied.Detail = `mkdir E:\Spytify: The system cannot find the path specified.`
	if got := CheckDetail(notDenied, "windows"); got != notDenied.Detail {
		t.Errorf("unreachable folder on windows: %q, want the reason alone", got)
	}
	if !strings.Contains(lang.OutputDeniedWindows, "spytifyd.exe") || !strings.Contains(lang.OutputDeniedWindows, "ffmpeg.exe") {
		t.Errorf("%q does not name both programs that write there", lang.OutputDeniedWindows)
	}
}
