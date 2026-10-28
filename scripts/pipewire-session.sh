#!/usr/bin/env bash
# Starts a headless PipeWire session (D-Bus, PipeWire, WirePlumber and a null
# sink) for the integration tests. No sound card is needed: the fake player
# and the capture share the null sink. Source it to get the environment:
#   . scripts/pipewire-session.sh
# It does not change the caller's shell options, and it checks each service
# through its socket, so a dead process from an earlier run is replaced.

export XDG_RUNTIME_DIR="${XDG_RUNTIME_DIR:-/tmp/spytify-xdg}"
mkdir -p -m 700 "$XDG_RUNTIME_DIR"
export PIPEWIRE_RUNTIME_DIR="$XDG_RUNTIME_DIR"
export DBUS_SESSION_BUS_ADDRESS="unix:path=$XDG_RUNTIME_DIR/bus"

spytify_wait() {
  for _ in $(seq 100); do
    "$@" >/dev/null 2>&1 && return 0
    sleep 0.1
  done
  echo "pipewire-session: timed out waiting for: $*" >&2
  return 1
}

if ! dbus-send --session --print-reply --dest=org.freedesktop.DBus / org.freedesktop.DBus.GetId >/dev/null 2>&1; then
  rm -f "$XDG_RUNTIME_DIR/bus"
  dbus-daemon --session --address="$DBUS_SESSION_BUS_ADDRESS" --fork --nopidfile
  spytify_wait dbus-send --session --print-reply --dest=org.freedesktop.DBus / org.freedesktop.DBus.GetId
fi

# Let the graph run at the stream's rate, so 44.1 kHz audio is not
# resampled to 48 kHz before it is recorded.
# SPYTIFY_PIPEWIRE_RATES=default keeps PipeWire's own rates instead: the
# graph at 48 kHz only, as on most computers.
rates_conf="$XDG_RUNTIME_DIR/config/pipewire/pipewire.conf.d/10-spytify-rates.conf"
if [ "${SPYTIFY_PIPEWIRE_RATES:-}" = default ]; then
  rm -f "$rates_conf"
else
  mkdir -p "$(dirname "$rates_conf")"
  cat >"$rates_conf" <<'EOF'
context.properties = {
    default.clock.rate          = 44100
    default.clock.allowed-rates = [ 44100 48000 ]
}
EOF
fi
unset rates_conf

if ! pw-cli info 0 >/dev/null 2>&1; then
  rm -f "$XDG_RUNTIME_DIR/pipewire-0" "$XDG_RUNTIME_DIR/pipewire-0.lock" "$XDG_RUNTIME_DIR/pipewire-0-manager" "$XDG_RUNTIME_DIR/pipewire-0-manager.lock"
  XDG_CONFIG_HOME="$XDG_RUNTIME_DIR/config" pipewire >"$XDG_RUNTIME_DIR/pipewire.log" 2>&1 &
  spytify_wait pw-cli info 0
fi

if ! wpctl status >/dev/null 2>&1 || ! pgrep -x -r S,R,D wireplumber >/dev/null 2>&1; then
  wireplumber >"$XDG_RUNTIME_DIR/wireplumber.log" 2>&1 &
  spytify_wait wpctl status
fi

if ! pw-dump 2>/dev/null | grep -q '"node.name": "spytify-null"'; then
  pw-cli create-node adapter '{ factory.name=support.null-audio-sink node.name=spytify-null node.description="Spytify null sink" media.class=Audio/Sink object.linger=true audio.position=[ FL FR ] }' >/dev/null
fi
spytify_wait sh -c 'wpctl status | grep -q "Spytify null sink"'
sink_id=$(pw-dump | python3 -c 'import json,sys; print(next(o["id"] for o in json.load(sys.stdin) if o.get("info",{}).get("props",{}).get("node.name")=="spytify-null"))' 2>/dev/null)
[ -n "$sink_id" ] && wpctl set-default "$sink_id" >/dev/null 2>&1
unset -f spytify_wait
echo "PipeWire session ready in $XDG_RUNTIME_DIR"
