#!/usr/bin/env bash
# Starts a headless PipeWire session (D-Bus, PipeWire, WirePlumber and a null
# sink) for the integration tests. No sound card is needed: the fake player
# and the capture share the null sink. Source it to get the environment:
#   . scripts/pipewire-session.sh
# It does not change the caller's shell options.

export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp/spytify-xdg}"
mkdir -p -m 700 "$XDG_RUNTIME_DIR"
export PIPEWIRE_RUNTIME_DIR="$XDG_RUNTIME_DIR"
export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"

if ! pgrep -x dbus-daemon >/dev/null; then
  dbus-daemon --session --address="$DBUS_SESSION_BUS_ADDRESS" --fork --nopidfile
fi
if ! pgrep -x pipewire >/dev/null; then
  pipewire >"$XDG_RUNTIME_DIR/pipewire.log" 2>&1 &
fi
for _ in $(seq 50); do
  [ -S "$XDG_RUNTIME_DIR/pipewire-0" ] && break
  sleep 0.1
done
if ! pgrep -x wireplumber >/dev/null; then
  wireplumber >"$XDG_RUNTIME_DIR/wireplumber.log" 2>&1 &
fi
for _ in $(seq 50); do
  pw-cli info 0 >/dev/null 2>&1 && break
  sleep 0.1
done
if ! pw-dump 2>/dev/null | grep -q '"node.name": "spytify-null"'; then
  pw-cli create-node adapter '{ factory.name=support.null-audio-sink node.name=spytify-null node.description="Spytify null sink" media.class=Audio/Sink object.linger=true audio.position=[ FL FR ] audio.rate=48000 }' >/dev/null
fi
for _ in $(seq 50); do
  wpctl status 2>/dev/null | grep -q 'Spytify null sink' && break
  sleep 0.1
done
wpctl set-default "$(pw-dump | python3 -c 'import json,sys; print(next(o["id"] for o in json.load(sys.stdin) if o.get("info",{}).get("props",{}).get("node.name")=="spytify-null"))' 2>/dev/null || echo 0)" >/dev/null 2>&1 || true
echo "PipeWire session ready in $XDG_RUNTIME_DIR"
