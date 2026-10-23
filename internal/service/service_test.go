package service

import (
	"encoding/xml"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSystemdUnit(t *testing.T) {
	p, err := For("linux", "/opt/My Apps/spytifyd%1", "/home/u", 1000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != filepath.Join("/home/u", ".config", "systemd", "user", "spytifyd.service") {
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
	if p.Path != filepath.Join("/Users/u", "Library", "LaunchAgents", Label+".plist") {
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
