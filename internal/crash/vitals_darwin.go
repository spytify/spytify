package crash

import (
	"syscall"

	"golang.org/x/sys/unix"
)

// osVitals are the most resident memory the process had, which the
// system gives without asking the kernel for its tasks, and the
// computer's memory.
func osVitals() []vital {
	var v []vital
	var ru syscall.Rusage
	if syscall.Getrusage(syscall.RUSAGE_SELF, &ru) == nil {
		// In bytes on macOS, in kB elsewhere.
		v = append(v, vital{"process.max_rss_bytes", uint64(ru.Maxrss)})
	}
	if n, err := unix.SysctlUint64("hw.memsize"); err == nil {
		v = append(v, vital{"system.physical_bytes", n})
	}
	return v
}
