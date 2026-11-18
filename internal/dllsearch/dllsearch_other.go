//go:build !windows

package dllsearch

// System32Only does nothing outside Windows.
func System32Only() error { return nil }
