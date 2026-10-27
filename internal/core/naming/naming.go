// Package naming turns a track's metadata and the output settings into the
// path of its file, following v1's FileManager rules: folders per artist and
// album, an "Untitled" album, invalid characters removed, a three-way prefix,
// and a numeric suffix for duplicates.
package naming

import (
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/text/runes"
	"golang.org/x/text/transform"
	"golang.org/x/text/unicode/norm"
)

const (
	untitledAlbum = "Untitled"
	invalidName   = "INVALID"
	// maxComponent leaves room for a duplicate suffix, the extension and
	// ".part" under the usual 255-byte file name limit.
	maxComponent = 200
)

// Track is the metadata naming needs.
type Track struct {
	Artists       []string
	AlbumArtists  []string
	Title         string
	Album         string
	TrackNumber   int
	Year          int
	Unknown       bool // no title: podcasts and untitled items go to the root
	FallbackStamp string
}

// Options are the output.* settings.
type Options struct {
	GroupBy         string // none, artist, artist/album, artists, artists/album
	Prefix          string // none, counter, album_track
	Underscores     bool
	Template        string
	CounterWidth    int
	ASCII           bool
	ArtistSeparator string
	Extension       string
}

// Plan is where a track goes, relative to the output folder.
type Plan struct {
	Dir  string // relative folder, "" for the root
	Base string // file name without prefix, suffix or extension
}

// Build plans the path of t.
func Build(t Track, o Options) Plan {
	sep := " "
	if o.Underscores {
		sep = "_"
	}
	if o.ArtistSeparator == "" {
		o.ArtistSeparator = ", "
	}
	var dirs []string
	if o.GroupBy != "none" && o.GroupBy != "" && !t.Unknown {
		artist := mainArtist(t)
		if strings.HasPrefix(o.GroupBy, "artists") {
			artist = allArtists(t, o.ArtistSeparator)
		}
		dirs = append(dirs, component(artist, o.ASCII, sep))
		if strings.HasSuffix(o.GroupBy, "/album") {
			album := t.Album
			if strings.TrimSpace(album) == "" {
				album = untitledAlbum
			}
			if t.Year > 0 {
				album = fmt.Sprintf("%s (%d)", album, t.Year)
			}
			dirs = append(dirs, component(album, o.ASCII, sep))
		}
	}

	var name string
	if t.Unknown {
		name = firstNonEmpty(strings.Join(t.Artists, o.ArtistSeparator), t.FallbackStamp)
	} else {
		name = expand(o.Template, t, o.ArtistSeparator)
	}
	return Plan{Dir: filepath.Join(dirs...), Base: component(name, o.ASCII, sep)}
}

// FileName is the final name for a plan: prefix, base, extension.
func FileName(p Plan, o Options, counter int64, trackNumber int) string {
	sep := " "
	if o.Underscores {
		sep = "_"
	}
	prefix := ""
	switch o.Prefix {
	case "counter":
		width := o.CounterWidth
		if width <= 0 {
			width = 3
		}
		counter = min(max(counter, 0), CounterLimit(width))
		prefix = fmt.Sprintf("%0*d%s", width, counter, sep)
	case "album_track":
		if trackNumber > 0 {
			prefix = fmt.Sprintf("%02d%s", trackNumber, sep)
		}
	}
	return prefix + p.Base + "." + o.Extension
}

// CounterLimit is the last number a counter of width digits writes: 999
// for 3.
func CounterLimit(width int) int64 {
	if width <= 0 {
		width = 3
	}
	limit := int64(1)
	for range width {
		limit *= 10
	}
	return limit - 1
}

// Example is the path, from the output folder, a sample track gets with o
// and the counter at counter: the settings show it under the template.
func Example(o Options, counter int64) string {
	// Its accents show what removing diacritics does, its guest what all
	// artists folders do.
	t := Track{Artists: []string{"Neon Harbor", "Lumen"}, AlbumArtists: []string{"Neon Harbor"}, Title: "Café Lumière",
		Album: "Harbor Lights", TrackNumber: 4, Year: 2024}
	p := Build(t, o)
	return filepath.Join(p.Dir, FileName(p, o, counter, t.TrackNumber))
}

// WithSuffix returns name with v1's duplicate counter: "Name 2.mp3".
func WithSuffix(name string, n int, underscores bool) string {
	if n <= 1 {
		return name
	}
	sep := " "
	if underscores {
		sep = "_"
	}
	ext := filepath.Ext(name)
	return strings.TrimSuffix(name, ext) + sep + strconv.Itoa(n) + ext
}

// FreeSuffix returns the first name in dir, starting with name itself, for
// which exists reports false.
func FreeSuffix(dir, name string, underscores bool, exists func(string) bool) string {
	for n := 1; ; n++ {
		candidate := WithSuffix(name, n, underscores)
		if !exists(filepath.Join(dir, candidate)) {
			return candidate
		}
	}
}

