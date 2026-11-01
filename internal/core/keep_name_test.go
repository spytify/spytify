package core

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/pkg/proto"
)

// A kept take's "(partial)" ends its file name when the template puts the
// title first, and still ends its title tag.
func TestAKeptTakeEndsItsNameWithPartial(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, map[string]any{"output.template": "{title} - {artist}"})
	// Paper Lanterns starts at 6.5 s: stop 2 s into it.
	s.player.At(8500, func() { _, _ = s.core.StopSession(true) })
	runWith(t, s, SessionOptions{EndOnStop: true})
	if _, skipped := outcome(t, s); skipped["Paper Lanterns"] != proto.SkipIncomplete {
		t.Fatalf("skipped %v", skipped)
	}
	id := idOf(s, "Paper Lanterns")
	if err := s.core.Keep(id); err != nil {
		t.Fatal(err)
	}
	f := finished(t, s, id)
	if got := filepath.Base(f.Path); got != "Paper Lanterns - Juno Vale (partial).wav" {
		t.Errorf("kept as %q", got)
	}
	b, err := os.ReadFile(f.Path + ".json")
	if err != nil {
		t.Fatal(err)
	}
	var side struct{ Title string }
	if err := json.Unmarshal(b, &side); err != nil || side.Title != "Paper Lanterns (partial)" {
		t.Errorf("sidecar %s: %v", b, err)
	}
}
