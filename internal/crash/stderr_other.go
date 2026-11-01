//go:build !windows

package crash

// CaptureStderr does nothing outside Windows: a program started without a
// terminal still has a stderr there, the session's log on Linux, or
// /dev/null for an app opened from the Finder.
func CaptureStderr(string) {}
