package target

import (
	"os"
	"os/exec"
	"strconv"
	"strings"
	"testing"
	"time"
)

func TestZombieIsNotListed(t *testing.T) {
	cmd := exec.Command("true")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	// Not waited for: once it exits, it stays a zombie until Wait.
	defer cmd.Wait()
	pid := strconv.Itoa(cmd.Process.Pid)
	deadline := time.Now().Add(2 * time.Second)
	for !zombie(pid) {
		if time.Now().After(deadline) {
			t.Fatal("the exited child never showed as a zombie")
		}
		time.Sleep(10 * time.Millisecond)
	}
	for _, p := range listProc() {
		if p.PID == cmd.Process.Pid {
			t.Fatalf("zombie %d listed as %q", p.PID, p.Name)
		}
	}
}

// A player that plays from a helper process is found through the helper's
// parents: here the test's own child, under the test binary.
func TestMatchesTreeFollowsTheParents(t *testing.T) {
	self, err := os.ReadFile("/proc/self/comm")
	if err != nil {
		t.Skip(err)
	}
	cmd := exec.Command("sleep", "5")
	if err := cmd.Start(); err != nil {
		t.Skip(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	tg := Target{ProcessNames: []string{strings.TrimSpace(string(self))}}
	if !tg.MatchesTree(cmd.Process.Pid) {
		t.Fatal("the child of the target is not in its tree")
	}
	if (Target{ProcessNames: []string{"no-such-player"}}).MatchesTree(cmd.Process.Pid) {
		t.Fatal("another target matched")
	}
}
