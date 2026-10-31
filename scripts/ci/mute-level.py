# CI only, for the mute probe: prints the level of what spytify-tap wrote,
# overall and per half second, so a change in the middle of a capture
# shows where it lands. -inf is digital silence.
import math, struct, sys

name, path = sys.argv[1], sys.argv[2]
data = open(path, "rb").read()
if b"\n" not in data:
    print(f"{name}: no capture")
    sys.exit()
end = data.index(b"\n")
magic, sample, rate, channels = data[:end].decode().split()
assert magic == "SPYTIFY-TAP" and sample == "f32le", data[:end]
rate, channels = int(rate), int(channels)
pcm = data[end + 1:]
n = len(pcm) // 4
samples = struct.unpack("<%df" % n, pcm[: n * 4])


def dbfs(s):
    rms = math.sqrt(sum(x * x for x in s) / max(len(s), 1))
    return 20 * math.log10(rms) if rms > 0 else -math.inf


block = rate * channels // 2
blocks = [dbfs(samples[i : i + block]) for i in range(0, n - block + 1, block)]
peak = max((abs(x) for x in samples), default=0)
print(f"{name}: {n / channels / rate:.2f} s, {dbfs(samples):.1f} dBFS RMS, peak {peak:.4f}")
print(f"{name}: per 0.5 s: " + " ".join(f"{b:.1f}" for b in blocks))
