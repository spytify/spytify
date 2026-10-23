package target

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
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

func TestDefaultPlayerWhenUnset(t *testing.T) {
	defer func(p, b string) { DefaultProcess, DefaultBundleID = p, b }(DefaultProcess, DefaultBundleID)
	t.Setenv("SPYTIFY_TARGET_PROCESS", "x")
	os.Unsetenv("SPYTIFY_TARGET_PROCESS")

	DefaultProcess, DefaultBundleID = "", ""
	if err := Load().Validate(); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("a build without a default player: Validate() = %v", err)
	}
	DefaultProcess, DefaultBundleID = "Player", "org.example.player"
	if err := Load().Validate(); err != nil {
		t.Fatalf("Validate() = %v with the default player", err)
	}
	for goos, want := range map[string]Target{
		"darwin":  {ProcessNames: []string{"Player"}, BundleID: "org.example.player"},
		"linux":   {ProcessNames: []string{"Player"}},
		"windows": {ProcessNames: []string{"Player"}},
	} {
		if got := withDefaultPlayer(Target{}, goos); !reflect.DeepEqual(got, want) {
			t.Errorf("%s: %+v, want %+v", goos, got, want)
		}
	}
}

func TestSaveUserTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("SPYTIFY_TARGET_PROCESS", "")
	t.Setenv("SPYTIFY_TARGET_BUNDLE_ID", "")
	path := UserEnvPath()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("SPYTIFY_FFMPEG=/opt/ffmpeg\nSPYTIFY_TARGET_BUNDLE_ID=old.bundle\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := SaveUserTarget(map[string]string{"SPYTIFY_TARGET_PROCESS": " Player ", "SPYTIFY_TARGET_BUNDLE_ID": ""}); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if s := string(b); !strings.Contains(s, `SPYTIFY_TARGET_PROCESS="Player"`) || !strings.Contains(s, "SPYTIFY_FFMPEG") || strings.Contains(s, "old.bundle") {
		t.Fatalf("file:\n%s", b)
	}
	if os.Getenv("SPYTIFY_TARGET_PROCESS") != "Player" {
		t.Fatal("not applied to this process")
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
