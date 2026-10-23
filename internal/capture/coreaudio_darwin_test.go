//go:build darwin

package capture

import (
	"reflect"
	"testing"

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
	got := TapArgs(target.Target{ProcessNames: []string{"Player", "Player Helper"}, BundleID: "org.example.player"}, true)
	want := []string{"--bundle-id", "org.example.player", "--name", "Player", "--name", "Player Helper", "--mute"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v", got)
	}
}
