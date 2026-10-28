package capture

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// rateDriver runs the functions of windows/spytify-tap/rate.h on lines read
// from stdin and prints each answer; "-" is an empty endpoint id.
const rateDriver = `#include "rate.h"

#include <iostream>
#include <sstream>

static std::wstring id(const std::string &s) { return s == "-" ? std::wstring() : std::wstring(s.begin(), s.end()); }
static std::string text(const std::wstring &s) { return s.empty() ? "-" : std::string(s.begin(), s.end()); }

int main() {
  const char *from[] = {"given", "player", "default", "fallback"};
  std::string line;
  while (std::getline(std::cin, line)) {
    std::istringstream in(line);
    std::string op, a, b, c, d;
    in >> op;
    if (op == "rate") {
      unsigned long given, player, byDefault;
      in >> given >> player >> byDefault;
      Rate r = chooseRate(given, player, byDefault);
      std::cout << r.hz << " " << from[static_cast<int>(r.from)] << "\n";
    } else if (op == "player") {
      in >> a >> b;
      std::vector<std::wstring> sessions;
      while (in >> c) sessions.push_back(id(c));
      std::cout << text(playerDevice(id(a), sessions, id(b))) << "\n";
    } else if (op == "moved") {
      in >> a >> b >> c >> d;
      std::cout << (moved(id(a), id(b), id(c), id(d)) ? "moved" : "stays") << "\n";
    } else {
      std::cout << "?\n";
    }
  }
}
`

// rateProgram builds rateDriver with this computer's C++ compiler and the
// flags of scripts/build-windows.sh: rate.h calls no Windows API.
func rateProgram(t *testing.T) string {
	t.Helper()
	var cxx string
	for _, name := range []string{"c++", "clang++", "g++"} {
		if p, err := exec.LookPath(name); err == nil {
			cxx = p
			break
		}
	}
	if cxx == "" {
		t.Skip("no C++ compiler to build the rate choice of spytify-tap.exe")
	}
	dir := t.TempDir()
	src := filepath.Join(dir, "rate.cpp")
	if err := os.WriteFile(src, []byte(rateDriver), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "rate")
	if runtime.GOOS == "windows" {
		out += ".exe"
	}
	inc, err := filepath.Abs(filepath.Join("..", "..", "windows", "spytify-tap"))
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(cxx, "-O2", "-Wall", "-Wextra", "-std=c++17", "-I", inc, "-o", out, src)
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%s: %v\n%s", filepath.Base(cxx), err, b)
	}
	return out
}

// spytify-tap.exe records at the mix rate of the device the player plays
// to: Windows already converted the player's audio to it, and any other rate
// converts it a second time.
func TestTapRateFollowsThePlayersDevice(t *testing.T) {
	cases := []struct{ name, in, want string }{
		{"--rate is asked for as it is", "rate 44100 48000 48000", "44100 given"},
		{"the mix rate of the device the player plays to", "rate 0 44100 48000", "44100 player"},
		{"a headset in a call shows its rate", "rate 0 16000 48000", "16000 player"},
		{"the default device while the player's gives no rate", "rate 0 0 44100", "44100 default"},
		{"48 kHz when no device answers", "rate 0 0 0", "48000 fallback"},

		{"the device it plays to now", "player B D A B D", "B"},
		{"paused, its session on the default device first", "player - D A D", "D"},
		{"paused, its session on another device", "player - D A", "A"},
		{"no session yet", "player - D", "-"},

		{"still plays to the device the rate came from", "moved A A D D", "stays"},
		{"plays to another device", "moved B A D D", "moved"},
		{"paused, the default device unchanged", "moved - A D D", "stays"},
		{"paused, another default device", "moved - A E D", "moved"},
		{"still plays there after the default changed", "moved A A E D", "stays"},
	}
	var in strings.Builder
	for _, c := range cases {
		in.WriteString(c.in + "\n")
	}
	cmd := exec.Command(rateProgram(t))
	cmd.Stdin = strings.NewReader(in.String())
	out, err := cmd.Output()
	if err != nil {
		t.Fatal(err)
	}
	got := strings.Split(strings.TrimSpace(string(out)), "\n")
	if len(got) != len(cases) {
		t.Fatalf("%d answers for %d cases:\n%s", len(got), len(cases), out)
	}
	for i, c := range cases {
		// A Windows build writes \r\n.
		if g := strings.TrimSpace(got[i]); g != c.want {
			t.Errorf("%s: %q gives %q, want %q", c.name, c.in, g, c.want)
		}
	}
}
