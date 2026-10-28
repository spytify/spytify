package target

import (
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"runtime"
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

// The target's rate is the default player's unless SPYTIFY_TARGET_RATE
// gives another; 0 leaves the device alone.
func TestTargetRate(t *testing.T) {
	for env, want := range map[string]int{"": 44100, "nonsense": 44100, "96000": 96000, " 48000 ": 48000, "0": 0} {
		t.Setenv("SPYTIFY_TARGET_RATE", env)
		if got := Load().Target.Rate; got != want {
			t.Errorf("SPYTIFY_TARGET_RATE=%q: Rate = %d, want %d", env, got, want)
		}
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
	defer func(p string) { DefaultProcess = p }(DefaultProcess)
	t.Setenv("SPYTIFY_TARGET_PROCESS", "x")
	os.Unsetenv("SPYTIFY_TARGET_PROCESS")

	DefaultProcess = ""
	if err := Load().Validate(); !errors.Is(err, ErrNoTarget) {
		t.Fatalf("a build without a default player: Validate() = %v", err)
	}
	DefaultProcess = "player"
	env := Load()
	if err := env.Validate(); err != nil {
		t.Fatalf("Validate() = %v with the default player", err)
	}
	tg := env.Target
	// One bare name matches every OS's form of the executable.
	for _, name := range []string{"player", "Player", "Player.exe", "PLAYER.EXE", "Player.app", "/Applications/Player.app/Contents/MacOS/Player"} {
		if !tg.Matches(name) {
			t.Errorf("%q does not match the default player", name)
		}
	}
	if got := tg.MPRISNames(); !reflect.DeepEqual(got, []string{"org.mpris.MediaPlayer2.player"}) {
		t.Errorf("MPRIS names %v", got)
	}
}

func TestSaveUserTarget(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	// os.UserConfigDir reads AppData on Windows, not HOME or XDG_CONFIG_HOME;
	// without these the test would write the real %APPDATA%\spytify\.env.
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("USERPROFILE", home)
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

func TestPickRootFindsTheTreeRoot(t *testing.T) {
	tg := Target{ProcessNames: []string{"Player"}}
	procs := []Process{
		{PID: 40, Parent: 30, Name: "Player.exe"}, // a renderer child, listed first
		{PID: 30, Parent: 4, Name: "player.exe"},
		{PID: 50, Parent: 30, Name: "Player.exe"},
		{PID: 60, Parent: 4, Name: "other.exe"},
	}
	if p, ok := pickRoot(tg, procs); !ok || p.PID != 30 {
		t.Fatalf("root = %+v, %v", p, ok)
	}
	// Without parents (Linux, macOS) the first match wins, as before.
	if p, ok := pickRoot(tg, []Process{{PID: 7, Name: "player"}, {PID: 3, Name: "player"}}); !ok || p.PID != 7 {
		t.Fatalf("first match = %+v, %v", p, ok)
	}
	if _, ok := pickRoot(tg, []Process{{PID: 9, Name: "other"}}); ok {
		t.Fatal("matched a process of another name")
	}
}

// A variable set in the environment wins over the working directory's
// .env, which wins over the user's (the one the app writes).
func TestLoadDotEnvKeepsTheFirstValue(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("USERPROFILE", home)
	t.Setenv("SPYTIFY_TEST_A", "env")
	for _, k := range []string{"SPYTIFY_TEST_B", "SPYTIFY_TEST_C"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	if err := os.MkdirAll(filepath.Dir(UserEnvPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserEnvPath(), []byte("SPYTIFY_TEST_B=user\nSPYTIFY_TEST_C=user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, ".env"), []byte("SPYTIFY_TEST_A=cwd\nSPYTIFY_TEST_B=cwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	LoadDotEnv()
	for k, want := range map[string]string{"SPYTIFY_TEST_A": "env", "SPYTIFY_TEST_B": "cwd", "SPYTIFY_TEST_C": "user"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}
}

func TestProcessNameIsTheFirstName(t *testing.T) {
	if got := (Target{}).ProcessName(); got != "" {
		t.Errorf("no name: %q", got)
	}
	if got := (Target{ProcessNames: []string{"Player.app", "PlayerHelper"}}).ProcessName(); got != "Player.app" {
		t.Errorf("first name: %q", got)
	}
	if got := (Target{ProcessNames: []string{"Player.app"}}).AppSpecifier(); got != "Player" {
		t.Errorf("app specifier: %q", got)
	}
}

// The socket and the state folder follow each OS's place for them.
func TestDefaultPaths(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	t.Setenv("XDG_RUNTIME_DIR", dir)
	t.Setenv("XDG_STATE_HOME", dir)
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	socket, state := DefaultSocketPath(), defaultStateDir()
	want := map[string][2]string{
		"windows": {filepath.Join(dir, "Spytify", "spytify.sock"), filepath.Join(dir, "Spytify")},
		"darwin":  {filepath.Join(dir, "Library", "Application Support", "Spytify", "spytify.sock"), filepath.Join(dir, "spytify")},
		"linux":   {filepath.Join(dir, "spytify.sock"), filepath.Join(dir, "spytify")},
	}[runtime.GOOS]
	if socket != want[0] || state != want[1] {
		t.Errorf("socket %s, state %s; want %s, %s", socket, state, want[0], want[1])
	}
	if runtime.GOOS == "linux" {
		t.Setenv("XDG_RUNTIME_DIR", "")
		if got := DefaultSocketPath(); filepath.Dir(got) != os.TempDir() {
			t.Errorf("without XDG_RUNTIME_DIR: %s", got)
		}
	}
}
