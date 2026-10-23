//go:build !darwin

package target

func bundleIDOf(string) string { return "" }
