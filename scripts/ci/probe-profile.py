# CI probe only (temporary): prints a spytify-tap capture block by block,
# 100 ms a character: # at or above -20 dBFS, - above -60, . below, 0 silent.
import math, struct, sys

data = open(sys.argv[1], "rb").read()
end = data.index(b"\n")
_, _, rate, channels = data[:end].decode().split()
rate, channels = int(rate), int(channels)
pcm = data[end + 1:]
n = len(pcm) // 4
samples = struct.unpack("<%df" % n, pcm[: n * 4])
block = rate * channels // 10
line = ""
loud = 0
first = None
for i in range(0, n, block):
    b = samples[i : i + block]
    rms = math.sqrt(sum(s * s for s in b) / len(b))
    db = 20 * math.log10(rms) if rms > 0 else -math.inf
    if db >= -20:
        line += "#"
        loud += 1
        first = i // block if first is None else first
    elif db > -60:
        line += "-"
    elif rms > 0:
        line += "."
    else:
        line += "0"
print(f"{sys.argv[1]}: {n / channels / rate:.2f} s, loud {loud / 10:.1f} s, first loud at {first / 10 if first is not None else None} s")
print(f"{sys.argv[1]}: {line}")
