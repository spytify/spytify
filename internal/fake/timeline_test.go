package fake

import (
	"path/filepath"
	"strings"
	"testing"
)

func load(t *testing.T, name string) *Timeline {
	t.Helper()
	tl, err := Load(filepath.Join("..", "..", "testdata", "timelines", name+".json"))
	if err != nil {
		t.Fatal(err)
	}
	return tl
}

// Tracks get a cover from testdata/covers, shuffled by the seed: the same
// on every run, and a different one for each of ten tracks. Ads get none.
func TestTracksGetACoverAndAdsDoNot(t *testing.T) {
	tl := load(t, "jitter")
	picked := map[string]bool{}
	for i, it := range tl.Items {
		if !strings.Contains(filepath.ToSlash(it.Art), "testdata/covers/cover-") {
			t.Fatalf("item %d has cover %q", i, it.Art)
		}
		picked[it.Art] = true
	}
	if len(picked) != 10 {
		t.Errorf("ten tracks share %d covers", len(picked))
	}
	again := load(t, "jitter")
	for i := range tl.Items {
		if again.Items[i].Art != tl.Items[i].Art {
			t.Fatalf("item %d got another cover on a second load", i)
		}
	}
	for i, it := range load(t, "ads").Items {
		if (it.Kind == "ad") != (it.Art == "") {
			t.Errorf("ads.json item %d (%s) has cover %q", i, it.Kind, it.Art)
		}
	}
	p, err := NewPlayer(tl, false)
	if err != nil {
		t.Fatal(err)
	}
	if url := p.Info(0).ArtURL; !strings.HasPrefix(url, "file:///") {
		t.Errorf("the player reports art %q", url)
	}
}
