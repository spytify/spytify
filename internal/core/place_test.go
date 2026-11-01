package core

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/spytify/spytify/pkg/proto"
)

// The output check says where the folder is and why it failed, never
// its path, and the log says it too.
func TestTheOutputCheckSaysWhereTheFolderIs(t *testing.T) {
	tl := loadTimeline(t, "basic")
	s := newScenario(t, tl, nil)
	facts := func() map[string]string {
		for _, c := range s.core.Info().Checks {
			if c.Name == proto.CheckOutputWritable {
				return c.Facts
			}
		}
		t.Fatal("no output check")
		return nil
	}
	if f := facts(); f["output.exists"] != "yes" || f["output.default"] != "no" || f["output.errno"] != "" {
		t.Errorf("facts of a folder written to: %v", f)
	}
	if err := os.RemoveAll(s.out); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(s.out, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f := facts()
	if f["output.exists"] != "no" || f["output.op"] != "mkdir" || f["output.errno"] == "" {
		t.Errorf("facts of a folder that cannot be made: %v", f)
	}
	for k, v := range f {
		if strings.Contains(v, s.out) {
			t.Errorf("%s holds the path: %q", k, v)
		}
	}
	if log := s.logs.String(); !strings.Contains(log, "output folder not writable") || !strings.Contains(log, "output.errno="+f["output.errno"]) {
		t.Errorf("log %q, want the failure and its facts", log)
	}
}

func TestTheDefaultFolderIsToldApart(t *testing.T) {
	if f := outputFacts(t.TempDir(), nil, true); f["output.default"] != "yes" {
		t.Errorf("default %q", f["output.default"])
	}
}

// The deepest folder names where a path is, in any case, and a folder
// whose name starts the same is not it.
func TestKnownFolder(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "ada")
	folders := []namedFolder{{"Music", filepath.Join(home, "Music")}, {"home", home}}
	for path, want := range map[string]string{
		filepath.Join(home, "Music", "Spytify"):            "Music",
		filepath.Join(home, "Music"):                       "Music",
		strings.ToUpper(filepath.Join(home, "music", "x")): "Music",
		filepath.Join(home, "Musicals"):                    "home",
		filepath.Join(string(filepath.Separator), "mnt"):   "elsewhere",
	} {
		if got := knownFolder(path, folders); got != want {
			t.Errorf("%s in %q, want %q", path, got, want)
		}
	}
	root := string(filepath.Separator)
	if !inFolder(filepath.Join(root, "a"), root) {
		t.Error("a folder at the root is not in it")
	}
}

// A business's OneDrive names the business: only the provider leaves.
func TestCloudName(t *testing.T) {
	for in, want := range map[string]string{
		"OneDrive": "OneDrive", "OneDrive - Contoso Ltd": "OneDrive", "Dropbox": "Dropbox", "Box Drive": "Box",
		"Google Drive": "Google Drive", "GoogleDrive-ada@example.com": "Google Drive", "iCloudDrive": "iCloud",
		"Nextcloud": "Nextcloud", "pCloudDrive": "pCloud", "MEGA": "MEGA", "Contoso Sync": "other",
	} {
		if got := cloudName(in); got != want {
			t.Errorf("%q as %q, want %q", in, got, want)
		}
	}
}

func TestControlledFolderAccessModes(t *testing.T) {
	for v, want := range map[uint64]string{0: "off", 1: "on", 2: "audit", 3: "disks", 4: "disks audit", 7: "7"} {
		if got := cfaMode(v); got != want {
			t.Errorf("%d as %q, want %q", v, got, want)
		}
	}
}

func TestFactsInTheLog(t *testing.T) {
	got := formatFacts(map[string]string{"output.drive": "fixed", "output.cloud": "Google Drive", "output.fs": ""})
	if want := `output.cloud="Google Drive" output.drive=fixed output.fs=""`; got != want {
		t.Errorf("%s, want %s", got, want)
	}
}

func TestFirstUnder(t *testing.T) {
	root := filepath.Join(string(filepath.Separator), "Users", "ada")
	for path, want := range map[string]string{
		filepath.Join(root, "Dropbox", "Music"): "Dropbox",
		filepath.Join(root, "Dropbox"):          "Dropbox",
		root:                                    "",
		filepath.Join(string(filepath.Separator), "mnt"): "",
	} {
		if got := firstUnder(path, root); got != want {
			t.Errorf("%s: %q, want %q", path, got, want)
		}
	}
}

// A cloud's own folder in the home folder, such as ~/Dropbox, holds what
// is in it, or behind a link to it; a folder whose name only contains a
// cloud's does not.
func TestACloudFolderInTheHomeFolder(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	in := filepath.Join(home, "Dropbox (Personal)", "Music", "Spytify")
	if err := os.MkdirAll(in, 0o755); err != nil {
		t.Fatal(err)
	}
	if got := homeCloud(in); got != "Dropbox" {
		t.Errorf("in ~/Dropbox: %q", got)
	}
	if got := homeCloud(filepath.Join(home, "Music", "My Dropbox songs")); got != "" {
		t.Errorf("in ~/Music: %q", got)
	}
	link := filepath.Join(home, "Spytify")
	if err := os.Symlink(in, link); err != nil {
		t.Skip("no symbolic links here:", err)
	}
	if got := homeCloud(link); got != "Dropbox" {
		t.Errorf("through a link: %q", got)
	}
}

func TestMusicFolder(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "home", "ada")
	for music, want := range map[string]string{
		filepath.Join(home, "Music"):             "home",
		filepath.Join(home, "Musique"):           "elsewhere",
		filepath.Join(home, "OneDrive", "Music"): "elsewhere",
	} {
		if got, _ := musicFolder([]namedFolder{{"Music", music}, {"home", home}}); got != want {
			t.Errorf("%s: %q, want %q", music, got, want)
		}
	}
	if got, _ := musicFolder([]namedFolder{{"home", home}}); got != "" {
		t.Errorf("no Music folder: %q", got)
	}
}

func oneOf(t *testing.T, facts map[string]string, key string, want ...string) {
	t.Helper()
	for _, w := range want {
		if facts[key] == w {
			return
		}
	}
	t.Errorf("%s %q, want one of %v", key, facts[key], want)
}
