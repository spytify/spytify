//go:build darwin

package capture

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/target"
)

func TestTapSupported(t *testing.T) {
	cases := map[string]bool{"13.6": false, "14.1.2": false, "14.2": true, "14.4.1": true, "15.0": true, "26.7.1": true}
	for v, want := range cases {
		if got := TapSupported(v); got != want {
			t.Errorf("TapSupported(%q) = %v", v, got)
		}
	}
	if MacOSVersion() == "" {
		t.Fatal("no macOS version from sysctl")
	}
}

func TestTapArgs(t *testing.T) {
	got := TapArgs(target.Target{ProcessNames: []string{"Player", "Player Helper"}, BundleID: "org.example.player", Rate: 44100}, true, "/state/device-rates.json")
	want := []string{"--bundle-id", "org.example.player", "--name", "Player", "--name", "Player Helper", "--mute", "--rate", "44100", "--state", "/state/device-rates.json"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
	if got := TapArgs(target.Target{ProcessNames: []string{"Player"}}, false, "/state/device-rates.json"); !reflect.DeepEqual(got, []string{"--name", "Player"}) {
		t.Fatalf("a target with no rate: got %v", got)
	}
}

// The recording sets the player's device to the player's rate, and the
// helper's --restore puts it back when the capture closes. It also runs when
// the capture opens, for a device a crashed session left switched.
func TestTheDeviceRateIsPutBackWhenTheCaptureOpensAndCloses(t *testing.T) {
	dir := t.TempDir()
	calls := filepath.Join(dir, "calls")
	script := filepath.Join(dir, "spytify-tap")
	body := `#!/bin/sh
echo "$*" >> "` + calls + `"
case " $* " in *" --restore "*) exit 0 ;; esac
printf 'SPYTIFY-TAP f32le 44100 2\n'
exec cat /dev/zero
`
	if err := os.WriteFile(script, []byte(body), 0o755); err != nil {
		t.Fatal(err)
	}
	c, err := NewDarwin(script, false, dir, t.Logf)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Open(context.Background(), target.Target{ProcessNames: []string{"Player"}, Rate: 44100}); err != nil {
		t.Fatal(err)
	}
	select {
	case <-c.Frames():
	case <-time.After(10 * time.Second):
		t.Fatal("no frames from the helper")
	}
	c.Close()
	b, err := os.ReadFile(calls)
	if err != nil {
		t.Fatal(err)
	}
	state := filepath.Join(dir, "device-rates.json")
	got := strings.Split(strings.TrimSpace(string(b)), "\n")
	want := []string{"--restore --state " + state, "--name Player --rate 44100 --state " + state, "--restore --state " + state}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("helper runs:\n%s\nwant:\n%s", strings.Join(got, "\n"), strings.Join(want, "\n"))
	}
}
