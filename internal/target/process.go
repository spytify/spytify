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
	// Parent is the parent's PID where the listing gives it (Windows), else 0.
	Parent int
}

// FindProcess returns the running process whose executable name matches one
// of the target names, ignoring case and a trailing ".exe". Of a player that
// runs as a tree of processes of the same name, it returns the root, whose
// tree holds the one that plays.
func FindProcess(t Target) (Process, bool) {
	if len(t.ProcessNames) == 0 {
		return Process{}, false
	}
	var procs []Process
	switch runtime.GOOS {
	case "linux":
		procs = listProc()
	case "windows":
		procs = listToolhelp()
	default:
		procs = listPS()
	}
	return pickRoot(t, procs)
}

// pickRoot returns the first matching process whose parent does not match.
func pickRoot(t Target, procs []Process) (Process, bool) {
	matching := map[int]bool{}
	var first *Process
	for i, p := range procs {
		if t.Matches(p.Name) {
			matching[p.PID] = true
			if first == nil {
				first = &procs[i]
			}
		}
	}
	for _, p := range procs {
		if matching[p.PID] && !matching[p.Parent] {
			return p, true
		}
	}
	if first != nil {
		return *first, true
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

// MatchesTree reports whether pid or one of its ancestors is a target
// process, for players that play audio from a helper process. It needs
// /proc and returns false elsewhere.
func (t Target) MatchesTree(pid int) bool {
	for depth := 0; pid > 1 && depth < 16; depth++ {
		if b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "comm")); err == nil && t.Matches(string(b)) {
			return true
		}
		if b, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "cmdline")); err == nil && len(b) > 0 {
			if t.Matches(string(bytes.SplitN(b, []byte{0}, 2)[0])) {
				return true
			}
		}
		stat, err := os.ReadFile(filepath.Join("/proc", strconv.Itoa(pid), "stat"))
		if err != nil {
			return false
		}
		// The command name is in parentheses and may contain spaces.
		rest := stat[bytes.LastIndexByte(stat, ')')+1:]
		fields := strings.Fields(string(rest))
		if len(fields) < 2 {
			return false
		}
		if pid, err = strconv.Atoi(fields[1]); err != nil {
			return false
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
		if err != nil || zombie(e.Name()) {
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

// zombie reports a process that exited but was not reaped: it plays nothing.
func zombie(pid string) bool {
	b, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
	if err != nil {
		return false
	}
	// "pid (comm) state ...": comm may hold spaces and parentheses.
	i := bytes.LastIndexByte(b, ')')
	return i >= 0 && i+2 < len(b) && b[i+2] == 'Z'
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
