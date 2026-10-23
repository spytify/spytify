package main

import (
	"strings"
	"testing"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/pkg/proto"
)

func TestPagesCoverTheCode(t *testing.T) {
	s := settingsPage()
	for _, d := range settings.Table {
		if !strings.Contains(s, "`"+d.Key+"`") {
			t.Errorf("settings page lacks %s", d.Key)
		}
	}
	p := protocolPage()
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
