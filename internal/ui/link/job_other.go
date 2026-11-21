//go:build !windows

package link

import "os"

func contain(*os.Process) error { return nil }

func release() error { return nil }
