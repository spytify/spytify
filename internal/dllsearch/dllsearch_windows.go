package dllsearch

import "golang.org/x/sys/windows"

// System32Only makes System32 the one folder Windows looks in for a DLL
// loaded by name, for the rest of the process: GLFW's, Fyne's, and those
// the system DLLs load in turn. It must run before any of them, first in
// main. LOAD_LIBRARY_SEARCH_DEFAULT_DIRS would keep the program's folder,
// searched before System32. A DLL loaded by its full path, as Mesa's,
// still loads.
func System32Only() error {
	return windows.SetDefaultDllDirectories(windows.LOAD_LIBRARY_SEARCH_SYSTEM32)
}
