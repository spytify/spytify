package model

import (
	"fmt"

	"github.com/spytify/spytify/internal/settings"
	"github.com/spytify/spytify/internal/ui/lang"
)

// SettingWords is how Settings words d: the lexicon's label, note and
// values' names, the bitrate's made from its bitrates.
func SettingWords(d settings.Def) lang.Setting {
	w := lang.SettingTexts[d.Key]
	if d.Kind == settings.KindIntEnum {
		w.Choices = nil
		for _, kbps := range d.Ints {
			w.Choices = append(w.Choices, fmt.Sprintf(lang.BitrateChoice, kbps, float64(kbps)*60/8/1000))
		}
	}
	return w
}
