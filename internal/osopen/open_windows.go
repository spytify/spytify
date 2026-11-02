package osopen

import "github.com/spytify/spytify/internal/proc"

// Open opens a file or folder with its default application.
func Open(path string) error {
	return proc.Command("cmd", "/C", "start", "", path).Start()
}
