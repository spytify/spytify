package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"

	"github.com/spf13/cobra"

	"github.com/spytify/spytify/internal/service"
)

func (a *app) serviceCmd() *cobra.Command {
	var daemon string
	plan := func() (service.Plan, error) {
		if !service.Supported(runtime.GOOS) {
			return service.Plan{}, service.ErrUnsupported
		}
		path, err := findDaemon(daemon)
		if err != nil {
			return service.Plan{}, err
		}
		home, err := os.UserHomeDir()
		if err != nil {
			return service.Plan{}, err
		}
		return service.For(runtime.GOOS, path, home, os.Getuid())
	}
	cmd := &cobra.Command{
		Use:   "service install|uninstall|status",
		Short: "Run spytifyd at login and start it again after a crash (systemd user unit, launchd agent)",
	}
	cmd.PersistentFlags().StringVar(&daemon, "daemon", "", "spytifyd to run (default: the one next to spytify-cli, then PATH)")
	cmd.AddCommand(&cobra.Command{
		Use:   "install",
		Short: "Install and start the service",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			p, err := plan()
			if err != nil {
				return err
			}
			if err := service.Install(p); err != nil {
				return err
			}
			fmt.Fprintf(a.out, "installed %s; spytifyd now starts at login\n", p.Path)
			return nil
		},
	}, &cobra.Command{
		Use:   "uninstall",
		Short: "Stop and remove the service",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			p, err := plan()
			if err != nil {
				return err
			}
			if err := service.Uninstall(p); err != nil {
				return err
			}
			fmt.Fprintln(a.out, "spytifyd no longer starts at login")
			return nil
		},
	}, &cobra.Command{
		Use:   "status",
		Short: "Show whether the service is installed and running",
		Args:  cobra.NoArgs,
		RunE: func(*cobra.Command, []string) error {
			p, err := plan()
			if err != nil {
				return err
			}
			if !service.Installed(p) {
				fmt.Fprintln(a.out, "not installed")
				return nil
			}
			c := exec.Command(p.Status[0], p.Status[1:]...)
			c.Stdout, c.Stderr = a.out, a.out
			_ = c.Run()
			return nil
		},
	})
	return cmd
}

// findDaemon resolves spytifyd: the given path, next to this binary, then
// PATH, as an absolute path for the service file.
func findDaemon(path string) (string, error) {
	if path == "" {
		if exe, err := os.Executable(); err == nil {
			if p := filepath.Join(filepath.Dir(exe), "spytifyd"); fileExists(p) {
				path = p
			}
		}
	}
	if path == "" {
		p, err := exec.LookPath("spytifyd")
		if err != nil {
			return "", fmt.Errorf("spytifyd not found next to spytify-cli or on PATH; pass --daemon")
		}
		path = p
	}
	return filepath.Abs(path)
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}
