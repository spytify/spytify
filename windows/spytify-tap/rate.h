// The rate spytify-tap.exe records at, and when it must start again at
// another. Nothing here calls Windows, so this computer's C++ compiler tests
// it (internal/capture/taprate_test.go).
//
// Windows converts the player's audio to the mix rate of the output device
// it plays to (the device's format in Sound settings) before process
// loopback sees it, then to the rate loopback is asked for. Asking for the
// mix rate converts nothing twice: a device at 44.1 kHz gives the samples of
// a 44.1 kHz stream as they are, and a device in a low-quality mode, such as
// a Bluetooth headset in a call at 16 kHz, shows its rate.

#pragma once

#include <string>
#include <vector>

// kFallbackRate is the rate when no output device gives its mix rate.
constexpr unsigned long kFallbackRate = 48000;

enum class RateFrom { Given, Player, Default, Fallback };

struct Rate {
  unsigned long hz;
  RateFrom from;
};

// chooseRate takes --rate when given, else the mix rate of the device the
// player plays to, else that of the default device, else kFallbackRate. 0 is
// none.
inline Rate chooseRate(unsigned long given, unsigned long player, unsigned long byDefault) {
  if (given != 0) return {given, RateFrom::Given};
  if (player != 0) return {player, RateFrom::Player};
  if (byDefault != 0) return {byDefault, RateFrom::Default};
  return {kFallbackRate, RateFrom::Fallback};
}

// playerDevice is the device the player plays to, else one it has an audio
// session on, the default device first: a paused player keeps its sessions.
// Ids are endpoint ids, empty for none.
inline std::wstring playerDevice(const std::wstring &playing, const std::vector<std::wstring> &sessions,
                                 const std::wstring &byDefault) {
  if (!playing.empty()) return playing;
  for (const std::wstring &id : sessions) {
    if (id == byDefault) return id;
  }
  return sessions.empty() ? std::wstring() : sessions.front();
}

// moved says whether the player left the device the rate came from: it plays
// to another, or, while it plays to none, the default device, where it will
// most likely play, changed.
inline bool moved(const std::wstring &playing, const std::wstring &source, const std::wstring &defaultNow,
                  const std::wstring &defaultAtStart) {
  if (!playing.empty()) return playing != source;
  return defaultNow != defaultAtStart;
}
