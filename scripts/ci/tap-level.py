# CI only: checks what spytify-tap wrote: its header, and at least
# min_seconds of audio above min_dbfs (the tone is at -9 dBFS RMS),
# counted in 100 ms blocks, so silence anywhere in the capture counts for
# nothing.
import math, struct, sys

path, min_seconds, min_dbfs = sys.argv[1], float(sys.argv[2]), float(sys.argv[3])
data = open(path, "rb").read()
end = data.index(b"\n")
magic, sample, rate, channels = data[:end].decode().split()
assert magic == "SPYTIFY-TAP" and sample == "f32le", data[:end]
rate, channels = int(rate), int(channels)
pcm = data[end + 1:]
n = len(pcm) // 4
samples = struct.unpack("<%df" % n, pcm[: n * 4])
seconds = n / channels / rate


def dbfs(s):
    rms = math.sqrt(sum(x * x for x in s) / max(len(s), 1))
    return 20 * math.log10(rms) if rms > 0 else -math.inf


block = rate * channels // 10
heard = sum(dbfs(samples[i : i + block]) >= min_dbfs for i in range(0, n - block + 1, block)) / 10
print(f"{rate} Hz, {channels} channels, {seconds:.2f} s, {dbfs(samples):.1f} dBFS RMS, {heard:.1f} s of it above {min_dbfs} dBFS")
if heard < min_seconds:
    sys.exit(f"expected at least {min_seconds} s above {min_dbfs} dBFS")
