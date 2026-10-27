// Package tag identifies a recorded track and writes its tags. Providers
// only run after a file's audio is cut; nothing here ever influences where
// a track starts or ends.
package tag

import (
	"strings"

	"github.com/spytify/spytify/internal/meta"
)

// Track is every field spytify writes, as v1's MapperID3 did. A provider
// leaves a field zero when it does not know it.
type Track struct {
	Title        string   `json:"title"`
	Subtitle     string   `json:"subtitle,omitempty"`
	Performers   []string `json:"performers,omitempty"`
	AlbumArtists []string `json:"album_artists,omitempty"`
	Album        string   `json:"album,omitempty"`
	Genres       []string `json:"genres,omitempty"`
	Disc         int      `json:"disc,omitempty"`
	Track        int      `json:"track,omitempty"`
	Year         int      `json:"year,omitempty"`
	CoverURL     string   `json:"cover_url,omitempty"`
	Cover        []byte   `json:"-"`
	CoverMIME    string   `json:"-"`
	// SubtitleSep is how the subtitle was attached to the title, to put it
	// back in a format with no subtitle tag.
	SubtitleSep Separator `json:"-"`
	// Source is where the tags came from: local, the player itself.
	Source string `json:"source"`
}

// Options are the tags.* settings that shape the mapping.
type Options struct {
	ExtraToSubtitle bool
	ArtistSeparator string
	CoverSize       string // small, medium, large
	Cover           bool
}

// FromLocal maps what the player's now-playing interface said. When the
// interface gives no artist list, the title is parsed as v1 parsed the
// window title ("Artist - Title - Extra").
func FromLocal(info meta.TrackInfo, o Options) Track {
	title := info.Title
	performers := append([]string(nil), info.Artists...)
	if len(performers) == 0 {
		if artist, rest, ok := SplitArtistTitle(title); ok {
			performers, title = []string{artist}, rest
		}
	}
	if len(performers) == 1 {
		for _, p := range Performers(title) {
			if !contains(performers, p) {
				performers = append(performers, p)
			}
		}
	}
	t := Track{
		Title:        title,
		Performers:   performers,
		AlbumArtists: append([]string(nil), info.AlbumArtists...),
		Album:        info.Album,
		Disc:         info.DiscNumber,
		Track:        info.TrackNumber,
		Year:         info.Year,
		CoverURL:     info.ArtURL,
		Source:       "local",
	}
	if len(t.AlbumArtists) == 0 && len(performers) > 0 {
		t.AlbumArtists = []string{performers[0]}
	}
	return t.withSubtitle(o)
}

func (t Track) withSubtitle(o Options) Track {
	if !o.ExtraToSubtitle || t.Subtitle != "" {
		return t
	}
	if base, extra, sep := SplitTitle(t.Title); sep != SepNone && extra != "" {
		t.Title, t.Subtitle, t.SubtitleSep = base, extra, sep
	}
	return t
}

func contains(list []string, s string) bool {
	for _, x := range list {
		if strings.EqualFold(x, s) {
			return true
		}
	}
	return false
}
