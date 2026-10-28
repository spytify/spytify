// Where spytify-tap.exe writes each captured packet in its stream. Plain C++,
// so internal/capture/tapplacement_test.go tests it on any computer.

#pragma once

#include <cstdint>

// Placement is zeros silent frames, then the packet without its first skip
// frames.
struct Placement {
  uint64_t zeros;
  uint64_t skip;
};

// place puts a packet of frames frames, whose timestamp gives frame at, in a
// stream of rate frames a second that holds written frames.
//
// Timestamps stray by about a frame on continuous audio, so a packet whose
// timestamp is within 2 ms of the end of the stream follows it, losing and
// adding nothing. 2 ms is far over that jitter and under the audio engine's
// 10 ms period, the least a missing packet leaves out. Beyond it, the packet
// goes at its timestamp: silence fills the gap, or the packet loses the
// first frames that overlap what is written, such as silence written behind
// the clock. When the device's clock drifts from the computer's, that puts
// the stream back on the clock once it strays 2 ms.
inline Placement place(uint64_t written, uint64_t at, uint32_t frames, bool timestampError, uint32_t rate) {
  const uint64_t tolerance = rate / 500;
  if (timestampError || (at <= written + tolerance && written <= at + tolerance)) return {0, 0};
  if (at > written) return {at - written, 0};
  uint64_t skip = written - at;
  return {0, skip < frames ? skip : frames};
}
