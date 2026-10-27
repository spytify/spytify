package tag

import (
	"slices"
	"strings"
	"testing"
)

// AAC files have no subtitle tag: the extra part stays in the title there,
// as the player showed it, instead of being lost.
func TestAACKeepsTheExtraInItsTitle(t *testing.T) {
	for _, c := range []struct {
		title, aac, base, subtitle string
	}{
		{"Song (Live)", "Song (Live)", "Song", "Live"},
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

// FLAC is normalised by tags: the gain in dB with two decimals, the peak
// linear with six, as ReplayGain readers expect them.
func TestFLACCarriesItsReplayGain(t *testing.T) {
	tr := Track{Title: "Song", ReplayGain: &ReplayGain{Gain: -3.2149, Peak: 0.89125094}}
	got := metadataPairs(tr, "flac", "")
	for _, want := range []string{"REPLAYGAIN_TRACK_GAIN=-3.21 dB", "REPLAYGAIN_TRACK_PEAK=0.891251"} {
		if !slices.Contains(got, want) {
			t.Errorf("%s missing from %v", want, got)
		}
	}
	if got := metadataPairs(Track{Title: "Song"}, "flac", ""); slices.ContainsFunc(got, func(kv string) bool { return strings.HasPrefix(kv, "REPLAYGAIN") }) {
		t.Errorf("a ReplayGain with none measured: %v", got)
	}
}
