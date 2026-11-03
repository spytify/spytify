package ui

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"fyne.io/fyne/v2"
)

// Opening the Library reads its folder again, the one it showed: when the
// output folder changed meanwhile, as Reset does in Settings, it reads the
// new one too. The two reads run side by side, and the former folder's,
// full of recordings, comes back last: its files are not listed under the
// new folder's name.
func TestTheLibraryListsTheNewOutputFolder(t *testing.T) {
	former, chosen := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(former, "M83 - Intro.flac"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	values := defaults()
	values["output.dir"] = former
	a, m := connectedTestApp(t, values)
	a.showTab(tabLibrary)
	lib := a.screens[tabLibrary].(*libraryScreen)
	if len(lib.all) != 1 {
		t.Fatalf("%d files in the former folder", len(lib.all))
	}
	a.showTab(tabSettings)
	values["output.dir"] = chosen
	m.SetSettings(values)
	a.render()

	// The two reads run in the background, as they do out of tests, and
	// hand the window their files through fyne.Do, which a real driver runs
	// on its own goroutine: here the test runs them, in the order they came
	// and then the other way round.
	calls := make(chan func(), 2)
	fyne.SetCurrentApp(queuedApp{a.fa, queuedDriver{a.fa.Driver(), calls}})
	defer fyne.SetCurrentApp(a.fa)
	a.opts.ManualRender = false
	a.tabs.SelectIndex(tabLibrary)
	a.opts.ManualRender = true
	var reads []func()
	for len(reads) < 2 {
		select {
		case read := <-calls:
			reads = append(reads, read)
		case <-time.After(10 * time.Second):
			t.Fatalf("%d reads of the folders came back", len(reads))
		}
	}
	fyne.SetCurrentApp(a.fa)
	for _, order := range [][]func(){{reads[0], reads[1]}, {reads[1], reads[0]}} {
		for _, read := range order {
			read()
		}
		if len(lib.all) != 0 || !lib.empty.Visible() {
			t.Fatalf("the new, empty output folder lists %d files", len(lib.all))
		}
	}
}
