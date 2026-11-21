//go:build windows

package link

import (
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"

	"golang.org/x/sys/windows"

	"github.com/spytify/spytify/internal/ui/model"
)

// The fake app starts its recorder as the app does, the fake recorder
// starts a player as a recorder may, and each writes its PID to
// SPYTIFY_FAKE_DIR. Then they wait to be killed.
func init() {
	fakeParts["app"] = func() {
		dir := os.Getenv("SPYTIFY_FAKE_DIR")
		os.Setenv("SPYTIFY_FAKE_PART", "recorder")
		l := New(filepath.Join(dir, "absent.sock"), model.New())
		if err := l.StartDaemon(); err != nil {
			os.Exit(1)
		}
		if os.Getenv("SPYTIFY_FAKE_RELEASE") != "" {
			if err := l.Release(); err != nil {
				os.Exit(1)
			}
		}
		writePID(dir, "app")
		time.Sleep(2 * time.Minute)
	}
	fakeParts["recorder"] = func() {
		dir := os.Getenv("SPYTIFY_FAKE_DIR")
		self, err := os.Executable()
		if err != nil {
			os.Exit(1)
		}
		player := exec.Command(self)
		player.Env = append(os.Environ(), "SPYTIFY_FAKE_PART=player")
		if err := player.Start(); err != nil {
			os.Exit(1)
		}
		os.WriteFile(filepath.Join(dir, "player"), []byte(strconv.Itoa(player.Process.Pid)), 0o644)
		writePID(dir, "recorder")
		time.Sleep(2 * time.Minute)
	}
	fakeParts["player"] = func() { time.Sleep(2 * time.Minute) }
}

func writePID(dir, name string) {
	os.WriteFile(filepath.Join(dir, name), []byte(strconv.Itoa(os.Getpid())), 0o644)
}

// startFakeApp starts the fake app and returns it with the PIDs of its
// recorder and of the player that recorder started. Whatever still runs
// as the test ends is killed.
func startFakeApp(t *testing.T, releaseIt bool) (app *exec.Cmd, recorder, player uint32) {
	t.Helper()
	fakeDaemon(t, "")
	dir := t.TempDir()
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	app = exec.Command(self)
	app.Env = append(os.Environ(), "SPYTIFY_FAKE_PART=app", "SPYTIFY_FAKE_DIR="+dir)
	if releaseIt {
		app.Env = append(app.Env, "SPYTIFY_FAKE_RELEASE=1")
	}
	if err := app.Start(); err != nil {
		t.Fatal(err)
	}
	pids := map[string]uint32{}
	t.Cleanup(func() {
		app.Process.Kill()
		app.Wait()
		for _, pid := range pids {
			kill(pid)
		}
	})
	for deadline := time.Now().Add(15 * time.Second); len(pids) < 3; time.Sleep(50 * time.Millisecond) {
		if time.Now().After(deadline) {
			t.Fatalf("the fake app wrote only %v", pids)
		}
		for _, name := range []string{"app", "recorder", "player"} {
			if b, err := os.ReadFile(filepath.Join(dir, name)); err == nil && len(b) > 0 {
				n, _ := strconv.Atoi(string(b))
				pids[name] = uint32(n)
			}
		}
	}
	return app, pids["recorder"], pids["player"]
}

// exits reports whether the process pid ends within d.
func exits(pid uint32, d time.Duration) bool {
	h, err := windows.OpenProcess(windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return true
	}
	defer windows.CloseHandle(h)
	ev, _ := windows.WaitForSingleObject(h, uint32(d.Milliseconds()))
	return ev == windows.WAIT_OBJECT_0
}

func kill(pid uint32) {
	h, err := windows.OpenProcess(windows.PROCESS_TERMINATE|windows.SYNCHRONIZE, false, pid)
	if err != nil {
		return
	}
	defer windows.CloseHandle(h)
	windows.TerminateProcess(h, 1)
	windows.WaitForSingleObject(h, 5000)
}

// An app killed (End task, a crash) takes the recorder it started with it,
// and not the player that recorder started.
func TestKillingTheAppEndsTheRecorderItStarted(t *testing.T) {
	app, recorder, player := startFakeApp(t, false)
	if err := app.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	app.Wait()
	if !exits(recorder, 5*time.Second) {
		t.Error("the recorder still runs 5 s after its app was killed")
	}
	if exits(player, time.Second) {
		t.Error("the player ended with the app")
	}
}

// After a Quit, the recorder outlives the app to finish saving.
func TestAReleasedRecorderOutlivesTheApp(t *testing.T) {
	app, recorder, _ := startFakeApp(t, true)
	if err := app.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	app.Wait()
	if exits(recorder, 2*time.Second) {
		t.Error("the released recorder ended with its app")
	}
}
