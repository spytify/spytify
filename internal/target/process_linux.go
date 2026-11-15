package target

import (
	"bytes"
	"os"
	"path/filepath"
	"strconv"
)

// Running reports whether pid is still a live process of the target,
// reading that pid's /proc entries alone.
func Running(t Target, pid int) bool {
	name, argv0, ok := procNames(strconv.Itoa(pid))
	return ok && (t.Matches(name) || t.Matches(argv0))
}

// procNames reads a process's comm, from its stat, and the first word of
// its command line; ok is false once it is gone or a zombie.
func procNames(pid string) (comm, argv0 string, ok bool) {
	stat, err := os.ReadFile(filepath.Join("/proc", pid, "stat"))
	if err != nil {
		return "", "", false
	}
	// "pid (comm) state ...": comm may hold spaces and parentheses.
	open, end := bytes.IndexByte(stat, '('), bytes.LastIndexByte(stat, ')')
	if open < 0 || end < open || end+2 >= len(stat) || stat[end+2] == 'Z' {
		return "", "", false
	}
	comm = string(stat[open+1 : end])
	if b, err := os.ReadFile(filepath.Join("/proc", pid, "cmdline")); err == nil && len(b) > 0 {
		argv0 = filepath.Base(string(bytes.SplitN(b, []byte{0}, 2)[0]))
	}
	return comm, argv0, true
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
		// Two reads a pid, stat and cmdline, where comm was a third (#835).
		comm, argv0, ok := procNames(e.Name())
		if !ok {
			continue
		}
		if argv0 != "" {
			out = append(out, Process{PID: pid, Name: argv0})
		}
		out = append(out, Process{PID: pid, Name: comm})
	}
	return out
}
