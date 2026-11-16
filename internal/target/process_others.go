//go:build !darwin && !linux

package target

// Running cannot tell here: the caller lists every process instead.
func Running(Target, int) bool { return false }
