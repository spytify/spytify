package model

import (
	"slices"
	"testing"
	"unicode"
	"unicode/utf8"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/lang"
)

// The lexicon words every setting of the table, and no other, under a
// group it titles.
func TestEverySettingIsWorded(t *testing.T) {
	inTable := map[string]bool{}
	for _, d := range settings.Table {
		inTable[d.Key] = true
		if SettingWords(d).Label == "" {
			t.Errorf("%s has no label", d.Key)
		}
		if lang.SettingGroups[d.Group] == "" {
			t.Errorf("%s's group %q has no title", d.Key, d.Group)
		}
	}
	for key := range lang.SettingTexts {
		if !inTable[key] {
			t.Errorf("the lexicon words %s, which the table does not have", key)
		}
	}
}

// A list names every value of its setting, in order, with words, never
// the values config.toml keeps: every choice starts with a capital.
func TestEveryListNamesEachValue(t *testing.T) {
	for _, d := range settings.Table {
		values := len(d.Enum)
		if d.Kind == settings.KindIntEnum {
			values = len(d.Ints)
		}
		choices := SettingWords(d).Choices
		if values == 0 {
			if choices != nil {
				t.Errorf("%s takes no value from a list, yet has choices %q", d.Key, choices)
			}
			continue
		}
		if len(choices) != values {
			t.Errorf("%s: %d choices for %d values", d.Key, len(choices), values)
		}
		for _, c := range choices {
			if r, _ := utf8.DecodeRuneInString(c); unicode.IsLower(r) {
				t.Errorf("%s: choice %q starts lowercase", d.Key, c)
			}
		}
	}
}

// Each bitrate says what a minute of it takes, in decimal megabytes; the
// setting keeps the number.
func TestBitrateChoicesSayTheSizePerMinute(t *testing.T) {
	d, _ := settings.Lookup(settings.EncodeBitrateKbps.String())
	want := []string{
		"128 kbps · ~ 1.0 MB/min",
		"160 kbps · ~ 1.2 MB/min",
		"256 kbps · ~ 1.9 MB/min",
		"320 kbps · ~ 2.4 MB/min",
	}
	if got := SettingWords(*d).Choices; !slices.Equal(got, want) {
		t.Errorf("choices %q, want %q", got, want)
	}
}