// Recorded reports whether a file for plan already exists under root,
// ignoring a counter or album-track prefix and a duplicate suffix of 1.
func Recorded(root string, p Plan, o Options) bool {
	dir := filepath.Join(root, p.Dir)
	want := p.Base + "." + o.Extension
	if _, err := os.Stat(filepath.Join(dir, want)); err == nil {
		return true
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return false
	}
	prefixed := regexp.MustCompile(`^\d+[ _]` + regexp.QuoteMeta(want) + `$`)
	for _, e := range entries {
		// macOS and some network drives return accents decomposed (NFD);
		// want is composed.
		if name := norm.NFC.String(e.Name()); !e.IsDir() && (name == want || prefixed.MatchString(name)) {
			return true
		}
	}
	return false
}

var placeholder = regexp.MustCompile(`\{([a-z]+)\}`)

func expand(tmpl string, t Track, artistSep string) string {
	if tmpl == "" {
		tmpl = "{artist} - {title}"
	}
	out := placeholder.ReplaceAllStringFunc(tmpl, func(m string) string {
		switch m {
		case "{artist}":
			return artistsOf(t, artistSep)
		case "{title}":
			return t.Title
		case "{album}":
			if t.Album == "" {
				return untitledAlbum
			}
			return t.Album
		case "{track}":
			if t.TrackNumber > 0 {
				return fmt.Sprintf("%02d", t.TrackNumber)
			}
			return ""
		case "{year}":
			if t.Year > 0 {
				return strconv.Itoa(t.Year)
			}
			return ""
		}
		return m
	})
	// Empty placeholders leave dangling separators: "Artist - " or "()".
	out = strings.ReplaceAll(out, "()", "")
	out = strings.TrimSpace(out)
	out = strings.Trim(out, "-_ ")
	return out
}

// artistsOf follows v1's Track.Artists: album artists when known, otherwise
// the artist plus extra performers, without repeats.
func artistsOf(t Track, sep string) string {
	if len(t.AlbumArtists) > 0 {
		return strings.Join(t.AlbumArtists, sep)
	}
	return strings.Join(unique(t.Artists), sep)
}

// mainArtist names a main artist folder: the album's first artist, else
// the track's first, so a collaboration goes in its main artist's folder
// and a compilation stays together.
func mainArtist(t Track) string {
	for _, list := range [][]string{t.AlbumArtists, t.Artists} {
		if names := unique(list); len(names) > 0 {
			return names[0]
		}
	}
	return ""
}

// allArtists names an all-artists folder: every artist of the track, a
// folder for each group of artists; the album's when the track names none.
func allArtists(t Track, sep string) string {
	if names := unique(t.Artists); len(names) > 0 {
		return strings.Join(names, sep)
	}
	return strings.Join(unique(t.AlbumArtists), sep)
}

// unique is names trimmed, without the empty ones and the repeats.
func unique(names []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, a := range names {
		if a = strings.TrimSpace(a); a != "" && !seen[a] {
			seen[a] = true
			out = append(out, a)
		}
	}
	return out
}

var invalidChars = regexp.MustCompile(`[/\\:*?"<>|\x00-\x1f\x7f]`)
var spaces = regexp.MustCompile(`\s+`)

// component cleans one path component the way v1's GetCleanFileFolder does,
// plus an optional diacritics removal and a byte cap.
func component(s string, ascii bool, sep string) string {
	s = norm.NFC.String(strings.TrimSpace(s))
	if ascii {
		s = stripDiacritics(s)
	}
	s = invalidChars.ReplaceAllString(s, "")
	s = strings.TrimRight(strings.TrimSpace(s), ".")
	s = strings.TrimSpace(s)
	if s == "" {
		return invalidName
	}
	s = truncateBytes(s, maxComponent)
	if reserved(s) {
		s += "_"
	}
	return spaces.ReplaceAllString(s, sep)
}

// reserved says whether Windows refuses s as a file or folder name, with or
// without an extension: CON, PRN, AUX, NUL, COM1 to COM9, LPT1 to LPT9. The
// rule applies on every OS, so a library moves between them.
func reserved(s string) bool {
	name := strings.ToUpper(strings.TrimSpace(strings.SplitN(s, ".", 2)[0]))
	switch name {
	case "CON", "PRN", "AUX", "NUL":
		return true
	}
	return len(name) == 4 && (strings.HasPrefix(name, "COM") || strings.HasPrefix(name, "LPT")) && name[3] >= '1' && name[3] <= '9'
}

func stripDiacritics(s string) string {
	t := transform.Chain(norm.NFD, runes.Remove(runes.In(unicode.Mn)), norm.NFC)
	out, _, err := transform.String(t, s)
	if err != nil {
		return s
	}
	return out
}

func truncateBytes(s string, max int) string {
	if len(s) <= max {
		return s
	}
	s = s[:max]
	for !utf8.ValidString(s) {
		s = s[:len(s)-1]
	}
	return strings.TrimSpace(s)
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if strings.TrimSpace(v) != "" {
			return v
		}
	}
	return ""
}
