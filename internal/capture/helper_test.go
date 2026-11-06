//go:build darwin || windows

package capture

import "testing"

// The output device's name, which the user chose, leaves the helper's
// stderr lines kept for its health; the rest of each line stays.
func TestTheHelpersLinesLoseTheDevicesName(t *testing.T) {
	for line, want := range map[string]string{
		"recording 4242 at 48000 Hz, the mix rate of Speakers (Jo's Headset)":                        "recording 4242 at 48000 Hz, the mix rate of a device",
		"recording 4242 at 48000 Hz, the mix rate of Speakers (Jo's Headset), the default output":    "recording 4242 at 48000 Hz, the mix rate of a device, the default output",
		"Speakers (Jo's Headset) now mixes at 44100 Hz":                                              "a device now mixes at 44100 Hz",
		"tapping player[101], player Helper[102] on Jo's AirPods at 48000 Hz (tap reports 48000 Hz)": "tapping player[101], player Helper[102] on a device at 48000 Hz (tap reports 48000 Hz)",
		"process 4242 has no audio session yet":                                                      "process 4242 has no audio session yet",
		"recording 4242 at 48000 Hz: no output device gives its mix rate (0x88890004)":               "recording 4242 at 48000 Hz: no output device gives its mix rate (0x88890004)",
	} {
		if got := deviceless(line); got != want {
			t.Errorf("deviceless(%q) = %q, want %q", line, got, want)
		}
	}
}
