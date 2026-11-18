//go:build windows

package core

import (
	"errors"
	"strings"
	"testing"

	"github.com/spytify/spytify/pkg/proto"
)

// With no launch command, the player is started by its name through the
// shell, never through cmd /C start (#829), and a name the shell cannot
// find fails the launch at once.
func TestThePlayerStartsByItsNameWithNoShell(t *testing.T) {
	c, _ := newTestCore(t, false)
	c.env.Target.Launch = ""
	var opened []string
	builtin := openApp
	refused := errors.New("the system cannot find the file specified")
	openApp = func(name string) error {
		opened = append(opened, name)
		if len(opened) > 1 {
			return refused
		}
		return nil
	}
	t.Cleanup(func() { openApp = builtin })

	if err := call(t, c, proto.MethodPlayerLaunch, nil, nil); err != nil {
		t.Fatalf("launch: %v", err)
	}
	if len(opened) != 1 || opened[0] != c.env.Target.ProcessNames[0] {
		t.Fatalf("opened %q, want the player's name %q", opened, c.env.Target.ProcessNames[0])
	}
	err := call(t, c, proto.MethodPlayerLaunch, nil, nil)
	if err == nil || !strings.Contains(err.Error(), refused.Error()) {
		t.Fatalf("launch of a name the shell cannot find = %v, want %q", err, refused)
	}
}
