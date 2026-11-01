//go:build darwin && cgo

package trash

/*
#cgo CFLAGS: -x objective-c
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
char *spytifyTrash(const char *path, char **trashed);
*/
import "C"

import (
	"errors"
	"os"
	"path/filepath"
	"unsafe"
)

// Move moves the file at path to the Trash of its volume, through
// NSFileManager, which needs no permission for it: moving the file into
// ~/.Trash by hand needs Full Disk Access, and asking the Finder to needs
// Automation.
func Move(path string) error {
	_, err := move(path)
	return err
}

// move returns where the file went in the Trash.
func move(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	if _, err := os.Lstat(abs); err != nil {
		return "", err
	}
	cpath := C.CString(abs)
	defer C.free(unsafe.Pointer(cpath))
	var trashed *C.char
	if why := C.spytifyTrash(cpath, &trashed); why != nil {
		defer C.free(unsafe.Pointer(why))
		return "", &os.PathError{Op: "trash", Path: path, Err: errors.New(C.GoString(why))}
	}
	defer C.free(unsafe.Pointer(trashed))
	return C.GoString(trashed), nil
}
