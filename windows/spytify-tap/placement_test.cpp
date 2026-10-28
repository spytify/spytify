// placement_test plays captured packets through place() as spytify-tap.exe
// does and prints "ok <case>" or "FAIL <case>: <why>" for each case.
// internal/capture/tapplacement_test.go builds and runs it.

#include <cstdint>
#include <cstdio>
#include <string>
#include <vector>

#include "placement.h"

template <typename... Args>
static std::string format(const char *f, Args... args) {
  char s[256];
  snprintf(s, sizeof s, f, args...);
  return s;
}

// Stream lays out packets as spytify-tap.exe writes them: each frame of out
// holds the number of the source frame it carries, or -1 for silence.
struct Stream {
  uint32_t rate;
  std::vector<int64_t> out;
  // worst is the farthest a packet landed from its timestamp, in frames.
  uint64_t worst = 0;
  uint32_t seed = 1;

  explicit Stream(uint32_t r) : rate(r) {}

  void packet(uint64_t at, int64_t first, uint32_t frames, bool timestampError = false) {
    Placement p = place(out.size(), at, frames, timestampError, rate);
    if (!timestampError) {
      uint64_t landed = out.size() + p.zeros - p.skip;
      uint64_t off = landed > at ? landed - at : at - landed;
      if (off > worst) worst = off;
    }
    out.insert(out.end(), p.zeros, -1);
    for (uint32_t i = p.skip; i < frames; i++) out.push_back(first + i);
  }

  // play captures count packets of 10 ms from clock frame clock and source
  // frame first, each timestamp off by up to a frame either way, the clock
  // gaining gain frames on the source every 10 packets. It returns the clock
  // after them and moves first past them.
  uint64_t play(uint64_t clock, int64_t &first, int count, int gain = 0) {
    uint32_t frames = rate / 100;
    for (int n = 0; n < count; n++) {
      seed = seed * 1103515245u + 12345u;
      int jitter = (int)((seed >> 16) % 3) - 1;
      packet(jitter < 0 && clock == 0 ? 0 : clock + jitter, first, frames);
      clock += frames;
      if (n % 10 == 9) clock += gain;
      first += frames;
    }
    return clock;
  }

  // silenceTo is main.cpp's silence written behind the clock while the
  // player renders nothing.
  void silenceTo(uint64_t frame) {
    if (frame > out.size()) out.resize(frame, -1);
  }

  int64_t sourceAt(uint64_t frame) const { return frame < out.size() ? out[frame] : -2; }
};

// Layout is what a stream holds: its runs of silence, and its source frames,
// with the runs of them that are missing.
struct Layout {
  bool inOrder = true;
  std::vector<uint64_t> silences;
  std::vector<uint64_t> losses;

  Layout(const std::vector<int64_t> &out, int64_t sourceFrames) {
    int64_t next = 0;
    for (size_t i = 0; i < out.size(); i++) {
      if (out[i] < 0) {
        if (i == 0 || out[i - 1] >= 0) silences.push_back(0);
        silences.back()++;
        continue;
      }
      if (out[i] < next) inOrder = false;
      if (out[i] > next) losses.push_back(out[i] - next);
      next = out[i] + 1;
    }
    if (next < sourceFrames) losses.push_back(sourceFrames - next);
  }

  // whole says whether every source frame is there once, in order, with
  // nothing between.
  bool whole() const { return inOrder && losses.empty() && silences.empty(); }

  std::string describe() const {
    unsigned long long silent = 0, lost = 0;
    for (uint64_t n : silences) silent += n;
    for (uint64_t n : losses) lost += n;
    return format("%llu silent frames in %llu runs, %llu source frames lost in %llu runs%s", silent,
                  (unsigned long long)silences.size(), lost, (unsigned long long)losses.size(),
                  inOrder ? "" : ", source frames out of order");
  }
};

static int failures = 0;

static void report(const std::string &name, const std::string &why) {
  if (why.empty()) {
    printf("ok %s\n", name.c_str());
  } else {
    printf("FAIL %s: %s\n", name.c_str(), why.c_str());
    failures++;
  }
}

