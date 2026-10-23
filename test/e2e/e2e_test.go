// Package e2e builds spytifyd and spytify-cli and drives them as a user
// would, with the fake player standing in for a real one.
package e2e

import (
	"bufio"
	"bytes"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/pkg/proto"
)

var bin string

// exe is the path of a command built by TestMain.
func exe(name string) string {
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	return filepath.Join(bin, name)
}

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "spy-bin")
	if err != nil {
		panic(err)
	}
	bin = dir
	for _, cmd := range []string{"spytifyd", "spytify-cli"} {
		out, err := exec.Command("go", "build", "-o", exe(cmd), "../../cmd/"+cmd).CombinedOutput()
		if err != nil {
			panic(string(out))
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

type env struct {
	t      *testing.T
	dir    string
	out    string
	socket string
	vars   []string
}

func newEnv(t *testing.T) *env {
	t.Helper()
	if _, err := exec.LookPath("ffmpeg"); err != nil {
		t.Skip("ffmpeg not installed")
	}
	dir, err := os.MkdirTemp("", "spy")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.RemoveAll(dir) })
	e := &env{t: t, dir: dir, out: filepath.Join(dir, "out"), socket: filepath.Join(dir, "s.sock")}
	e.vars = append(os.Environ(),
		"SPYTIFY_TARGET_PROCESS=fakeplayer",
		"SPYTIFY_SOCKET="+e.socket,
		"SPYTIFY_CONFIG="+filepath.Join(dir, "config.toml"),
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
		"HOME="+dir,
		// Windows keeps the state and the user .env here instead.
		"LOCALAPPDATA="+filepath.Join(dir, "local"),
		"APPDATA="+filepath.Join(dir, "roaming"),
	)
	return e
}

var quiet = []string{
	"--set", "encode.format=wav", "--set", "recorder.normalize=false",
	"--set", "recorder.min_seconds=1", "--set", "output.group_by=none", "--set", "tags.cover=false",
}

func (e *env) daemonArgs(timeline string, extra ...string) []string {
	args := []string{"--fake", filepath.Join("..", "..", "testdata", "timelines", timeline+".json"), "--set", "output.dir=" + e.out}
	return append(append(args, quiet...), extra...)
}

func (e *env) cli(args ...string) (string, error) {
	cmd := exec.Command(exe("spytify-cli"), args...)
	cmd.Env = e.vars
	out, err := cmd.CombinedOutput()
	return string(out), err
}

