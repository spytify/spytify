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
// v1 did with the window title. ok is false when there is
// no artist part, which v1 treated as an advert or an unknown item.
func SplitArtistTitle(s string) (artist, title string, ok bool) {
	parts := splitNonEmpty(s, " - ", 2)
	if len(parts) < 2 {
		return "", "", false
	}
	return parts[0], parts[1], true
}

// SplitTitle separates an extended title: "Song - Live" or "Song (Live)".
// A dash wins over a parenthesis, as in v1. Every parenthesised group that
// ends the title is part of the extension: "Song (feat. A) (Live)" gives
// "Song" and "feat. A · Live". A group followed by more words, as in
// "Song (Part 1) Reprise", stays in the title.
func SplitTitle(title string) (base, extended string, sep Separator) {
	if strings.TrimSpace(title) == "" {
		return "", "", SepNone
	}
	if byDash := splitNonEmpty(title, " - ", 2); len(byDash) > 1 {
		return byDash[0], byDash[1], SepDash
	}
	if base, groups := trailingGroups(title); len(groups) > 0 {
		return base, strings.Join(groups, groupJoin), SepParenthesis
	}
	return title, "", SepNone
}

// groupJoin joins the parenthesised groups of one extended title.
const groupJoin = " · "

// trailingGroups peels the parenthesised groups, each after a space, off the
// end of a title: "Song (feat. A) (Live)" gives "Song" and [feat. A, Live].
// A group may hold parentheses of its own.
func trailingGroups(title string) (base string, groups []string) {
	base = strings.TrimRight(title, " ")
	for strings.HasSuffix(base, ")") {
		open := openingParen(base)
		if open < 1 || base[open-1] != ' ' {
			break
		}
		inner := strings.TrimSpace(base[open+1 : len(base)-1])
		rest := strings.TrimRight(base[:open], " ")
		if inner == "" || rest == "" {
			break
		}
		groups = append([]string{inner}, groups...)
		base = rest
	}
	return base, groups
}

// openingParen returns the index of the "(" that s's final ")" closes, or -1.
func openingParen(s string) int {
	depth := 0
	for i := len(s) - 1; i >= 0; i-- {
		switch s[i] {
		case ')':
			depth++
		case '(':
			if depth--; depth == 0 {
				return i
			}
		}
	}
	return -1
}

// JoinTitle is v1's ToTitleString: the title with its extension. Each group
// of a parenthesised extension gets its own parentheses back.
func JoinTitle(title, extended string, sep Separator) string {
	if title == "" || extended == "" {
		return title
	}
	switch sep {
	case SepDash:
		return title + " - " + extended
	case SepParenthesis:
		return title + " (" + strings.ReplaceAll(extended, groupJoin, ") (") + ")"
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
