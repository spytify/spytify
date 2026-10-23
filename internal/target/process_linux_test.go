package target

import (
	"os/exec"
	"strconv"
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
