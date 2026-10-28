package capture

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// spytify-tap.exe writes each captured packet where its timestamp says, so
// the stream follows the clock; timestamps off by a frame on continuous audio
// once cut a frame out or put a silent one in. Where a packet goes is plain
// C++ (windows/spytify-tap/placement.h), so its test program builds with this
// computer's C++ compiler and the warnings of scripts/build-windows.sh.
func TestWindowsTapPlacesPacketsOnTheClock(t *testing.T) {
	cxx := ""
	for _, c := range []string{os.Getenv("CXX"), "c++", "g++", "clang++"} {
		if c == "" {
			continue
		}
		if _, err := exec.LookPath(c); err == nil {
			cxx = c
			break
		}
	}
	if cxx == "" {
		t.Skip("no C++ compiler (c++, g++ or clang++)")
	}
	src := filepath.Join("..", "..", "windows", "spytify-tap", "placement_test.cpp")
	prog := filepath.Join(t.TempDir(), "placement_test")
	if runtime.GOOS == "windows" {
		prog += ".exe"
	}
	b, err := exec.Command(cxx, "-O2", "-Wall", "-Wextra", "-std=c++17", "-o", prog, src).CombinedOutput()
	if err != nil {
		t.Fatalf("%s: %v\n%s", cxx, err, b)
	}
	if len(b) > 0 {
		t.Logf("%s:\n%s", cxx, b)
	}

	out, err := exec.Command(prog).Output()
	cases := 0
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		status, rest, _ := strings.Cut(line, " ")
		name, why, _ := strings.Cut(rest, ": ")
		switch status {
		case "ok":
			cases++
			t.Run(name, func(t *testing.T) {})
		case "FAIL":
			cases++
			t.Run(name, func(t *testing.T) { t.Error(why) })
		}
	}
	if cases == 0 || (err != nil && !t.Failed()) {
		t.Fatalf("placement_test: %v\n%s", err, out)
	}
}
