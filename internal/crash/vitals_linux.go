package crash

import (
	"bufio"
	"os"
	"strconv"
	"strings"
)

// osVitals are the process's resident memory, threads and open files,
// then the computer's memory, what is left of it, and the commit charge
// and its limit, which the kernel enforces only when overcommit is off.
func osVitals() []vital {
	var v []vital
	v = append(v, procFields("/proc/self/status", map[string]string{
		"VmRSS":   "process.rss_bytes",
		"Threads": "process.threads",
	})...)
	if fds, err := os.ReadDir("/proc/self/fd"); err == nil {
		// ReadDir's own descriptor is among them.
		v = append(v, vital{"process.fds", uint64(len(fds) - 1)})
	}
	return append(v, procFields("/proc/meminfo", map[string]string{
		"MemTotal":     "system.physical_bytes",
		"MemAvailable": "system.physical_available_bytes",
		"CommitLimit":  "system.commit_limit_bytes",
		"Committed_AS": "system.commit_bytes",
	})...)
}

// procFields reads the fields of a /proc file such as "VmRSS:  1234 kB",
// keyed as keys says, in bytes when given in kB.
func procFields(file string, keys map[string]string) []vital {
	f, err := os.Open(file)
	if err != nil {
		return nil
	}
	defer f.Close()
	var v []vital
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		name, rest, _ := strings.Cut(sc.Text(), ":")
		key, ok := keys[name]
		fields := strings.Fields(rest)
		if !ok || len(fields) == 0 {
			continue
		}
		n, err := strconv.ParseUint(fields[0], 10, 64)
		if err != nil {
			continue
		}
		if len(fields) > 1 && fields[1] == "kB" {
			n *= 1024
		}
		v = append(v, vital{key, n})
	}
	return v
}
