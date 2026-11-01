package main

import (
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/target"
	"github.com/spytify/spytify/pkg/proto"
)

func TestPagesCoverTheCode(t *testing.T) {
	s := settingsPage()
	for _, d := range settings.Table {
		if !strings.Contains(s, "`"+d.Key+"`") {
			t.Errorf("settings page lacks %s", d.Key)
		}
	}
	p := protocolSection()
	for _, m := range proto.Methods {
		if !strings.Contains(p, "`"+m.Name+"`") {
			t.Errorf("protocol page lacks %s", m.Name)
		}
	}
	for _, e := range proto.Events {
		if !strings.Contains(p, "`"+e.Name+"`") {
			t.Errorf("protocol page lacks %s", e.Name)
		}
	}
	// Every type a table links to has its own section.
	for _, name := range []string{"CoreInfoResult", "Track", "SessionStateEvent"} {
		if !strings.Contains(p, "#### "+name+"\n") {
			t.Errorf("protocol page has no %s section", name)
		}
	}
}

// An advanced setting or a player variable the page does not explain fails
// here, and so does an explanation left over from a setting gone simple.
func TestAdvancedSettingsAreExplained(t *testing.T) {
	advanced := map[string]bool{}
	for _, d := range settings.Table {
		advanced[d.Key] = d.Advanced
		if d.Advanced && explain(d.Key) == "" {
			t.Errorf("%s is advanced and has no explanation", d.Key)
		}
	}
	for k := range explanations {
		if !advanced[k.String()] {
			t.Errorf("%s is explained but not advanced", k)
		}
	}
	s := settingsPage()
	for _, v := range target.TargetKeys {
		if !strings.Contains(s, "| `"+v+"` |") {
			t.Errorf("settings page lacks the player's %s", v)
		}
	}
}