func (e *env) mustCLI(args ...string) string {
	e.t.Helper()
	out, err := e.cli(args...)
	if err != nil {
		e.t.Fatalf("spytify-cli %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return out
}

func TestDaemonAndCLI(t *testing.T) {
	e := newEnv(t)
	logs := &bytes.Buffer{}
	d := exec.Command(exe("spytifyd"), e.daemonArgs("basic")...)
	d.Env, d.Stdout, d.Stderr = e.vars, logs, logs
	if err := d.Start(); err != nil {
		t.Fatal(err)
	}
	exited := make(chan error, 1)
	gone := make(chan struct{})
	go func() { exited <- d.Wait(); close(gone) }()
	defer func() {
		select {
		case <-gone:
		default:
			d.Process.Kill()
			<-gone
		}
	}()
	deadline := time.Now().Add(10 * time.Second)
	for {
		if _, err := os.Stat(e.socket); err == nil {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("daemon did not listen; log:\n%s", logs)
		}
		time.Sleep(50 * time.Millisecond)
	}

	if out := e.mustCLI("info"); !strings.Contains(out, "target   process=fakeplayer") || !strings.Contains(out, "[ok  ] fake player") {
		t.Fatalf("info:\n%s", out)
	}
	if out := e.mustCLI("set", "recorder.min_seconds=2", "output.prefix=counter"); !strings.Contains(out, "set output.prefix") {
		t.Fatalf("set:\n%s", out)
	}
	if out, err := e.cli("set", "encode.format=ogg"); err == nil || !strings.Contains(out, "rejected encode.format: expected one of mp3, aac, flac, wav") {
		t.Fatalf("invalid set accepted:\n%s", out)
	}
	if out := e.mustCLI("get", "output.prefix", "recorder.min_seconds"); out != "output.prefix = counter\nrecorder.min_seconds = 2\n" {
		t.Fatalf("get:\n%q", out)
	}
	cfg, err := os.ReadFile(filepath.Join(e.dir, "config.toml"))
	if err != nil || !strings.Contains(string(cfg), `prefix = "counter"`) {
		t.Fatalf("config.toml not written: %v\n%s", err, cfg)
	}

	if out := e.mustCLI("start"); !strings.Contains(out, "started") {
		t.Fatalf("start:\n%s", out)
	}
	for deadline := time.Now().Add(60 * time.Second); ; {
		out := e.mustCLI("status")
		if strings.Contains(out, "state    idle") {
			if !strings.Contains(out, "counters") {
				t.Fatalf("status:\n%s", out)
			}
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("session did not end:\n%s\nlog:\n%s", out, logs)
		}
		time.Sleep(100 * time.Millisecond)
	}
	files := fake.AudioFiles(e.out)
	want := []string{"001 Basic Artist - Basic One.wav", "002 Basic Artist - Basic Two.wav", "003 Basic Artist - Basic Three.wav"}
	if strings.Join(files, "|") != strings.Join(want, "|") {
		t.Fatalf("files = %v, want %v\nlog:\n%s", files, want, logs)
	}
	if out := e.mustCLI("tail", "-n", "200"); !strings.Contains(out, "saved "+filepath.Join(e.out, want[2])) {
		t.Fatalf("tail:\n%s", out)
	}
	if out := e.mustCLI("--json", "status"); !strings.Contains(out, `"state":"idle"`) {
		t.Fatalf("json status: %s", out)
	}
	if out, err := e.cli("stop"); err == nil || !strings.Contains(out, "no session is running") {
		t.Fatalf("stop without session:\n%s", out)
	}

	e.mustCLI("shutdown")
	select {
	case err := <-exited:
		if err != nil {
			t.Fatalf("daemon exit: %v\n%s", err, logs)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("daemon did not exit after shutdown")
	}
	if _, err := os.Stat(e.socket); !os.IsNotExist(err) {
		t.Fatal("socket left behind")
	}
}

func TestOnceWritesEventsForTheVerifier(t *testing.T) {
	for _, name := range []string{"jitter", "ads"} {
		t.Run(name, func(t *testing.T) {
			e := newEnv(t)
			events := filepath.Join(e.dir, "events.jsonl")
			d := exec.Command(exe("spytifyd"), e.daemonArgs(name, "--once", "--events", events)...)
			d.Env = e.vars
			if out, err := d.CombinedOutput(); err != nil {
				t.Fatalf("spytifyd --once: %v\n%s", err, out)
			}
			tl, err := fake.Load(filepath.Join("..", "..", "testdata", "timelines", name+".json"))
			if err != nil {
				t.Fatal(err)
			}
			if problems := fake.Verify(tl, tl.Expect, -1, e.out, readEvents(t, events)); len(problems) > 0 {
				t.Fatal(strings.Join(problems, "\n"))
			}
		})
	}
}

func readEvents(t *testing.T, path string) []proto.Event {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	var out []proto.Event
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<20)
	for sc.Scan() {
		var ev proto.Event
		if err := json.Unmarshal(sc.Bytes(), &ev); err != nil {
			t.Fatal(err)
		}
		out = append(out, ev)
	}
	return out
}

func TestDaemonRefusesWithoutTarget(t *testing.T) {
	e := newEnv(t)
	d := exec.Command(exe("spytifyd"))
	d.Env = append(e.vars, "SPYTIFY_TARGET_PROCESS=")
	out, err := d.CombinedOutput()
	if ee, ok := err.(*exec.ExitError); !ok || ee.ExitCode() != 2 || !strings.Contains(string(out), "SPYTIFY_TARGET_PROCESS is not set") {
		t.Fatalf("exit %v:\n%s", err, out)
	}
}
