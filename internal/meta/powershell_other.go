//go:build !windows

package meta

// systemDir is empty: only Windows has Windows PowerShell's folder.
func systemDir() string { return "" }
