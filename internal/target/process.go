package target

import (
	"bufio"
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Process is a running process that matched one of the target names.
type Process struct {
	PID  int
	Name string
}

// FindProcess returns the first running process whose executable name matches
// one of the target names, ignoring case and a trailing ".exe".
func FindProcess(t Target) (Process, bool) {
	if len(t.ProcessNames) == 0 {
		return Process{}, false
	}
	var procs []Process
	if runtime.GOOS == "linux" {
		procs = listProc()
	} else {
		procs = listPS()
	}
	for _, p := range procs {
		if t.Matches(p.Name) {
			return p, true
		}
	}
	return Process{}, false
}

// Matches reports whether a process or binary name is one of the targets.
func (t Target) Matches(name string) bool {
	name = normalizeName(name)
	if name == "" {
		return false
	}
	for _, want := range t.ProcessNames {
		w := normalizeName(want)
		// /proc/<pid>/comm is truncated to 15 bytes.
		if name == w || (len(name) == 15 && strings.HasPrefix(w, name)) {
			return true
		}
	}
	return false
}

func normalizeName(s string) string {
	s = strings.ToLower(strings.TrimSpace(filepath.Base(s)))
	return strings.TrimSuffix(s, ".exe")
}

func listProc() []Process {
	entries, err := os.ReadDir("/proc")
	if err != nil {
		return nil
	}
	var out []Process
	for _, e := range entries {
		pid, err := strconv.Atoi(e.Name())
		if err != nil {
			continue
		}
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "cmdline")); err == nil && len(b) > 0 {
			argv0 := string(bytes.SplitN(b, []byte{0}, 2)[0])
			out = append(out, Process{PID: pid, Name: filepath.Base(argv0)})
		}
		if b, err := os.ReadFile(filepath.Join("/proc", e.Name(), "comm")); err == nil {
			out = append(out, Process{PID: pid, Name: strings.TrimSpace(string(b))})
		}
	}
	return out
}

func listPS() []Process {
	out, err := exec.Command("ps", "-axo", "pid=,comm=").Output()
	if err != nil {
		return nil
	}
	var procs []Process
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		pid, err := strconv.Atoi(fields[0])
		if err != nil {
			continue
		}
		procs = append(procs, Process{PID: pid, Name: filepath.Base(strings.Join(fields[1:], " "))})
	}
	return procs
}
