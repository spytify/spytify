package tag

import (
	"regexp"
	"strings"
)

// Separator is how an extended title is attached to the title.
type Separator int

const (
	SepNone Separator = iota
	SepDash
	SepParenthesis
)

// SplitArtistTitle splits "Artist - Title - Extra" on the first " - ", as
// v1's SpotifyStatus did with the window title. ok is false when there is
// no artist part, which v1 treated as an advert or an unknown item.
func SplitArtistTitle(s string) (artist, title string, ok bool) {
	parts := splitNonEmpty(s, " - ", 2)
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// SplitTitle separates an extended title: "Song - Live" or "Song (Live)".
// A dash wins over a parenthesis, as in v1.
func SplitTitle(title string) (base, extended string, sep Separator) {
	if strings.TrimSpace(title) == "" {
		return "", "", SepNone
	}
	if byDash := splitNonEmpty(title, " - ", 2); len(byDash) > 1 {
		return byDash[0], byDash[1], SepDash
	}
	if byParen := splitNonEmpty(title, " (", 2); len(byParen) > 1 {
		return byParen[0], strings.ReplaceAll(byParen[1], ")", ""), SepParenthesis
	}
	return title, "", SepNone
}

// JoinTitle is v1's ToTitleString: the title with its extension.
func JoinTitle(title, extended string, sep Separator) string {
	if title == "" || extended == "" {
		return title
	}
	switch sep {
	case SepDash:
		return title + " - " + extended
	case SepParenthesis:
		return title + " (" + extended + ")"
	}
	return title
}

var performersRe = regexp.MustCompile(`\((?:with |feat\. )(.*)\)`)

// Performers extracts guests from "(feat. A & B)" or "(with A, B)".
func Performers(s string) []string {
	m := performersRe.FindStringSubmatch(s)
	if m == nil {
		return nil
	}
	var out []string
	for _, p := range strings.Split(strings.ReplaceAll(m[1], " & ", ", "), ", ") {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// SimplifyTitle drops an extended part, for a second lookup as v1's
// Last.fm client did: "Song (Remastered)" and "Song - Live" become "Song".
func SimplifyTitle(title string) string {
	return strings.TrimSpace(simplifyRe.ReplaceAllString(title, ""))
}

var simplifyRe = regexp.MustCompile(` \(.*?\)| - .*`)

func splitNonEmpty(s, sep string, n int) []string {
	var out []string
	for _, p := range strings.SplitN(s, sep, n) {
		if p != "" {
			out = append(out, p)
		}
	}
	return out
}
