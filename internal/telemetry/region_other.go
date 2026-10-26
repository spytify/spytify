//go:build !darwin && !windows

package telemetry

// osLocale has nothing to add to the locale variables.
func osLocale() string { return "" }
