//go:build integration && linux

// Package integration records the fakeplayer process through PipeWire and
// MPRIS with spytifyd --once, and checks every file against its source:
// the M1 gate. It needs a running PipeWire session; see
// scripts/pipewire-session.sh.
package integration

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/pkg/proto"
)

var bin string

const (
	startDelay = time.Second
	leadIn     = 2 * time.Second
)

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "spy-it")
	if err != nil {
		panic(err)
	}
	bin = dir
	for _, cmd := range []string{"spytifyd", "spytify-cli", "fakeplayer"} {
		if out, err := exec.Command("go", "build", "-o", filepath.Join(dir, cmd), "../../cmd/"+cmd).CombinedOutput(); err != nil {
			panic(string(out))
		}
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func requireSession(t *testing.T) {
	t.Helper()
	if os.Getenv("DBUS_SESSION_BUS_ADDRESS") == "" {
		t.Skip("no D-Bus session; source scripts/pipewire-session.sh first")
	}
	if err := exec.Command("pw-cli", "info", "0").Run(); err != nil {
		t.Skip("no PipeWire session; source scripts/pipewire-session.sh first")
	}
}

var quiet = map[string]any{
	"encode.format": "wav", "recorder.normalize": false, "tags.source": "none",
	"recorder.min_seconds": 1, "output.group_by": "none", "tags.cover": false,
}

func timelines(t *testing.T) []string {
	if only := os.Getenv("SPYTIFY_TIMELINES"); only != "" {
		return strings.Split(only, ",")
	}
	return []string{"basic", "jitter", "gapless", "ads", "format-change", "pause-resume", "duplicate", "control"}
}

func TestRecordFakePlayer(t *testing.T) {
	requireSession(t)
	for _, name := range timelines(t) {
		t.Run(name, func(t *testing.T) {
			tl := load(t, name)
			variants := tl.Variants
			if len(variants) == 0 {
				variants = []fake.Variant{{Name: "default", Expect: tl.Expect}}
			}
			for _, v := range variants {
				t.Run(v.Name, func(t *testing.T) { record(t, name, v) })
			}
		})
	}
}

func load(t *testing.T, name string) *fake.Timeline {
	tl, err := fake.Load(timelinePath(name))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

func timelinePath(name string) string {
	return filepath.Join("..", "..", "testdata", "timelines", name+".json")
}

func record(t *testing.T, name string, v fake.Variant) {
	tl := load(t, name)
	dir := t.TempDir()
	out := filepath.Join(dir, "out")
	events := filepath.Join(dir, "events.jsonl")
	socket := filepath.Join(dir, "s.sock")
	env := append(os.Environ(),
		"SPYTIFY_TARGET_PROCESS=fakeplayer",
		"SPYTIFY_SOCKET="+socket,
		"SPYTIFY_CONFIG="+filepath.Join(dir, "config.toml"),
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
	)

	args := []string{"--once", "--events", events, "--idle-exit", "10s", "--wait-target", "10s", "--set", "output.dir=" + out}
	for _, m := range []map[string]any{quiet, tl.Settings, v.Settings} {
		for k, val := range m {
			args = append(args, "--set", fmt.Sprintf("%s=%v", k, val))
		}
	}
	if len(tl.Actions) > 0 {
		args = append(args, "--serve")
	}

	var playerLog, daemonLog bytes.Buffer
	player := exec.Command(filepath.Join(bin, "fakeplayer"), "--start-delay", startDelay.String(), "--lead-in", leadIn.String(), timelinePath(name))
	player.Env, player.Stdout, player.Stderr = env, &playerLog, &playerLog
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	defer player.Process.Kill()

	daemon := exec.Command(filepath.Join(bin, "spytifyd"), args...)
	daemon.Env, daemon.Stdout, daemon.Stderr = env, &daemonLog, &daemonLog
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer daemon.Process.Kill()

	for _, a := range tl.Actions {
		at := started.Add(startDelay + leadIn + time.Duration(a.AtMs)*time.Millisecond)
		time.Sleep(time.Until(at))
		cli := exec.Command(filepath.Join(bin, "spytify-cli"), "control", a.Cmd)
		cli.Env = env
		if b, err := cli.CombinedOutput(); err != nil {
			t.Errorf("spytify-cli control %s: %v %s", a.Cmd, err, b)
		}
	}

	var total time.Duration
	for _, it := range tl.Items {
		total += time.Duration(it.LengthMs+it.GapMs+it.PauseMs) * time.Millisecond
	}
	ctx, cancel := context.WithTimeout(context.Background(), total+startDelay+leadIn+90*time.Second)
	defer cancel()
	waitFor(ctx, t, "fakeplayer", player, &playerLog)
	waitFor(ctx, t, "spytifyd", daemon, &daemonLog)

	files := -1
	if v.Files != nil {
		files = *v.Files
	}
	if problems := fake.Verify(tl, v.Expect, files, out, readEvents(t, events)); len(problems) > 0 {
		for _, p := range problems {
			t.Error(p)
		}
		t.Logf("spytifyd log:\n%s\nfakeplayer log:\n%s", daemonLog.String(), playerLog.String())
	}
}

func waitFor(ctx context.Context, t *testing.T, name string, cmd *exec.Cmd, log *bytes.Buffer) {
	t.Helper()
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("%s: %v\n%s", name, err, log)
		}
	case <-ctx.Done():
		cmd.Process.Kill()
		t.Fatalf("%s did not finish\n%s", name, log)
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
		if err := json.Unmarshal(sc.Bytes(), &ev); err == nil {
			out = append(out, ev)
		}
	}
	return out
}
