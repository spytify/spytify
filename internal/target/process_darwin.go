package target

import (
	"bytes"
	"os"
	"path/filepath"
	"slices"

	"golang.org/x/sys/unix"
)

// sZomb is p_stat for a process that exited but was not reaped.
const sZomb = 5

// listSysctl lists every process with its parent from the kernel, without
// starting ps every poll (#835). Each of the user's processes is listed
// under its first argument too, the name ps showed, which differs from the
// kernel's p_comm for a program run through a link or one that renames
// itself; p_comm is cut to 16 bytes, which Matches allows for.
func listSysctl() []Process {
	kps, err := unix.SysctlKinfoProcSlice("kern.proc.all")
	if err != nil {
		return nil
	}
	uid := uint32(os.Getuid())
	out := make([]Process, 0, len(kps))
	for _, kp := range kps {
		if kp.Proc.P_stat == sZomb {
			continue
		}
		p := Process{PID: int(kp.Proc.P_pid), Parent: int(kp.Eproc.Ppid), Name: unix.ByteSliceToString(kp.Proc.P_comm[:])}
		if kp.Eproc.Pcred.P_ruid == uid {
			if a := argv0(p.PID); a != "" && a != p.Name {
				out = append(out, Process{PID: p.PID, Parent: p.Parent, Name: a})
			}
		}
		out = append(out, p)
	}
	// In pid order, as ps lists them, so the first match is the same.
	slices.SortStableFunc(out, func(a, b Process) int { return a.PID - b.PID })
	return out
}

// argv0 is the base name of a process's first argument, "" when the
// kernel does not say (another user's process).
func argv0(pid int) string {
	b, err := unix.SysctlRaw("kern.procargs2", pid)
	if err != nil || len(b) < 4 {
		return ""
	}
	// argc, the executable's path, NUL padding, then the arguments.
	b = b[4:]
	i := bytes.IndexByte(b, 0)
	if i < 0 {
		return ""
	}
	b = bytes.TrimLeft(b[i:], "\x00")
	if j := bytes.IndexByte(b, 0); j >= 0 {
		b = b[:j]
	}
	if len(b) == 0 {
		return ""
	}
	return filepath.Base(string(b))
}

// Running reports whether pid is still a live process of the target,
// asking the kernel about that pid alone.
func Running(t Target, pid int) bool {
	kp, err := unix.SysctlKinfoProc("kern.proc.pid", pid)
	if err != nil || int(kp.Proc.P_pid) != pid || kp.Proc.P_stat == sZomb {
		return false
	}
	return t.Matches(unix.ByteSliceToString(kp.Proc.P_comm[:])) || t.Matches(argv0(pid))
}
