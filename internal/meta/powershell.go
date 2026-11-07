package meta

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
)

// PowerShell is Windows PowerShell's path: in its own folder under the
// system folder, where every Windows 10 and 11 install has it, or else
// where PATH finds it. A PATH can lose that folder (an installer or the
// user rewrote it), so PATH alone is not enough.
func PowerShell() (string, error) {
	return findPowerShell(systemDir(), exec.LookPath)
}

func findPowerShell(system string, lookPath func(string) (string, error)) (string, error) {
	var dir string
	if system != "" {
		dir = filepath.Join(system, "WindowsPowerShell", "v1.0")
		p := filepath.Join(dir, "powershell.exe")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	p, err := lookPath("powershell.exe")
	if err != nil && dir != "" {
		return "", fmt.Errorf("not in %s, and %w", dir, err)
	}
	return p, err
}
