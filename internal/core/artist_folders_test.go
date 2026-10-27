package core

import (
	"path/filepath"
	"testing"

	"github.com/spytify/spytify/internal/fake"
	"github.com/spytify/spytify/pkg/proto"
)

// Saved files land in the folders the grouping says: at the root, in the
// main artist's folder (a collaboration with its main artist), or in a
// folder for each group of artists.
func TestFlatAndArtistFolders(t *testing.T) {
	for group, want := range map[string]map[string]string{
		"none":          {"Solo": ".", "Together": ".", "Flip": "."},
		"artist":        {"Solo": "Neon Harbor", "Together": "Neon Harbor", "Flip": "Lumen"},
		"artist/album":  {"Solo": "Neon Harbor/Lights", "Together": "Neon Harbor/Lights", "Flip": "Lumen/Days"},
		"artists":       {"Solo": "Neon Harbor", "Together": "Neon Harbor, Lumen", "Flip": "Lumen, Neon Harbor"},
		"artists/album": {"Solo": "Neon Harbor/Lights", "Together": "Neon Harbor, Lumen/Lights", "Flip": "Lumen, Neon Harbor/Days"},
	} {
		t.Run(group, func(t *testing.T) {
			tl := loadTimeline(t, "artists")
			s := newScenario(t, tl, map[string]any{"output.group_by": group})
			s.run(t, tl)
			if problems := fake.Verify(tl, tl.Expect, -1, s.out, s.events.all()); len(problems) > 0 {
				t.Fatalf("%v\nlog:\n%s", problems, s.logs)
			}
			titles := map[string]string{}
			for _, ev := range s.events.all() {
				switch ev.Method {
				case proto.EventTrackStarted:
					var e proto.TrackStartedEvent
					if ev.Decode(&e) == nil {
						titles[e.Track.ID] = e.Track.Title
					}
				case proto.EventTrackFinished:
					var e proto.TrackFinishedEvent
					if ev.Decode(&e) != nil {
						continue
					}
					rel, _ := filepath.Rel(s.out, filepath.Dir(e.Path))
					if title := titles[e.ID]; filepath.ToSlash(rel) != want[title] {
						t.Errorf("%q saved in %q, want %q", title, filepath.ToSlash(rel), want[title])
					}
				}
			}
		})
	}
}
