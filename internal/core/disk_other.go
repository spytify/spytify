//go:build !unix

package core

const minFreeBytes = 256 << 20

func freeBytes(string) (int64, bool) { return 0, false }
