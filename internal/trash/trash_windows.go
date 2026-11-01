package trash

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"unsafe"

	"golang.org/x/sys/windows"
)

var shFileOperation = windows.NewLazySystemDLL("shell32.dll").NewProc("SHFileOperationW")

// shFileOpStruct is SHFILEOPSTRUCTW, laid out as on 64-bit Windows: 32-bit
// Windows packs it to the byte, and Spytify ships no 32-bit build.
type shFileOpStruct struct {
	hwnd                  uintptr
	wFunc                 uint32
	pFrom                 *uint16
	pTo                   *uint16
	fFlags                uint16
	fAnyOperationsAborted int32
	hNameMappings         uintptr
	lpszProgressTitle     *uint16
}

const (
	foDelete           = 0x0003
	fofSilent          = 0x0004
	fofNoConfirmation  = 0x0010
	fofAllowUndo       = 0x0040
	fofNoErrorUI       = 0x0400
	fofWantNukeWarning = 0x4000
)

// Move moves the file at path to the Recycle Bin, without a question or a
// progress window. A file the Recycle Bin cannot take, too big for it or
// on a drive without one, would be deleted for good: Windows asks first
// then, and the file stays when the answer is no.
func Move(path string) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(abs); err != nil {
		return err
	}
	if err := shFileOperation.Find(); err != nil {
		return err
	}
	from, err := windows.UTF16FromString(abs)
	if err != nil {
		return err
	}
	// pFrom is a list of names that ends with an empty one.
	from = append(from, 0)
	op := shFileOpStruct{
		wFunc:  foDelete,
		pFrom:  &from[0],
		fFlags: fofAllowUndo | fofNoConfirmation | fofSilent | fofNoErrorUI | fofWantNukeWarning,
	}
	if r, _, _ := shFileOperation.Call(uintptr(unsafe.Pointer(&op))); r != 0 {
		return &os.PathError{Op: "trash", Path: path, Err: fmt.Errorf("SHFileOperation error %#x", r)}
	}
	if op.fAnyOperationsAborted != 0 {
		return &os.PathError{Op: "trash", Path: path, Err: errors.New("the Recycle Bin cannot take it, and it was not deleted")}
	}
	return nil
}
