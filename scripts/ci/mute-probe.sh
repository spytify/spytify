#!/usr/bin/env bash
# CI only, an investigation for player.mute_output on Windows, not a test:
# whether WASAPI process loopback still records a process whose audio
# session is muted or at volume 0, set before the capture starts and while
# it runs, and whether Windows keeps that mute, and that volume, for the
# process's next run. It prints the numbers and fails on nothing.
set -u
tap=./spytify-tap.exe
tmp="$(cygpath -u "$RUNNER_TEMP")"

# tone starts tone.ps1 as tone <name>, which plays until <seconds> after
# its session's meter hears it, and prints its Windows PID (bash's $! is
# not one).
tone() {
  rm -f "$tmp/tone-$1.wav.playing"
  powershell -NoProfile -Command "(Start-Process powershell -ArgumentList '-NoProfile','-ExecutionPolicy','Bypass','-File','src/scripts/ci/tone.ps1','-Path','$RUNNER_TEMP\\tone-$1.wav','-PlayFor','$2' -WindowStyle Hidden -PassThru).Id" | tr -d '\r'
}

# heard waits for tone <name> to show on its session's meter.
heard() {
  for i in $(seq 60); do
    if [ -f "$tmp/tone-$1.wav.playing" ]; then
      echo "tone $1 shows on its session's meter after $i s"
      return
    fi
    sleep 1
  done
  echo "tone $1 does not show on its session's meter within 60 s"
}

# session waits for process <pid> to open an audio session.
session() {
  for i in $(seq 60); do
    if $tap --pid "$1" --get-volume > /dev/null 2>&1; then return; fi
    sleep 1
  done
  echo "process $1 opened no audio session within 60 s"
}

# state prints the mute and the volume of process <pid>'s session.
state() {
  echo "$2: mute $($tap --pid "$1" --get-mute | tr -d '\r'), volume $($tap --pid "$1" --get-volume | tr -d '\r')"
}

ended() {
  powershell -NoProfile -Command "if (Get-Process -Id $1 -ErrorAction SilentlyContinue) { exit 1 }"
}

kill_tone() {
  powershell -NoProfile -Command "Stop-Process -Id $1 -Force"
  sleep 2
}

# record captures process <pid> for <seconds> into <name>.raw and prints
# its level.
record() {
  timeout "$3" $tap --pid "$2" > "$1.raw" 2> "$1.err"
  python src/scripts/ci/mute-level.py "$1" "$1.raw"
}

# during captures process <pid> for 8 s into <name>.raw, runs the rest of
# its arguments 4 s after the capture starts, and prints the level.
during() {
  local name=$1 pid=$2
  shift 2
  timeout 8 $tap --pid "$pid" > "$name.raw" 2> "$name.err" &
  local rec=$!
  sleep 4
  "$@"
  wait "$rec"
  python src/scripts/ci/mute-level.py "$name" "$name.raw"
}

# saved prints what Windows saves of each app's audio session in the
# registry, where a mute or a volume can outlive the process.
saved() {
  echo "--- the registry's saved app audio state, $1:"
  MSYS_NO_PATHCONV=1 reg query 'HKCU\Software\Microsoft\Internet Explorer\LowRegistry\Audio\PolicyConfig\PropertyStore' /s
}

echo "=== 1. What process loopback records from a muted session, or one at volume 0"
a=$(tone a 900)
heard a
state "$a" "A, as it starts"
saved "before any change"
record baseline "$a" 4

$tap --pid "$a" --set-mute 1
state "$a" "A, muted"
record mute-before "$a" 4
$tap --pid "$a" --set-mute 0
during mute-during "$a" $tap --pid "$a" --set-mute 1
state "$a" "A, muted 4 s into the capture"
during unmute-during "$a" $tap --pid "$a" --set-mute 0
state "$a" "A, unmuted 4 s into the capture"

$tap --pid "$a" --set-volume 0
state "$a" "A, at volume 0"
record volume0-before "$a" 4
$tap --pid "$a" --set-volume 1
during volume0-during "$a" $tap --pid "$a" --set-volume 0
during volume1-during "$a" $tap --pid "$a" --set-volume 1
$tap --pid "$a" --set-volume 0.5
record volume-half-before "$a" 4
$tap --pid "$a" --set-volume 1
record baseline-again "$a" 4

echo "=== 2. Whether Windows keeps the mute after the process ends"
$tap --pid "$a" --set-mute 1
state "$a" "A, muted by one spytify-tap run, read by another"
saved "A muted and playing"
kill_tone "$a"
saved "A killed while muted"
b=$(tone b 900)
session "$b"
state "$b" "B, the next run after A was killed muted, as its session opens"
heard b
state "$b" "B, once heard or 60 s later"
record b-start "$b" 4

$tap --pid "$b" --set-mute 1
c=$(tone c 900)
session "$c"
state "$c" "C, started while B plays muted"
kill_tone "$c"

# D ends by itself, 20 s after it is heard: a player the user quits.
$tap --pid "$b" --set-mute 0
kill_tone "$b"
d=$(tone d 20)
session "$d"
heard d
$tap --pid "$d" --set-mute 1
state "$d" "D, muted"
for i in $(seq 90); do ended "$d" && break; sleep 1; done
ended "$d" && echo "D ended by itself" || echo "D still runs"
saved "D ended by itself while muted"
e=$(tone e 900)
session "$e"
state "$e" "E, the next run after D ended by itself muted"

echo "=== 3. Whether Windows keeps the volume after the process ends"
$tap --pid "$e" --set-mute 0
$tap --pid "$e" --set-volume 0.3
state "$e" "E, at 0.3"
kill_tone "$e"
f=$(tone f 900)
session "$f"
state "$f" "F, the next run after E was killed at 0.3"
saved "the end"
kill_tone "$f"
exit 0
