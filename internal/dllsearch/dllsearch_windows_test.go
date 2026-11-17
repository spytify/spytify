package dllsearch

import (
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/sys/windows"
)

// A copy of a System32 DLL beside the program, asked for by name, does not
// load: System32's does. xinput1_4.dll is one GLFW loads by name, and
// not one of Windows' KnownDLLs, which never load from elsewhere.
func TestADLLBesideTheProgramDoesNotLoad(t *testing.T) {
	const dll = "xinput1_4.dll"
	system32, err := windows.GetSystemDirectory()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(system32, dll)); err != nil {
		t.Skipf("no %s in System32: %v", dll, err)
	}
	name, _ := windows.UTF16PtrFromString(dll)
	var h windows.Handle
	if windows.GetModuleHandleEx(windows.GET_MODULE_HANDLE_EX_FLAG_UNCHANGED_REFCOUNT, name, &h) == nil {
		t.Skipf("%s is loaded already", dll)
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	planted := filepath.Join(filepath.Dir(exe), dll)
	copyFile(t, filepath.Join(system32, dll), planted)
	t.Cleanup(func() { os.Remove(planted) })

	if err := System32Only(); err != nil {
		t.Fatal(err)
	}
	h, err = windows.LoadLibrary(dll)
	if err != nil {
		t.Fatal(err)
	}
	defer windows.FreeLibrary(h)
	buf := make([]uint16, windows.MAX_LONG_PATH)
	n, err := windows.GetModuleFileName(h, &buf[0], uint32(len(buf)))
	if err != nil {
		t.Fatal(err)
	}
	if got := windows.UTF16ToString(buf[:n]); !strings.EqualFold(filepath.Dir(got), system32) {
		t.Errorf("%s loaded from %s, not System32 (%s)", dll, got, system32)
	}
}

func copyFile(t *testing.T, from, to string) {
	t.Helper()
	in, err := os.Open(from)
	if err != nil {
		t.Fatal(err)
	}
	defer in.Close()
	out, err := os.Create(to)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(out, in); err != nil {
		out.Close()
		t.Fatal(err)
	}
	if err := out.Close(); err != nil {
		t.Fatal(err)
	}
}
