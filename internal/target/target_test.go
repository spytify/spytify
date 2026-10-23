package target

import (
	"errors"
	"os"
	"reflect"
	"testing"
)

func TestLoadSplitsProcessList(t *testing.T) {
	t.Setenv("SPYTIFY_TARGET_PROCESS", " player , player-snap,")
	t.Setenv("SPYTIFY_TARGET_MPRIS", "")
	t.Setenv("SPYTIFY_SOCKET", "/tmp/x.sock")
	env := Load()
	if want := []string{"player", "player-snap"}; !reflect.DeepEqual(env.Target.ProcessNames, want) {
		t.Fatalf("ProcessNames = %v, want %v", env.Target.ProcessNames, want)
	}
	if env.SocketPath != "/tmp/x.sock" {
		t.Fatalf("SocketPath = %q", env.SocketPath)
	}
	if want := []string{"org.mpris.MediaPlayer2.player", "org.mpris.MediaPlayer2.player-snap"}; !reflect.DeepEqual(env.Target.MPRISNames(), want) {
		t.Fatalf("MPRISNames = %v, want %v", env.Target.MPRISNames(), want)
	}
}

func TestExplicitMPRISNameWins(t *testing.T) {
	tg := Target{ProcessNames: []string{"Player.exe"}, MPRISName: "org.mpris.MediaPlayer2.custom"}
	if got := tg.MPRISNames(); !reflect.DeepEqual(got, []string{"org.mpris.MediaPlayer2.custom"}) {
		t.Fatalf("MPRISNames = %v", got)
	}
}

func TestValidateRequiresProcess(t *testing.T) {
	t.Setenv("SPYTIFY_TARGET_PROCESS", "")
	if err := Load().Validate(); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("Validate() = %v, want ErrNoTarget", err)
	}
}

func TestMatches(t *testing.T) {
	tg := Target{ProcessNames: []string{"Player.exe", "averyveryverylongplayername"}}
	cases := map[string]bool{
		"player":          true,
		"PLAYER.EXE":      true,
		"/usr/bin/player": true,
		"play":            false,
		"averyveryverylo": true, // truncated /proc comm
		"":                false,
	}
	for name, want := range cases {
		if got := tg.Matches(name); got != want {
			t.Errorf("Matches(%q) = %v, want %v", name, got, want)
		}
	}
}

func TestFindProcessFindsItself(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	p, ok := FindProcess(Target{ProcessNames: []string{exe}})
	if !ok {
		t.Fatalf("FindProcess did not find the test binary %q", exe)
	}
	if p.PID <= 0 {
		t.Fatalf("PID = %d", p.PID)
	}
}