// Timestamps off by a frame either way on continuous audio: every frame is
// written once, in order, with nothing between.
static void jitter(uint32_t rate) {
  Stream s(rate);
  int64_t first = 0;
  s.play(0, first, 2000);
  Layout l(s.out, first);
  report(format("timestamps off by a frame at %u Hz", rate), l.whole() ? "" : l.describe());
}

// The player stops for 50 ms: that much silence, give or take the
// timestamp's frame, and the audio whole around it.
static void gap() {
  Stream s(44100);
  int64_t first = 0;
  uint64_t clock = s.play(0, first, 500);
  s.play(clock + 2205, first, 500);
  Layout l(s.out, first);
  bool ok = l.inOrder && l.losses.empty() && l.silences.size() == 1 && l.silences[0] >= 2204 && l.silences[0] <= 2206;
  report("a 50 ms gap", ok ? "" : l.describe());
}

// The player renders nothing for a while, so silence is written behind the
// clock, then a packet arrives late, its timestamp inside that silence. One
// that overlaps it by less than the tolerance follows it whole; one that
// overlaps it by more loses its first frames, as many as overlap; one
// entirely behind is left out.
static void late() {
  Stream s(44100);
  int64_t first = 0;
  uint64_t clock = s.play(0, first, 100);
  Layout before(s.out, first);
  std::string why = before.whole() ? "" : "before the pause, " + before.describe();

  uint64_t end = clock + 4410;
  s.silenceTo(end);
  s.packet(end - 50, first, 441);
  if (why.empty() && (s.out.size() != end + 441 || s.sourceAt(end) != first))
    why = format("a packet 50 frames into the silence: its frame %lld first, %llu frames written",
                 (long long)(s.sourceAt(end) - first), (unsigned long long)(s.out.size() - end));
  first += 441;

  end = s.out.size() + 4410;
  s.silenceTo(end);
  s.packet(end - 300, first, 441);
  if (why.empty() && (s.out.size() != end + 141 || s.sourceAt(end) != first + 300))
    why = format("a packet 300 frames into the silence: its frame %lld first, %llu frames written",
                 (long long)(s.sourceAt(end) - first), (unsigned long long)(s.out.size() - end));
  first += 441;

  end = s.out.size();
  s.packet(end - 1000, first, 441);
  if (why.empty() && s.out.size() != end)
    why = format("a packet entirely behind wrote %llu frames", (unsigned long long)(s.out.size() - end));
  report("a late packet in the silence written behind the clock", why);
}

// A packet whose timestamp Windows flags as wrong goes right after what is
// written, whatever its timestamp says.
static void timestampError() {
  Stream s(44100);
  int64_t first = 0;
  uint64_t clock = s.play(0, first, 100);
  s.packet(0, first, 441, true);
  first += 441;
  s.packet(clock + 441000, first, 441, true);
  first += 441;
  s.play(clock + 882, first, 100);
  Layout l(s.out, first);
  report("timestamp errors", l.whole() ? "" : l.describe());
}

// The clock gains (or loses) a frame on the source every 10 packets. The
// stream stays within the tolerance of the clock (2 ms, 88 frames), and when
// it would stray further, one jump puts it back on the clock: silence when
// the clock is ahead, a cut when it is behind.
static void drift(int gain) {
  Stream s(44100);
  int64_t first = 0;
  s.play(0, first, 3000, gain);
  Layout l(s.out, first);
  const uint64_t tolerance = 88;
  const std::vector<uint64_t> &jumps = gain > 0 ? l.silences : l.losses;
  const std::vector<uint64_t> &none = gain > 0 ? l.losses : l.silences;
  std::string why;
  if (s.worst > tolerance)
    why = format("a packet landed %llu frames from its timestamp", (unsigned long long)s.worst);
  if (why.empty() && (!l.inOrder || !none.empty() || jumps.empty())) why = l.describe();
  for (uint64_t n : jumps) {
    if (why.empty() && n <= tolerance)
      why = format("a jump of %llu frames, within the tolerance (%s)", (unsigned long long)n, l.describe().c_str());
  }
  report(gain > 0 ? "a clock running ahead" : "a clock running behind", why);
}

int main() {
  jitter(44100);
  jitter(48000);
  gap();
  late();
  timestampError();
  drift(1);
  drift(-1);
  return failures > 0 ? 1 : 0;
}
