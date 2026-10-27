package meta

import (
	"context"
	"encoding/base64"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
	"unicode/utf16"

	"github.com/spytify/spytify/internal/target"
)

func TestSMTCScriptQuotesItsParameters(t *testing.T) {
	m := NewSMTC()
	m.Target(target.Target{ProcessNames: []string{"Player.EXE", " it's "}})
	script := smtcScript(m.current(), map[string]string{"cmd": psQuote("next")}, smtcControl)
	if !strings.HasPrefix(script, "$names = @('player', 'it''s')\n$cmd = 'next'\n") {
		t.Fatalf("script starts with %q", script[:60])
	}
	raw, err := base64.StdEncoding.DecodeString(encodeCommand(script))
	if err != nil || len(raw)%2 != 0 {
		t.Fatalf("encoded command: %v", err)
	}
	units := make([]uint16, len(raw)/2)
	for i := range units {
		units[i] = binary.LittleEndian.Uint16(raw[2*i:])
	}
	if string(utf16.Decode(units)) != script {
		t.Fatal("-EncodedCommand does not decode to the script")
	}
}

// The Windows poller prints the macOS poller's shape; the tracker reads it
// with lengths in milliseconds and keeps the cover saved to a file.
func TestSMTCPollLines(t *testing.T) {
	k := &pollTracker{unit: "ms"}
	evs := k.next(poll(t, `{"running":true,"state":"playing","position":12.5,"track":{"id":null,"name":"Song","artist":"Artist","album":"Album","albumArtist":"Artist","duration":184864,"trackNumber":3,"artworkUrl":"file:///C:/Users/u/AppData/Local/Temp/spytify-cover-1"}}`))
	if len(evs) != 2 || evs[0].Kind != TrackChanged {
		t.Fatalf("first poll = %+v", evs)
	}
	got := evs[0].Track
	if got.Title != "Song" || got.LengthMs != 184864 || got.TrackNumber != 3 || got.ArtURL != "file:///C:/Users/u/AppData/Local/Temp/spytify-cover-1" || got.ID != "" {
		t.Fatalf("track = %+v", got)
	}
	if evs := k.next(poll(t, `{"running":false}`)); len(evs) != 1 || evs[0].Kind != PlayerGone {
		t.Fatalf("session gone = %+v", evs)
	}
}

// fakePowerShell makes the poller run this test binary, which prints out
// and exits with code, in place of Windows PowerShell.
func fakePowerShell(t *testing.T, out string, code int) {
	t.Helper()
	old := powershell
	powershell = func(ctx context.Context, script string) *exec.Cmd {
		cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestPowerShellStandIn$")
		cmd.Env = append(os.Environ(), "SPYTIFY_FAKE_PS_OUT="+out, "SPYTIFY_FAKE_PS_CODE="+strconv.Itoa(code))
		return cmd
	}
	t.Cleanup(func() { powershell = old })
}

func TestPowerShellStandIn(t *testing.T) {
	out, ok := os.LookupEnv("SPYTIFY_FAKE_PS_OUT")
	if !ok {
		t.Skip("run by fakePowerShell")
	}
	fmt.Print(out)
	code, _ := strconv.Atoi(os.Getenv("SPYTIFY_FAKE_PS_CODE"))
	os.Exit(code)
}

// Watch turns the poller's lines into events, skips a line that is not
// JSON, and closes its channel when the poller ends.
func TestSMTCWatchReadsThePoller(t *testing.T) {
	fakePowerShell(t, `{"running":true,"state":"playing","position":1.5,"track":{"name":"Song","artist":"Artist","album":"Album","duration":180000,"trackNumber":3,"artworkUrl":""}}
not json
{"running":true,"state":"playing","position":1.7,"error":"cover: access denied","track":{"name":"Song","artist":"Artist","album":"Album","duration":180000,"trackNumber":3,"artworkUrl":""}}
{"running":true,"state":"paused","position":2,"track":{"name":"Song","artist":"Artist","album":"Album","duration":180000,"trackNumber":3,"artworkUrl":""}}
{"running":false}
`, 0)
	m := NewSMTC()
	var logged []string
	m.Logf = func(f string, a ...any) { logged = append(logged, fmt.Sprintf(f, a...)) }
	events, err := m.Watch(context.Background(), target.Target{ProcessNames: []string{"Player.exe"}})
	if err != nil {
		t.Fatal(err)
	}
	var got []Event
	timeout := time.After(10 * time.Second)
	for done := false; !done; {
		select {
		case e := <-events:
			got = append(got, e)
			done = e.Kind == PlayerGone
		case <-timeout:
			t.Fatalf("events so far: %+v", got)
		}
	}
	paused := slices.ContainsFunc(got, func(e Event) bool { return e.Kind == StatusChanged && e.Status == Paused })
	if got[0].Kind != TrackChanged || got[0].Track.Title != "Song" || !paused || got[len(got)-1].Kind != PlayerGone {
		t.Fatalf("events %+v", got)
	}
	if !slices.Contains(logged, "now playing: cover: access denied") {
		t.Errorf("logged %q", logged)
	}
	if _, err := NewSMTC().Watch(context.Background(), target.Target{}); !errors.Is(err, ErrNoPlayer) {
		t.Fatalf("no player: %v", err)
	}
}

// Control's exit codes say what went wrong: 3 no session, 4 refused.
func TestSMTCControlReadsTheExitCode(t *testing.T) {
	m := NewSMTC()
	m.Target(target.Target{ProcessNames: []string{"player"}})
	for code, want := range map[int]string{0: "", 3: ErrNoPlayer.Error(), 4: "refused", 1: "powershell"} {
		fakePowerShell(t, "", code)
		err := m.Control("next")
		if (want == "" && err != nil) || (want != "" && (err == nil || !strings.Contains(err.Error(), want))) {
			t.Errorf("exit %d: %v", code, err)
		}
	}
	if err := m.Control("rewind"); !errors.Is(err, ErrUnsupported) {
		t.Errorf("rewind: %v", err)
	}
	if err := NewSMTC().Control("next"); !errors.Is(err, ErrNoPlayer) {
		t.Errorf("no player: %v", err)
	}
}
