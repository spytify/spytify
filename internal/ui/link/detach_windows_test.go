package link

import (
	"os/exec"
	"testing"
)

// The recorder starts detached from the window but not hidden: a hidden
// window flag on a background process is what behaviour engines score, and
// DETACHED_PROCESS already gives it no console.
func TestDetachStartsTheRecorderNotHidden(t *testing.T) {
	cmd := exec.Command("spytifyd.exe")
	detach(cmd)
	if cmd.SysProcAttr == nil {
		t.Fatal("no SysProcAttr")
	}
	if cmd.SysProcAttr.HideWindow {
		t.Error("HideWindow is set")
	}
	const createNewProcessGroup, detachedProcess = 0x00000200, 0x00000008
	if got, want := cmd.SysProcAttr.CreationFlags, uint32(createNewProcessGroup|detachedProcess); got != want {
		t.Errorf("CreationFlags = %#x, want %#x (CREATE_NEW_PROCESS_GROUP|DETACHED_PROCESS)", got, want)
	}
}
