package service

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
)

func TestSystemdUnit(t *testing.T) {
	p, err := For("linux", "/opt/My Apps/spytifyd%1", "/home/u", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "/home/u/.config/systemd/user/spytifyd.service" {
		t.Errorf("path %s", p.Path)
	}
	unit := string(p.Content)
	for _, want := range []string{`ExecStart="/opt/My Apps/spytifyd%%1"`, "Restart=on-failure", "WantedBy=default.target"} {
		if !strings.Contains(unit, want) {
			t.Errorf("unit lacks %q:\n%s", want, unit)
		}
	}
	if !reflect.DeepEqual(p.Install[1], []string{"systemctl", "--user", "enable", "--now", "spytifyd.service"}) {
		t.Errorf("install %v", p.Install)
	}
}

func TestLaunchdAgent(t *testing.T) {
	p, err := For("darwin", "/Applications/A & B.app/Contents/MacOS/spytifyd", "/Users/u", 501)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "/Users/u/Library/LaunchAgents/"+Label+".plist" {
		t.Errorf("path %s", p.Path)
	}
	// The plist is well-formed XML with the path escaped once.
	dec := xml.NewDecoder(strings.NewReader(string(p.Content)))
	dec.Strict = false
	var texts []string
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		if cd, ok := tok.(xml.CharData); ok && strings.TrimSpace(string(cd)) != "" {
			texts = append(texts, string(cd))
		}
	}
	if !contains(texts, "/Applications/A & B.app/Contents/MacOS/spytifyd") || !contains(texts, Label) || !contains(texts, "SuccessfulExit") {
		t.Fatalf("plist texts %v", texts)
	}
	if !reflect.DeepEqual(p.Install[0], []string{"launchctl", "bootstrap", "gui/501", p.Path}) {
		t.Errorf("install %v", p.Install)
	}
	// Where Apple's validator exists, it accepts the agent.
	if lint, err := exec.LookPath("plutil"); err == nil {
		f := filepath.Join(t.TempDir(), "agent.plist")
		if err := os.WriteFile(f, p.Content, 0o644); err != nil {
			t.Fatal(err)
		}
		if out, err := exec.Command(lint, "-lint", f).CombinedOutput(); err != nil {
			t.Fatalf("plutil: %v: %s", err, out)
		}
	}
}

func TestOtherOSesAndRelativePaths(t *testing.T) {
	if _, err := For("windows", `C:\spytifyd.exe`, `C:\Users\u`, 0); !errors.Is(err, ErrUnsupported) {
		t.Errorf("windows: %v", err)
	}
	if _, err := For("linux", "spytifyd", "/home/u", 1000); err == nil {
		t.Error("relative daemon path accepted")
	}
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if x == s {
			return true
		}
	}
	return false
}

// Install writes the file then runs its commands in order; Uninstall runs
// its commands then removes the file. Harmless commands stand in for
// systemctl and launchctl, which would change the machine running the test.
func TestInstallAndUninstallRunThePlan(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the plan's commands are POSIX")
	}
	dir := t.TempDir()
	order := filepath.Join(dir, "order")
	p := Plan{
		Path:      filepath.Join(dir, "agents", "spytifyd.service"),
		Content:   []byte("[Service]\n"),
		Install:   [][]string{{"sh", "-c", "test -f " + filepath.Join(dir, "agents", "spytifyd.service") + " && echo installed >> " + order}},
		Uninstall: [][]string{{"sh", "-c", "test -f " + filepath.Join(dir, "agents", "spytifyd.service") + " && echo stopped >> " + order}},
	}
	if Installed(p) {
		t.Fatal("installed before Install")
	}
	if err := Install(p); err != nil {
		t.Fatal(err)
	}
	if !Installed(p) {
		t.Fatal("not installed after Install")
	}
	if err := Uninstall(p); err != nil {
		t.Fatal(err)
	}
	if Installed(p) {
		t.Fatal("still installed after Uninstall")
	}
	if b, _ := os.ReadFile(order); string(b) != "installed\nstopped\n" {
		t.Fatalf("commands ran as %q, want install with the file there, then stop before it went", b)
	}
	if err := Uninstall(p); err != nil {
		t.Fatalf("uninstalling twice: %v", err)
	}
}

// A failing command says which one and what it printed.
func TestAFailingCommandIsReported(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the plan's commands are POSIX")
	}
	p := Plan{Path: filepath.Join(t.TempDir(), "unit"), Install: [][]string{{"sh", "-c", "echo no bus >&2; exit 3"}}}
	err := Install(p)
	if err == nil || !strings.Contains(err.Error(), "sh -c") || !strings.Contains(err.Error(), "no bus") {
		t.Fatalf("error %v", err)
	}
}
