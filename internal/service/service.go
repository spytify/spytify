// Package service runs spytifyd as a per-user service that starts at login
// and starts again after a crash: a systemd user unit on Linux, a launchd
// agent on macOS. A clean exit (core.shutdown, the app's restart) is left
// alone, so the app can still stop and start the daemon itself.
package service

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
)

// Label names the service on every OS.
const Label = "io.github.spytify.spytifyd"

// ErrUnsupported is returned where no service manager is wired in.
var ErrUnsupported = errors.New("no per-user service on this OS yet; the desktop app starts spytifyd")

// Plan is what installing the service writes and runs.
type Plan struct {
	Path      string
	Content   []byte
	Install   [][]string // run after writing Path
	Uninstall [][]string // run before removing Path
	Status    []string
}

// Supported reports whether goos has a service manager wired in.
func Supported(goos string) bool { return goos == "linux" || goos == "darwin" }

// For returns the plan for daemon, the absolute path of spytifyd, on goos.
// Both service managers are POSIX: the plan uses slash paths.
func For(goos, daemon, home string, uid int) (Plan, error) {
	if !Supported(goos) {
		return Plan{}, ErrUnsupported
	}
	if !path.IsAbs(daemon) {
		return Plan{}, fmt.Errorf("spytifyd path %q is not absolute", daemon)
	}
	if goos == "linux" {
		unit := "spytifyd.service"
		return Plan{
			Path:      path.Join(home, ".config", "systemd", "user", unit),
			Content:   systemdUnit(daemon),
			Install:   [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "enable", "--now", unit}},
			Uninstall: [][]string{{"systemctl", "--user", "disable", "--now", unit}},
			Status:    []string{"systemctl", "--user", "--no-pager", "status", unit},
		}, nil
	}
	agent := path.Join(home, "Library", "LaunchAgents", Label+".plist")
	domain := fmt.Sprintf("gui/%d", uid)
	return Plan{
		Path:      agent,
		Content:   launchdAgent(daemon),
		Install:   [][]string{{"launchctl", "bootstrap", domain, agent}},
		Uninstall: [][]string{{"launchctl", "bootout", domain, agent}},
		Status:    []string{"launchctl", "print", domain + "/" + Label},
	}, nil
}

// systemdUnit quotes the path, which may hold spaces, and doubles the %
// that systemd would expand.
func systemdUnit(daemon string) []byte {
	quoted := `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`, "%", "%%").Replace(daemon) + `"`
	return []byte(`[Unit]
Description=Spytify recorder (spytifyd)
After=pipewire.service pipewire-pulse.service

[Service]
ExecStart=` + quoted + `
Restart=on-failure
RestartSec=5

[Install]
WantedBy=default.target
`)
}

func launchdAgent(daemon string) []byte {
	var path bytes.Buffer
	_ = xml.EscapeText(&path, []byte(daemon))
	return []byte(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key>
	<string>` + Label + `</string>
	<key>ProgramArguments</key>
	<array>
		<string>` + path.String() + `</string>
	</array>
	<key>RunAtLoad</key>
	<true/>
	<key>KeepAlive</key>
	<dict>
		<key>SuccessfulExit</key>
		<false/>
	</dict>
	<key>ProcessType</key>
	<string>Interactive</string>
</dict>
</plist>
`)
}

// Install writes the plan's file and runs its commands.
func Install(p Plan) error {
	if err := os.MkdirAll(filepath.Dir(p.Path), 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(p.Path, p.Content, 0o644); err != nil {
		return err
	}
	return runAll(p.Install)
}

// Uninstall stops the service and removes its file.
func Uninstall(p Plan) error {
	if _, err := os.Stat(p.Path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	err := runAll(p.Uninstall)
	if rerr := os.Remove(p.Path); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

// Installed reports whether the plan's file is in place.
func Installed(p Plan) bool {
	_, err := os.Stat(p.Path)
	return err == nil
}

func runAll(cmds [][]string) error {
	for _, c := range cmds {
		if out, err := exec.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v: %s", strings.Join(c, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
