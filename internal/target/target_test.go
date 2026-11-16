package target

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"

	"github.com/spytify/spytify/pkg/proto"
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
	defer func(p string) { DefaultProcess = p }(DefaultProcess)
	// Nothing the shell exports counts, build.env's MPRIS name included.
	for _, k := range TargetKeys {
		t.Setenv(k, "x")
		os.Unsetenv(k)
	}

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
		"player":            true,
		"PLAYER.EXE":        true,
		"/usr/bin/player":   true,
		"play":              false,
		"averyveryverylo":   true, // truncated /proc comm
		"averyveryverylon":  true, // truncated macOS p_comm
		"averyveryverylonx": false,
		"":                  false,
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

// When the kernel's list comes back empty on macOS, ps lists instead, and
// finds the player as before #835.
func TestAnEmptyListingFallsBackToPS(t *testing.T) {
	calls := 0
	fallback := func() []Process { calls++; return []Process{{PID: 7, Name: "player"}} }
	if got := orElse([]Process{{PID: 3, Name: "player"}}, fallback); calls != 0 || got[0].PID != 3 {
		t.Errorf("a listing that worked: %v, fallback called %d times", got, calls)
	}
	if got := orElse(nil, fallback); calls != 1 || len(got) != 1 || got[0].PID != 7 {
		t.Errorf("an empty listing: %v, fallback called %d times", got, calls)
	}
	// The fallback is macOS's alone.
	if runtime.GOOS != "darwin" {
		return
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	if p, ok := pickRoot(Target{ProcessNames: []string{exe}}, listPS()); !ok || p.PID != os.Getpid() {
		t.Errorf("ps found %v, %v; want the test binary, pid %d", p, ok, os.Getpid())
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
	// Without parents (Linux) the first match wins, as before.
	if p, ok := pickRoot(tg, []Process{{PID: 7, Name: "player"}, {PID: 3, Name: "player"}}); !ok || p.PID != 7 {
		t.Fatalf("first match = %+v, %v", p, ok)
	}
	if _, ok := pickRoot(tg, []Process{{PID: 9, Name: "other"}}); ok {
		t.Fatal("matched a process of another name")
	}
}

func setVersion(t *testing.T, v string) {
	old := proto.Version
	proto.Version = v
	t.Cleanup(func() { proto.Version = old })
}

// A release build reads only the user's .env: one in the working directory,
// where a download or a shell may have left it, is ignored. And only
// SPYTIFY_* keys are applied, from any file.
func TestLoadDotEnvInAReleaseReadsOnlyTheUsersSpytifyKeys(t *testing.T) {
	setVersion(t, "v2.0.2")
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(home, ".config"))
	t.Setenv("APPDATA", filepath.Join(home, "AppData", "Roaming"))
	t.Setenv("USERPROFILE", home)
	for _, k := range []string{"SPYTIFY_TEST_LAUNCH", "SPYTIFY_TEST_USER", "SPYTIFY_TEST_OTHER"} {
		t.Setenv(k, "")
		os.Unsetenv(k)
	}
	if err := os.MkdirAll(filepath.Dir(UserEnvPath()), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserEnvPath(), []byte("SPYTIFY_TEST_USER=user\nSPYTIFY_TEST_OTHER=user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	work := t.TempDir()
	if err := os.WriteFile(filepath.Join(work, ".env"), []byte("SPYTIFY_TEST_LAUNCH=planted\nSPYTIFY_TEST_OTHER=cwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	t.Chdir(work)
	LoadDotEnv()
	if v, set := os.LookupEnv("SPYTIFY_TEST_LAUNCH"); set {
		t.Errorf("a release applied the working directory's .env: SPYTIFY_TEST_LAUNCH = %q", v)
	}
	for k, want := range map[string]string{"SPYTIFY_TEST_USER": "user", "SPYTIFY_TEST_OTHER": "user"} {
		if got := os.Getenv(k); got != want {
			t.Errorf("%s = %q, want %q", k, got, want)
		}
	}

	setVersion(t, "v2.0.3-dev.4")
	t.Setenv("SPYTIFY_TEST_OTHER", "")
	os.Unsetenv("SPYTIFY_TEST_OTHER")
	t.Setenv("PATH_TEST_SPYTIFY", "")
	os.Unsetenv("PATH_TEST_SPYTIFY")
	if err := os.WriteFile(filepath.Join(work, ".env"), []byte("PATH_TEST_SPYTIFY=cwd\nSPYTIFY_TEST_OTHER=cwd\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(UserEnvPath(), []byte("PATH_TEST_SPYTIFY=user\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	LoadDotEnv()
	if v, set := os.LookupEnv("PATH_TEST_SPYTIFY"); set {
		t.Errorf("a key without the SPYTIFY_ prefix was applied: %q", v)
	}
	if got := os.Getenv("SPYTIFY_TEST_OTHER"); got != "cwd" {
		t.Errorf("a development build ignored the working directory's .env: SPYTIFY_TEST_OTHER = %q", got)
	}
}

// A variable set in the environment wins over the working directory's
// .env, which wins over the user's (the one the app writes).
func TestLoadDotEnvKeepsTheFirstValue(t *testing.T) {
	setVersion(t, "v2.0.3-dev.4")
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

// The test binary is running; a pid with no process, or another target's,
// is not.
func TestRunningAsksAboutOnePid(t *testing.T) {
	if runtime.GOOS != "darwin" && runtime.GOOS != "linux" {
		t.Skip("Running cannot tell on " + runtime.GOOS)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Skip(err)
	}
	self := Target{ProcessNames: []string{exe}}
	if !Running(self, os.Getpid()) {
		t.Fatalf("the test binary %q is not running", exe)
	}
	if Running(Target{ProcessNames: []string{"no-such-player"}}, os.Getpid()) {
		t.Fatal("another target matched the test binary")
	}
	cmd := exec.Command("true")
	if err := cmd.Run(); err != nil {
		t.Skip(err)
	}
	if Running(Target{ProcessNames: []string{"true"}}, cmd.Process.Pid) {
		t.Fatal("a process that exited is running")
	}
}
