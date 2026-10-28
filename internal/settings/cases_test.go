package settings

import (
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"path/filepath"
	"strings"
	"testing"
)

// cases names, for each setting, the tests that show it doing what it says
// with the setting on and off, as "package folder:TestName".
var cases = map[string][]string{
	"app.theme":                        {"internal/ui:TestThemeFollowsTheSetting"},
	"app.minimize_to_tray":             {"internal/ui:TestClosingTheWindowQuitsUnlessMinimizedToTray", "internal/ui:TestClosingToTheTrayKeepsSpytifyThere"},
	"app.autostart_recording":          {"test/e2e:TestAutostartRecordsOnLaunch"},
	"app.check_updates":                {"internal/ui:TestTheUpdateCheckFollowsTheSetting"},
	"output.dir":                       {"internal/core:TestOutputFolderGoneStopsTheRecording", "internal/core:TestNoRecordingStartsWithoutAnOutputFolder"},
	"output.group_by":                  {"internal/core:TestOutputLayoutForEverySetting", "internal/core:TestFlatAndArtistFolders"},
	"output.template":                  {"internal/core:TestOutputPathsOfTheSettingsTimeline"},
	"output.underscores":               {"internal/core:TestOutputLayoutForEverySetting"},
	"output.ascii_filenames":           {"internal/core:TestOutputLayoutForEverySetting"},
	"output.prefix":                    {"internal/core:TestOutputLayoutForEverySetting"},
	"output.counter_next":              {"internal/core:TestEachFileSaysItsCounter"},
	"output.counter_width":             {"internal/core:TestTheRecordingStopsWhenTheCounterIsFull"},
	"encode.format":                    {"internal/core:TestEncodingSettingsReachTheFiles"},
	"encode.bitrate_kbps":              {"internal/core:TestEncodingSettingsReachTheFiles", "internal/ui:TestPickingABitrateSendsItsNumber"},
	"tags.cover":                       {"internal/core:TestEncodingSettingsReachTheFiles"},
	"tags.cover_size":                  {"internal/core:TestEncodingSettingsReachTheFiles"},
	"tags.extra_to_subtitle":           {"internal/core:TestEncodingSettingsReachTheFiles", "internal/core:TestOutputPathsOfTheSettingsTimeline", "internal/core:TestTheSubtitleOptionKeepsTheFileName"},
	"tags.artist_separator":            {"internal/core:TestEncodingSettingsReachTheFiles"},
	"recorder.duplicates":              {"internal/core:TestScenarios", "internal/core:TestOutputLayoutForEverySetting"},
	"recorder.skip_duplicates_forward": {"internal/core:TestSkipDuplicateForward"},
	"recorder.single_track":            {"test/e2e:TestAutostartRecordsOnLaunch"},
	"recorder.normalize":               {"internal/core:TestLoudnessSettings"},
	"recorder.trim_silence":            {"internal/core:TestSilenceSettings"},
	"recorder.silence_db":              {"internal/core:TestSilenceSettings"},
	"recorder.boundary_window_ms":      {"internal/core:TestScenarios"},
	"recorder.session_timer":           {"internal/core:TestTheSessionTimerEndsTheSession"},
	"recorder.target_lufs":             {"internal/core:TestLoudnessSettings"},
	"recorder.pause_on_disk_full":      {"internal/core:TestAFullDiskStopsTheRecording"},
	"player.mute_output":               {"internal/core:TestScenarios"},
	"recorder.mute_ads":                {"internal/core:TestMuteAdsSilencesThePlayerDuringTheAd"},
	"recorder.skip_short_untitled":     {"internal/core:TestShortItemsWithNoAlbum"},
	"privacy.bug_tracker":              {"internal/ui:TestNothingLeavesWhenTheBugTrackerIsOff", "internal/ui:TestTheBugTrackerFollowsTheSetting"},
}

// A setting cannot ship without a test that shows it working: a new key in
// Table fails here until cases names one, and a renamed or removed test
// fails here too.
func TestEverySettingHasAFunctionalCase(t *testing.T) {
	root, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	tests := map[string]bool{}
	fset := token.NewFileSet()
	err = filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() || !strings.HasSuffix(path, "_test.go") {
			return err
		}
		f, err := parser.ParseFile(fset, path, nil, parser.SkipObjectResolution)
		if err != nil {
			return err
		}
		dir, _ := filepath.Rel(root, filepath.Dir(path))
		for _, decl := range f.Decls {
			if fn, ok := decl.(*ast.FuncDecl); ok && fn.Recv == nil && strings.HasPrefix(fn.Name.Name, "Test") {
				tests[filepath.ToSlash(dir)+":"+fn.Name.Name] = true
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range Table {
		names, ok := cases[d.Key]
		if !ok || len(names) == 0 {
			t.Errorf("%s has no functional test: add one and name it in cases", d.Key)
		}
		for _, n := range names {
			if !tests[n] {
				t.Errorf("%s: test %s does not exist", d.Key, n)
			}
		}
	}
	for key := range cases {
		if _, ok := Lookup(key); !ok {
			t.Errorf("cases lists %s, which is not a setting", key)
		}
	}
}
