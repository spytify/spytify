// Package dllsearch keeps a Windows program from loading DLLs from its own
// folder. Windows looks there first for a DLL asked for by name, so one
// dropped beside the program, in Downloads say, would load in place of
// Windows' own: DLL search-order hijacking.
package dllsearch
