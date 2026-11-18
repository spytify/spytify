package main

import (
	"bytes"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// tail gives up on a recorder that takes the connection but never answers,
// instead of waiting for ever.
func TestTailGivesUpOnARecorderThatDoesNotAnswer(t *testing.T) {
	defer func(d time.Duration) { connectTimeout = d }(connectTimeout)
	connectTimeout = 300 * time.Millisecond
	// macOS caps socket paths at 104 bytes, shorter than t.TempDir().
	dir, err := os.MkdirTemp("", "spy")
	if err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(dir)
	path := filepath.Join(dir, "s.sock")
	ln, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
		}
	}()
	root := newRoot(&bytes.Buffer{})
	root.SetArgs([]string{"--socket", path, "tail", "-f"})
	done := make(chan error, 1)
	go func() { done <- root.Execute() }()
	select {
	case err := <-done:
		if err == nil || !strings.Contains(err.Error(), "did not answer") {
			t.Fatalf("tail ended with %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("tail still waits after 5 s")
	}
}
