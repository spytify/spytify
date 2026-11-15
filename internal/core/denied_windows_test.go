package core

import (
	"os"
	"syscall"
	"testing"
)

// Windows' "Access is denied", what ransomware protection answers a
// program it does not allow, is a denied write; a folder that cannot be
// found is not (#887).
func TestAccessIsDeniedIsDeniedOnWindows(t *testing.T) {
	if !denied(&os.PathError{Op: "mkdir", Path: `C:\Users\u\Music\Spytify`, Err: syscall.ERROR_ACCESS_DENIED}) {
		t.Error("ERROR_ACCESS_DENIED not denied")
	}
	if denied(&os.PathError{Op: "mkdir", Path: `E:\Spytify`, Err: syscall.ERROR_PATH_NOT_FOUND}) {
		t.Error("ERROR_PATH_NOT_FOUND denied")
	}
}
