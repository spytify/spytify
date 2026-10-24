//go:build integration && linux

package integration

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// maxUICPU is the share of one core the app may use while it shows a
// recording. QA found it taking a whole core on an M2, because every
// 10 Hz level event rebuilt the window. Measured here, with software
// rendering on a virtual display, the app used 17 % before that fix and
// 6 to 7 % after it: the meter still repaints ten times a second.
const maxUICPU = 12.0

// TestUIStaysIdleWhileRecording runs the real app on an X display (CI uses
// xvfb-run) while spytifyd records the fake player through PipeWire, and
// measures the app's CPU over 10 s of recording.
func TestUIStaysIdleWhileRecording(t *testing.T) {
	requireSession(t)
	if os.Getenv("DISPLAY") == "" {
		t.Skip("needs an X display: run under xvfb-run")
	}
	// SPYTIFY_UI_BINARY measures another build of the app, such as one from
	// before a fix, to check that this test would have caught it.
	ui := os.Getenv("SPYTIFY_UI_BINARY")
	if ui == "" {
		ui = filepath.Join(bin, "spytify-ui")
		if out, err := exec.Command("go", "build", "-o", ui, "../../cmd/spytify-ui").CombinedOutput(); err != nil {
			t.Fatalf("building the app: %v\n%s", err, out)
		}
	}
	dir := t.TempDir()
	env := append(os.Environ(),
		"SPYTIFY_TARGET_PROCESS=fakeplayer",
		"SPYTIFY_SOCKET="+filepath.Join(dir, "s.sock"),
		"SPYTIFY_CONFIG="+filepath.Join(dir, "config.toml"),
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
		"XDG_CONFIG_HOME="+filepath.Join(dir, "config"),
		"HOME="+filepath.Join(dir, "home"),
	)
	// The first-run dialog is already answered: this measures the record
	// screen as it is used. (Under a dialog the meter does not move.)
	prefs := filepath.Join(dir, "config", "fyne", "org.spytify.ui")
	if err := os.MkdirAll(prefs, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(prefs, "preferences.json"), []byte(`{"first-run-seen":true}`), 0o644); err != nil {
		t.Fatal(err)
	}
	const delay = 12 * time.Second
	player := exec.Command(filepath.Join(bin, "fakeplayer"), "--start-delay", delay.String(), "--lead-in", leadIn.String(), timelinePath("jitter"))
	player.Env = env
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	defer player.Process.Kill()

	args := []string{"--set", "output.dir=" + filepath.Join(dir, "out")}
	for k, v := range quiet {
		args = append(args, "--set", fmt.Sprintf("%s=%v", k, v))
	}
	daemon := exec.Command(filepath.Join(bin, "spytifyd"), args...)
	daemon.Env = env
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer daemon.Process.Kill()

	app := exec.Command(ui)
	app.Env = env
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	defer app.Process.Kill()

	cli := func(args ...string) {
		t.Helper()
		c := exec.Command(filepath.Join(bin, "spytify-cli"), args...)
		c.Env = env
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("spytify-cli %v: %v %s", args, err, out)
		}
	}
	time.Sleep(4 * time.Second)
	cli("start")
	defer cli("shutdown")

	// The fake player's audio starts after its delay and lead-in; the
	// jitter timeline then plays ten items for about 55 s.
	time.Sleep(time.Until(started.Add(delay + leadIn + 3*time.Second)))
	a := cpuTicks(t, app.Process.Pid)
	const window = 10 * time.Second
	time.Sleep(window)
	b := cpuTicks(t, app.Process.Pid)
	share := float64(b-a) / clockTicks(t) / window.Seconds() * 100
	t.Logf("the app used %.1f %% of a core over %s of recording", share, window)
	if share > maxUICPU {
		t.Errorf("the app used %.1f %% of a core while recording, more than %.0f %%", share, maxUICPU)
	}
}

// cpuTicks is a process's user and system time, in clock ticks.
func cpuTicks(t *testing.T, pid int) int64 {
	t.Helper()
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/stat", pid))
	if err != nil {
		t.Fatalf("the app is not running: %v", err)
	}
	// Fields after the command name, which is in parentheses and may hold
	// spaces: utime and stime are the 12th and 13th.
	f := strings.Fields(string(b[strings.LastIndexByte(string(b), ')')+2:]))
	utime, _ := strconv.ParseInt(f[11], 10, 64)
	stime, _ := strconv.ParseInt(f[12], 10, 64)
	return utime + stime
}

func clockTicks(t *testing.T) float64 {
	out, err := exec.Command("getconf", "CLK_TCK").Output()
	if err != nil {
		return 100
	}
	n, err := strconv.ParseFloat(strings.TrimSpace(string(out)), 64)
	if err != nil || n <= 0 {
		return 100
	}
	return n
}
