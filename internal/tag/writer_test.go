package tag

import (
	"slices"
	"testing"
)

// AAC files have no subtitle tag: the extra part stays in the title there,
// as the player showed it, instead of being lost.
func TestAACKeepsTheExtraInItsTitle(t *testing.T) {
	for _, c := range []struct {
		title, aac, base, subtitle string
	}{
		{"Song (Live)", "Song (Live)", "Song", "Live"},
		{"Song (feat. Lumen) (Live)", "Song (feat. Lumen) (Live)", "Song", "feat. Lumen · Live"},
		{"Song - Remastered 2011", "Song - Remastered 2011", "Song", "Remastered 2011"},
		{"Song", "Song", "Song", ""},
	} {
		tr := Track{Title: c.title, Performers: []string{"Artist"}}.withSubtitle(Options{ExtraToSubtitle: true})
		if got := metadataPairs(tr, "aac", ""); !slices.Contains(got, "title="+c.aac) || slices.ContainsFunc(got, func(kv string) bool { return kv == "title="+c.base && c.base != c.aac }) {
			t.Errorf("%q: aac tags %v", c.title, got)
		}
		mp3 := metadataPairs(tr, "mp3", "")
		if !slices.Contains(mp3, "title="+c.base) {
			t.Errorf("%q: mp3 tags %v", c.title, mp3)
		}
		if c.subtitle != "" && !slices.Contains(mp3, "TIT3="+c.subtitle) {
			t.Errorf("%q: mp3 subtitle missing from %v", c.title, mp3)
		}
	}
	// A subtitle a provider gave, with no separator known, joins with a dash
	// as in file names.
	tr := Track{Title: "Song", Subtitle: "Live"}
	if got := metadataPairs(tr, "aac", ""); !slices.Contains(got, "title=Song - Live") {
		t.Errorf("provider subtitle: aac tags %v", got)
	}
}
