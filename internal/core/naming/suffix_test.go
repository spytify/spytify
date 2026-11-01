package naming

import (
	"strings"
	"testing"
)

// A suffix ends the name whatever the template, and stays whole when the
// name is cut to its length limit.
func TestSuffixEndsTheName(t *testing.T) {
	o := Options{GroupBy: "none", Prefix: "none", Template: "{title} - {artist}", Extension: "mp3"}
	tr := Track{Artists: []string{"A"}, Title: "T", Suffix: " (partial)"}
	if got := Build(tr, o).Base; got != "T - A (partial)" {
		t.Errorf("got %q", got)
	}
	o.Underscores = true
	if got := Build(tr, o).Base; got != "T_-_A_(partial)" {
		t.Errorf("with underscores, got %q", got)
	}
	o.Underscores = false
	tr.Title = strings.Repeat("x", 300)
	if got := Build(tr, o).Base; len(got) > maxComponent || !strings.HasSuffix(got, "x (partial)") {
		t.Errorf("long name: %d bytes, %q", len(got), got)
	}
	tr.Title, tr.Unknown, tr.FallbackStamp = "", true, "Untitled 20261031"
	if got := Build(tr, o).Base; got != "A (partial)" {
		t.Errorf("untitled, got %q", got)
	}
}
