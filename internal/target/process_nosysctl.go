//go:build !darwin

package target

func listSysctl() []Process { return nil }
