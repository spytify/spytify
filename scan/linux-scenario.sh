#!/usr/bin/env bash
# Throwaway (scan-behaviour branch). The installed package on a desktop-like
# session (PipeWire, D-Bus, an X display): the window, the CLI, a recording
# of the fake player, and the login service. In strace mode every program
# start, connection, file write and signal is traced.
#   linux-scenario.sh strace|plain
mode="${1:-plain}"
scan="$(cd "$(dirname "$0")" && pwd)"
repo="$(dirname "$scan")"
report="$RUNNER_TEMP/report/linux-$mode"
mkdir -p "$report"
say() { echo "== $*"; echo "$(date -u +%FT%T.%3NZ) $*" >>"$report/marks.txt"; }

export XDG_RUNTIME_DIR=/tmp/spytify-xdg
unset DBUS_SESSION_BUS_ADDRESS
. "$repo/scripts/pipewire-session.sh" >"$report/pipewire-session.log" 2>&1
say "pipewire session: $(tail -n 1 "$report/pipewire-session.log")"

export DISPLAY=:99
if ! xdotool getdisplaygeometry >/dev/null 2>&1; then
  Xvfb :99 -screen 0 1280x800x24 -nolisten tcp >"$report/xvfb.log" 2>&1 &
  for _ in $(seq 50); do xdotool getdisplaygeometry >/dev/null 2>&1 && break; sleep 0.2; done
fi
glxinfo -B >"$report/glxinfo.txt" 2>&1
python3 -I "$scan/sentry-sink.py" 19090 "$report/sentry-sink.jsonl" &
sink=$!

calls='?execve,?execveat,?clone,?clone3,?fork,?vfork,?connect,?bind,?listen,?ptrace,?process_vm_readv,?process_vm_writev,?memfd_create,?openat,?readlink,?readlinkat,?rename,?renameat,?renameat2,?unlink,?unlinkat,?mkdir,?mkdirat,?chmod,?fchmod,?fchmodat,?symlink,?symlinkat,?link,?linkat,?kill,?tkill,?tgkill,?prctl,?setuid,?setgid,?setns,?unshare,?mount,?bpf,?perf_event_open,?init_module,?finit_module,?inotify_add_watch,?fanotify_init,?fanotify_mark'

# run_traced <phase> <command>...: the command, traced in strace mode.
run_traced() {
  local phase="$1"; shift
  if [ "$mode" = strace ]; then
    mkdir -p "$report/trace-raw/$phase"
    strace -ff -tt -y -s 400 -o "$report/trace-raw/$phase/t" -e "trace=$calls" "$@"
  else
    "$@"
  fi
}

# One file per phase, each line led by its pid, in time order.
merge_traces() {
  [ -d "$report/trace-raw" ] || return
  mkdir -p "$report/trace"
  for d in "$report"/trace-raw/*/; do
    phase="$(basename "$d")"
    for f in "$d"t.*; do
      [ -f "$f" ] || continue
      p="${f##*.}"
      sed "s/^/$p /" "$f"
    done | sort -k2,2 -s >>"$report/trace/$phase.txt"
  done
  rm -rf "$report/trace-raw"
}

cli() {
  local name="$1"; shift
  run_traced "cli-$name" spytify-cli "$@" >"$report/cli-$name.out" 2>"$report/cli-$name.err"
  say "cli $name: exit $?"
}

say "phase A: the window and the CLI"
SPYTIFY_SENTRY_DSN=http://scan@127.0.0.1:19090/1 run_traced ui spytify-ui >"$report/ui.log" 2>&1 &
for i in $(seq 90); do
  xdotool search --onlyvisible --name '^Spytify$' >"$report/window.txt" 2>/dev/null && break
  sleep 1
done
say "window: $(wc -l <"$report/window.txt") after ${i} s"
sleep 20
ps -eo pid,ppid,etimes,comm,args --forest >"$report/ps-idle.txt"
cli info info
cli info-json info --json
cli status status
cli get get
cli launch launch
cli start start
sleep 5
ps -eo pid,ppid,etimes,comm,args --forest >"$report/ps-start.txt"
cli status2 status
cli stop stop
cli shutdown shutdown
pkill -TERM -x spytify-ui
for _ in $(seq 20); do pgrep -x spytify-ui >/dev/null || break; sleep 1; done
pkill -KILL -x spytify-ui 2>/dev/null
pkill -KILL -x spytifyd 2>/dev/null

say "phase B: a recording of the fake player"
work="$RUNNER_TEMP/phase-b-$mode"
mkdir -p "$work/out"
(
  export SPYTIFY_TARGET_PROCESS=fakeplayer SPYTIFY_SOCKET="$work/s.sock" SPYTIFY_CONFIG="$work/config.toml" XDG_STATE_HOME="$work/state"
  "$RUNNER_TEMP/bin/fakeplayer" -start-delay 6s -lead-in 2s "$repo/testdata/timelines/basic.json" >"$report/fakeplayer.log" 2>&1 &
  run_traced daemon spytifyd --once --events "$report/events.jsonl" --idle-exit 10s --wait-target 20s \
    --set "output.dir=$work/out" >"$report/spytifyd-once.out" 2>"$report/spytifyd-once.err"
  echo "$?" >"$work/exit"
  wait
)
say "spytifyd --once: exit $(cat "$work/exit"); files: $(find "$work/out" -type f | wc -l)"
find "$work/out" -type f -printf '%P %s\n' >"$report/output-files.txt"
cp "$work/state/spytify/spytifyd.log" "$report/spytifyd-once.log" 2>/dev/null

if [ "$mode" = strace ]; then
  say "phase C: the login service"
  uid="$(id -u)"
  sudo loginctl enable-linger "$(id -un)"
  for _ in $(seq 30); do [ -S "/run/user/$uid/bus" ] && break; sleep 1; done
  (
    export XDG_RUNTIME_DIR="/run/user/$uid" DBUS_SESSION_BUS_ADDRESS="unix:path=/run/user/$uid/bus"
    cli service-install service install
    cp "$HOME/.config/systemd/user/spytifyd.service" "$report/spytifyd.service" 2>/dev/null
    systemctl --user --no-pager status spytifyd.service >"$report/systemctl-status.txt" 2>&1
    systemctl --user cat spytifyd.service >"$report/systemctl-cat.txt" 2>&1
    systemd-analyze --user security spytifyd.service >"$report/systemd-security.txt" 2>&1
    cli service-status service status
    cli service-uninstall service uninstall
  )
  sudo loginctl disable-linger "$(id -un)"
fi

kill "$sink" 2>/dev/null
sudo dmesg 2>/dev/null | grep -iE 'apparmor|audit|seccomp' | tail -n 200 >"$report/dmesg.txt"
merge_traces
say "done"
exit 0
