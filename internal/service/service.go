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
	"path"
	"path/filepath"
	"strings"

	"github.com/spytify/spytify/internal/proc"
)

// Label names the service on every OS.
const Label = "io.github.spytify.spytifyd"

// ErrUnsupported is returned where no service manager is wired in.
var ErrUnsupported = errors.New("no per-user service on this OS yet; the desktop app starts spytifyd")

// Plan is what installing the service writes and runs.
type Plan struct {
	Path      string // empty when the plan enables the packaged unit
	Content   []byte
	Install   [][]string // run after writing Path
	Uninstall [][]string // run before removing Path
	Status    []string
	// Packaged is the unit the .deb or .rpm installed, enabled in place of
	// a written one. Stale is the unit an earlier install wrote in the
	// user's folder, which would shadow it: Install and Uninstall remove
	// it when it holds one of StaleContents, what such an install wrote.
	Packaged      string
	Enabled       []string // succeeds when the packaged unit is enabled
	Stale         string
	StaleContents [][]byte
}

// packagedUnit is where the .deb and .rpm put the user unit, which runs
// packagedDaemon.
var packagedUnit = "/usr/lib/systemd/user/spytifyd.service"

const packagedDaemon = "/usr/bin/spytifyd"

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
		user := path.Join(home, ".config", "systemd", "user", unit)
		if daemon == packagedDaemon && fileExists(packagedUnit) {
			return Plan{
				// reenable drops the link to a removed earlier unit.
				Install:   [][]string{{"systemctl", "--user", "daemon-reload"}, {"systemctl", "--user", "reenable", unit}, {"systemctl", "--user", "start", unit}},
				Uninstall: [][]string{{"systemctl", "--user", "disable", "--now", unit}},
				Status:    []string{"systemctl", "--user", "--no-pager", "status", unit},
				Packaged:  packagedUnit,
				Enabled:   []string{"systemctl", "--user", "--quiet", "is-enabled", unit},
				Stale:     user,
				StaleContents: [][]byte{
					systemdUnit(daemon),
					bytes.Replace(systemdUnit(daemon), []byte(hardening+"\n"), nil, 1), // before the hardening
				},
			}, nil
		}
		return Plan{
			Path:      user,
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
` + hardening + `

[Install]
WantedBy=default.target
`)
}

// hardening is the packaged unit's too. It holds no line that needs a
// user namespace, so the output folder can be anywhere, /tmp stays the
// desktop's, and PipeWire, D-Bus and crash reports keep working.
const hardening = `NoNewPrivileges=yes
LockPersonality=yes
RestrictSUIDSGID=yes
SystemCallArchitectures=native
RestrictAddressFamilies=AF_UNIX AF_NETLINK AF_INET AF_INET6`

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
	if p.Path == "" {
		if err := removeStale(p); err != nil {
			return err
		}
		return runAll(p.Install)
	}
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
	if p.Path == "" {
		err := runAll(p.Uninstall)
		if rerr := removeStale(p); rerr != nil && err == nil {
			err = rerr
		}
		return err
	}
	if _, err := os.Stat(p.Path); errors.Is(err, os.ErrNotExist) {
		return nil
	}
	err := runAll(p.Uninstall)
	if rerr := os.Remove(p.Path); rerr != nil && err == nil {
		err = rerr
	}
	return err
}

// Installed reports whether the plan's file is in place, or the packaged
// unit enabled.
func Installed(p Plan) bool {
	if p.Path == "" {
		return len(p.Enabled) > 0 && proc.Command(p.Enabled[0], p.Enabled[1:]...).Run() == nil
	}
	return fileExists(p.Path)
}

// removeStale removes the unit an earlier install wrote, only when it is
// unchanged: a unit the user edited stays.
func removeStale(p Plan) error {
	if p.Stale == "" {
		return nil
	}
	b, err := os.ReadFile(p.Stale)
	if err != nil {
		return nil
	}
	for _, c := range p.StaleContents {
		if bytes.Equal(b, c) {
			return os.Remove(p.Stale)
		}
	}
	return nil
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

func runAll(cmds [][]string) error {
	for _, c := range cmds {
		if out, err := proc.Command(c[0], c[1:]...).CombinedOutput(); err != nil {
			return fmt.Errorf("%s: %v: %s", strings.Join(c, " "), err, strings.TrimSpace(string(out)))
		}
	}
	return nil
}
