# CI only: checks what spytify-tap wrote: its header, at least min_seconds
# of audio, and a level above min_dbfs (the tone is at -9 dBFS RMS).
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
rms = math.sqrt(sum(s * s for s in samples) / max(n, 1))
level = 20 * math.log10(rms) if rms > 0 else -math.inf
print(f"{rate} Hz, {channels} channels, {seconds:.2f} s, {level:.1f} dBFS RMS")
if seconds < min_seconds or level < min_dbfs:
    sys.exit(f"expected at least {min_seconds} s above {min_dbfs} dBFS")
