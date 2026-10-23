//go:build unix

package core

import "syscall"

// minFreeBytes is the free space below which a new track is not started.
const minFreeBytes = 256 << 20

func freeBytes(dir string) (int64, bool) {
	var st syscall.Statfs_t
	if err := syscall.Statfs(dir, &st); err != nil {
		return 0, false
	}
	return int64(st.Bavail) * int64(st.Bsize), true
}
