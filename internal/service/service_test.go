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

// With the .deb or .rpm installed, service install enables the packaged
// unit and writes no second one in the user's folder, which would shadow
// it. It removes the one an earlier install wrote, unless the user edited
// it.
func TestThePackagedUnitIsEnabledNotWritten(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the plan's commands are POSIX")
	}
	dir := t.TempDir()
	old := packagedUnit
	packagedUnit = filepath.Join(dir, "usr-lib", "spytifyd.service")
	t.Cleanup(func() { packagedUnit = old })
	if err := os.MkdirAll(filepath.Dir(packagedUnit), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(packagedUnit, []byte("[Service]\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	home := filepath.Join(dir, "home")

	p, err := For("linux", "/usr/bin/spytifyd", home, 1000)
	if err != nil {
		t.Fatal(err)
	}
	if p.Path != "" || p.Content != nil || p.Packaged != packagedUnit {
		t.Fatalf("plan writes %q (packaged %q), want no file and the packaged unit", p.Path, p.Packaged)
	}
	if !reflect.DeepEqual(p.Install, [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "reenable", "spytifyd.service"}, {"systemctl", "--user", "start", "spytifyd.service"}}) {
		t.Errorf("install %v", p.Install)
	}
	if !reflect.DeepEqual(p.Uninstall, [][]string{{"systemctl", "--user", "disable", "--now", "spytifyd.service"}}) {
		t.Errorf("uninstall %v", p.Uninstall)
	}

	// A spytifyd of its own, built or unpacked elsewhere, still gets the
	// written unit.
	if q, _ := For("linux", "/opt/spytify/spytifyd", home, 1000); q.Path == "" || q.Packaged != "" {
		t.Errorf("own spytifyd: path %q, packaged %q", q.Path, q.Packaged)
	}

	// Install runs the commands and writes nothing; the unit an earlier
	// install wrote goes, before and after the hardening.
	user := filepath.Join(home, ".config", "systemd", "user", "spytifyd.service")
	for i, earlier := range p.StaleContents {
		if err := os.MkdirAll(filepath.Dir(user), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(user, earlier, 0o644); err != nil {
			t.Fatal(err)
		}
		ran := filepath.Join(dir, "ran")
		_ = os.Remove(ran)
		q := p
		q.Install = [][]string{{"sh", "-c", "echo enabled >> " + ran}}
		if err := Install(q); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(user); !errors.Is(err, os.ErrNotExist) {
			t.Errorf("earlier unit %d still in the user's folder: %v", i, err)
		}
		if b, _ := os.ReadFile(ran); string(b) != "enabled\n" {
			t.Errorf("commands ran as %q", b)
		}
	}
	if !strings.Contains(string(p.StaleContents[1]), "RestartSec=5\n\n[Install]") {
		t.Errorf("the unit before the hardening is not the one earlier installs wrote:\n%s", p.StaleContents[1])
	}
	edited := []byte("[Service]\nExecStart=/usr/bin/spytifyd --verbose\n")
	if err := os.WriteFile(user, edited, 0o644); err != nil {
		t.Fatal(err)
	}
	q := p
	q.Install = nil
	if err := Install(q); err != nil {
		t.Fatal(err)
	}
	if b, err := os.ReadFile(user); err != nil || string(b) != string(edited) {
		t.Errorf("an edited unit was removed or changed: %q, %v", b, err)
	}
	entries, _ := os.ReadDir(filepath.Dir(user))
	if len(entries) != 1 {
		t.Errorf("install wrote in the user's folder: %v", entries)
	}

	// Installed asks systemd whether the packaged unit is enabled.
	q.Enabled = []string{"true"}
	if !Installed(q) {
		t.Error("enabled packaged unit reported not installed")
	}
	q.Enabled = []string{"false"}
	if Installed(q) {
		t.Error("disabled packaged unit reported installed")
	}
}

// The packaged unit and the one service install writes carry the same
// hardening, none of it needing a user namespace (ProtectSystem, PrivateTmp
// and the like would hide output folders and /tmp from spytifyd).
func TestBothUnitsAreHardened(t *testing.T) {
	packaged, err := os.ReadFile(filepath.Join("..", "..", "packaging", "linux", "spytifyd.service"))
	if err != nil {
		t.Fatal(err)
	}
	p, err := For("linux", "/opt/spytify/spytifyd", "/home/u", 1000)
	if err != nil {
		t.Fatal(err)
	}
	// Windows CI checks the repository out with CRLF line ends.
	for name, unit := range map[string]string{"packaged": strings.ReplaceAll(string(packaged), "\r\n", "\n"), "written": string(p.Content)} {
		lines := strings.Split(unit, "\n")
		for _, want := range []string{
			"NoNewPrivileges=yes",
			"LockPersonality=yes",
			"RestrictSUIDSGID=yes",
			"SystemCallArchitectures=native",
			"RestrictAddressFamilies=AF_UNIX AF_NETLINK AF_INET AF_INET6",
		} {
			if !contains(lines, want) {
				t.Errorf("%s unit lacks %q", name, want)
			}
		}
		for _, l := range lines {
			for _, ns := range []string{"ProtectSystem=", "ProtectHome=", "PrivateTmp=", "PrivateUsers=", "ReadWritePaths="} {
				if strings.HasPrefix(l, ns) {
					t.Errorf("%s unit has %q, which needs a user namespace", name, l)
				}
			}
		}
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
	if !reflect.DeepEqual(p.Install[0], []string{"/bin/launchctl", "bootstrap", "gui/501", p.Path}) {
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
