package naming

import (
	"path/filepath"
	"testing"
)

// The folder grouping. With none every file is at the root. The main
// artist is the album's artist, or the track's first when the player gives
// none, so a collaboration goes in its main artist's folder and a
// compilation stays together. All artists make a folder for each group of
// artists.
func TestArtistFolders(t *testing.T) {
	tracks := []Track{
		{Artists: []string{"Neon Harbor"}, AlbumArtists: []string{"Neon Harbor"}, Title: "Solo", Album: "Lights", Year: 2024},
		{Artists: []string{"Neon Harbor", "Lumen"}, AlbumArtists: []string{"Neon Harbor"}, Title: "Together", Album: "Lights", Year: 2024},
		{Artists: []string{"Lumen", "Neon Harbor", "Lumen"}, Title: "Flip", Album: "Days"},
		{Artists: []string{"Lumen"}, AlbumArtists: []string{"Various Artists"}, Title: "Hit", Album: "Summer"},
		{Artists: []string{"Band", "Other"}, AlbumArtists: []string{"Band", "Other"}, Title: "Split", Album: "EP"},
	}
	for group, want := range map[string][]string{
		"none":          {"", "", "", "", ""},
		"artist":        {"Neon Harbor", "Neon Harbor", "Lumen", "Various Artists", "Band"},
		"artist/album":  {"Neon Harbor/Lights (2024)", "Neon Harbor/Lights (2024)", "Lumen/Days", "Various Artists/Summer", "Band/EP"},
		"artists":       {"Neon Harbor", "Neon Harbor, Lumen", "Lumen, Neon Harbor", "Lumen", "Band, Other"},
		"artists/album": {"Neon Harbor/Lights (2024)", "Neon Harbor, Lumen/Lights (2024)", "Lumen, Neon Harbor/Days", "Lumen/Summer", "Band, Other/EP"},
	} {
		o := Options{GroupBy: group, Prefix: "none", Template: "{artist} - {title}", Extension: "mp3"}
		for i, tr := range tracks {
			if got := filepath.ToSlash(Build(tr, o).Dir); got != want[i] {
				t.Errorf("%s, %q: folder %q, want %q", group, tr.Title, got, want[i])
			}
		}
	}
	o := Options{GroupBy: "artists", ArtistSeparator: " & ", Template: "{title}", Extension: "mp3"}
	if got := Build(tracks[1], o).Dir; got != "Neon Harbor & Lumen" {
		t.Errorf("with the artist separator: %q", got)
	}
}
