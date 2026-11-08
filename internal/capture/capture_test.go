package capture

import (
	"fmt"
	"strings"
	"testing"
)

// The ring keeps a program's last lines, written in pieces or whole, and
// its newest bytes when they are too many.
func TestTheStderrRingKeepsTheLastLines(t *testing.T) {
	var r lineRing
	for i := 1; i <= 6; i++ {
		fmt.Fprintf(&r, "line %d\r\n", i)
	}
	r.Write([]byte("half "))
	if got, want := r.String(), "line 2\nline 3\nline 4\nline 5\nline 6"; got != want {
		t.Fatalf("ring %q, want %q", got, want)
	}
	r.Write([]byte("a line\n"))
	r.add("added")
	if got, want := r.String(), "line 4\nline 5\nline 6\nhalf a line\nadded"; got != want {
		t.Fatalf("ring %q, want %q", got, want)
	}
	r.add(strings.Repeat("x", 2*stderrBytes) + "end")
	if got := r.String(); len(got) != stderrBytes || !strings.HasSuffix(got, "end") {
		t.Fatalf("ring of %d bytes ending %q", len(got), got[len(got)-3:])
	}
}
