//go:build integration && linux

package integration

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"
)

// A muted player plays into Spytify's silent sink while it is recorded, and
// on the default sink again once the session ends.
func TestMutedPlayerIsNotHeard(t *testing.T) {
	requireSession(t)
	dir := t.TempDir()
	env := append(os.Environ(),
		"SPYTIFY_TARGET_PROCESS=fakeplayer",
		"SPYTIFY_SOCKET="+filepath.Join(dir, "s.sock"),
		"SPYTIFY_CONFIG="+filepath.Join(dir, "config.toml"),
		"XDG_STATE_HOME="+filepath.Join(dir, "state"),
	)
	var playerLog, daemonLog bytes.Buffer
	player := exec.Command(filepath.Join(bin, "fakeplayer"), "--start-delay", startDelay.String(), "--lead-in", leadIn.String(), timelinePath("basic"))
	player.Env, player.Stdout, player.Stderr = env, &playerLog, &playerLog
	if err := player.Start(); err != nil {
		t.Fatal(err)
	}
	defer player.Process.Kill()
	args := []string{"--once", "--idle-exit", "10s", "--wait-target", "10s", "--set", "output.dir=" + filepath.Join(dir, "out"), "--set", "player.mute_output=true"}
	for k, v := range quiet {
		args = append(args, "--set", fmt.Sprintf("%s=%v", k, v))
	}
	daemon := exec.Command(filepath.Join(bin, "spytifyd"), args...)
	daemon.Env, daemon.Stdout, daemon.Stderr = env, &daemonLog, &daemonLog
	if err := daemon.Start(); err != nil {
		t.Fatal(err)
	}
	defer daemon.Process.Kill()

	deadline := time.Now().Add(startDelay + leadIn + 8*time.Second)
	for sinkOf(t, "fakeplayer") != "spytify-muted" {
		if time.Now().After(deadline) {
			t.Fatalf("the player never played into the muted sink (it plays into %q)\nspytifyd:\n%s", sinkOf(t, "fakeplayer"), daemonLog.String())
		}
		time.Sleep(100 * time.Millisecond)
	}
	// Ending the session mid-track puts the player back where it was.
	time.Sleep(time.Second)
	if err := daemon.Process.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if err := daemon.Wait(); err != nil {
		t.Fatalf("spytifyd: %v\n%s", err, daemonLog.String())
	}
	if got := sinkOf(t, "fakeplayer"); got != "spytify-null" {
		t.Errorf("after the session the player plays into %q, want the default sink\nspytifyd:\n%s", got, daemonLog.String())
	}
	if nodeNamed(t, "spytify-muted") {
		t.Error("the muted sink is still there after the session")
	}
}

type dumpObject struct {
	ID   int    `json:"id"`
	Type string `json:"type"`
	Info struct {
		Props      map[string]any `json:"props"`
		OutputNode int            `json:"output-node-id"`
		InputNode  int            `json:"input-node-id"`
	} `json:"info"`
}

func dump(t *testing.T) []dumpObject {
	t.Helper()
	out, err := exec.Command("pw-dump").Output()
	if err != nil {
		t.Fatal(err)
	}
	var objs []dumpObject
	if err := json.Unmarshal(out, &objs); err != nil {
		t.Fatal(err)
	}
	return objs
}

func nodeNamed(t *testing.T, name string) bool {
	for _, o := range dump(t) {
		if o.Type == "PipeWire:Interface:Node" && o.Info.Props["node.name"] == name {
			return true
		}
	}
	return false
}

// sinkOf names the sink the application's output stream is linked to.
func sinkOf(t *testing.T, app string) string {
	objs := dump(t)
	names := map[int]string{}
	stream := -1
	for _, o := range objs {
		if o.Type != "PipeWire:Interface:Node" {
			continue
		}
		p := o.Info.Props
		names[o.ID], _ = p["node.name"].(string)
		if p["media.class"] == "Stream/Output/Audio" && (p["application.name"] == app || p["application.process.binary"] == app) {
			stream = o.ID
		}
	}
	for _, o := range objs {
		if o.Type == "PipeWire:Interface:Link" && o.Info.OutputNode == stream {
			if n := names[o.Info.InputNode]; n != "spytify-capture" {
				return n
			}
		}
	}
	return ""
}
