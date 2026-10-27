package wintest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

var bin string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "spy-bin")
	if err != nil {
		panic(err)
	}
	bin = dir
	for _, cmd := range []string{"spytifyd", "spytify-cli"} {
		out, err := exec.Command("go", "build", "-o", filepath.Join(bin, cmd+".exe"), "../../cmd/"+cmd).CombinedOutput()
		if err != nil {
			panic(string(out))
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

var (
	user32            = windows.NewLazySystemDLL("user32.dll")
	setWinEventHook   = user32.NewProc("SetWinEventHook")
	unhookWinEvent    = user32.NewProc("UnhookWinEvent")
	getMessage        = user32.NewProc("GetMessageW")
	postThreadMessage = user32.NewProc("PostThreadMessageW")
	getAncestor       = user32.NewProc("GetAncestor")
	getClassName      = user32.NewProc("GetClassNameW")
)

const (
	eventSystemForeground = 0x0003
	eventObjectShow       = 0x8002
	objidWindow           = 0
	gaRoot                = 2
	wmQuit                = 0x0012
)

// shownWindow is a top-level window that appeared or came to the front.
type shownWindow struct {
	event uintptr
	class string
	exe   string
}

func (w shownWindow) String() string {
	what := "shown"
	if w.event == eventSystemForeground {
		what = "brought to the front"
	}
	return w.class + " window of " + w.exe + " " + what
}

// console says whether w is a console window: conhost's, or a Windows
// Terminal tab where Windows 11 opens consoles.
func (w shownWindow) console() bool {
	return w.class == "ConsoleWindowClass" || w.class == "CASCADIA_HOSTING_WINDOW_CLASS" ||
		strings.EqualFold(w.exe, "conhost.exe") || strings.EqualFold(w.exe, "OpenConsole.exe") || strings.EqualFold(w.exe, "WindowsTerminal.exe")
}

// ours says whether w belongs to spytify or a program it starts, or is a
// console one of them opened; anything else on the runner's desktop is
// not this test's business.
func (w shownWindow) ours() bool {
	for _, exe := range []string{"spytifyd.exe", "ffmpeg.exe", "ffprobe.exe", "cmd.exe", "powershell.exe", "spytify-tap.exe"} {
		if strings.EqualFold(w.exe, exe) {
			return true
		}
	}
	return w.console()
}

// watcher records every top-level window shown or brought to the front
// while it runs, however briefly: a hook, not a poll that a flash slips
// between.
type watcher struct {
	mu     sync.Mutex
	seen   []shownWindow
	thread uint32
	done   chan struct{}
}

func watchWindows(t *testing.T) *watcher {
	t.Helper()
	w := &watcher{done: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		runtime.LockOSThread()
		defer close(w.done)
		w.thread = windows.GetCurrentThreadId()
		cb := syscall.NewCallback(func(hook, event, hwnd, idObject, idChild, thread, ms uintptr) uintptr {
			if int32(idObject) != objidWindow || hwnd == 0 {
				return 0
			}
			if root, _, _ := getAncestor.Call(hwnd, gaRoot); root != hwnd {
				return 0
			}
			w.add(event, windows.HWND(hwnd))
			return 0
		})
		var hooks []uintptr
		for _, ev := range []uintptr{eventObjectShow, eventSystemForeground} {
			h, _, err := setWinEventHook.Call(ev, ev, 0, cb, 0, 0, 0)
			if h == 0 {
				ready <- err
				return
			}
			hooks = append(hooks, h)
		}
		ready <- nil
		var msg [48]byte
		for {
			if r, _, _ := getMessage.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0); r == 0 || int32(r) == -1 {
				break
			}
		}
		for _, h := range hooks {
			unhookWinEvent.Call(h)
		}
	}()
	if err := <-ready; err != nil {
		t.Fatalf("SetWinEventHook: %v", err)
	}
	t.Cleanup(func() {
		postThreadMessage.Call(uintptr(w.thread), wmQuit, 0, 0)
		<-w.done
	})
	return w
}

func (w *watcher) add(event uintptr, hwnd windows.HWND) {
	var name [256]uint16
	n, _, _ := getClassName.Call(uintptr(hwnd), uintptr(unsafe.Pointer(&name[0])), uintptr(len(name)))
	var pid uint32
	windows.GetWindowThreadProcessId(hwnd, &pid)
	w.mu.Lock()
	w.seen = append(w.seen, shownWindow{event: event, class: windows.UTF16ToString(name[:n]), exe: exeOf(pid)})
	w.mu.Unlock()
}

func (w *watcher) windows() []shownWindow {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]shownWindow(nil), w.seen...)
}

func (w *watcher) reset() {
	w.mu.Lock()
	w.seen = nil
	w.mu.Unlock()
}

func exeOf(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "process " + strconv.FormatUint(uint64(pid), 10)
	}
	defer windows.CloseHandle(h)
	var buf [windows.MAX_PATH]uint16
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return "?"
	}
	return filepath.Base(windows.UTF16ToString(buf[:n]))
}

// detached starts cmd the way the app starts spytifyd: with no console,
// so a console program it starts without proc's flags opens a window.
func detached(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: windows.CREATE_NEW_PROCESS_GROUP | windows.DETACHED_PROCESS | windows.CREATE_NO_WINDOW}
}

// Saving tracks runs ffmpeg several times each (encode, loudness, tags,
// cover) from a spytifyd started as the app starts it; none of it may
// open a window.
func TestSavingASongOpensNoWindow(t *testing.T) {
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	w := watchWindows(t)
	// The hook has to see a console window on this runner for the check to
	// mean anything.
	probe := exec.Command("cmd", "/C", "ping -n 2 127.0.0.1 > nul")
	probe.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	if err := probe.Run(); err != nil {
		t.Fatalf("probe: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	sawProbe := false
	for _, win := range w.windows() {
		sawProbe = sawProbe || win.console()
	}
	if !sawProbe {
		t.Skipf("no console window seen for the probe on this runner (saw %v): the check needs a desktop", w.windows())
	}
	w.reset()

	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	d := exec.Command(filepath.Join(bin, "spytifyd.exe"), "--once", "--fake", filepath.Join("..", "..", "testdata", "timelines", "basic.json"),
		"--set", "output.dir="+out, "--set", "encode.format=mp3", "--set", "recorder.normalize=true", "--set", "tags.cover=true")
	d.Env = append(os.Environ(),
		"SPYTIFY_SOCKET="+filepath.Join(dir, "s.sock"),
		"SPYTIFY_CONFIG="+filepath.Join(dir, "config.toml"),
		"LOCALAPPDATA="+filepath.Join(dir, "local"),
		"APPDATA="+filepath.Join(dir, "roaming"))
	detached(d)
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	finished := make(chan error, 1)
	go func() { finished <- d.Wait() }()
	select {
	case err := <-finished:
		if err != nil {
			t.Fatalf("spytifyd: %v", err)
		}
	case <-time.After(3 * time.Minute):
		d.Process.Kill()
		t.Fatal("spytifyd did not finish in 3 minutes")
	}
	time.Sleep(500 * time.Millisecond)

	saved, _ := filepath.Glob(filepath.Join(out, "*.mp3"))
	if len(saved) != 3 {
		t.Fatalf("saved %v, want 3 mp3 files", saved)
	}
	for _, win := range w.windows() {
		if win.ours() {
			t.Errorf("a %v while saving", win)
		}
	}
}
