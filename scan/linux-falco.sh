#!/usr/bin/env bash
# Throwaway (scan-behaviour branch). The same session again, untraced, while
# Falco (with its incubating and sandbox rules when it accepts them) watches.
scan="$(cd "$(dirname "$0")" && pwd)"
report="$RUNNER_TEMP/report/falco"
rules="$RUNNER_TEMP/falco-rules"
mkdir -p "$report" "$rules"
for r in falco-incubating_rules falco-sandbox_rules; do
  curl -sSfL --retry 3 -o "$rules/$r.yaml" "https://raw.githubusercontent.com/falcosecurity/rules/main/rules/$r.yaml" || rm -f "$rules/$r.yaml"
done

start_falco() {
  local image="$1" extra="$2" mounts=()
  docker rm -f falco >/dev/null 2>&1
  sudo rm -f /tmp/falco_events.json
  if [ "$extra" = yes ]; then
    for f in "$rules"/*.yaml; do [ -f "$f" ] && mounts+=(-v "$f:/etc/falco/rules.d/$(basename "$f"):ro"); done
  fi
  docker run -d --name falco --privileged -v /tmp:/tmp -v /var/run/docker.sock:/host/var/run/docker.sock \
    -v /proc:/host/proc:ro -v /etc:/host/etc:ro "${mounts[@]}" "$image" \
    falco -o json_output=true -o file_output.enabled=true -o file_output.keep_alive=false \
    -o file_output.filename=/tmp/falco_events.json -o engine.kind=modern_ebpf >/dev/null 2>&1 || return 1
  sleep 15
  [ "$(docker inspect -f '{{.State.Running}}' falco 2>/dev/null)" = true ]
}

ran=""
for extra in yes no; do
  for image in falcosecurity/falco:0.45.2 falcosecurity/falco-no-driver:0.45.2 falcosecurity/falco:latest; do
    if start_falco "$image" "$extra"; then ran="$image extra-rules=$extra"; break 2; fi
    docker logs falco >"$report/failed-$(echo "$image" | tr '/:' '__')-$extra.log" 2>&1
  done
done
if [ -z "$ran" ]; then echo "Falco did not start"; exit 1; fi
echo "$ran" >"$report/falco-ran.txt"
echo "== Falco running: $ran"

bash "$scan/linux-scenario.sh" plain

sleep 5
docker stop falco >/dev/null
docker logs falco >"$report/falco.log" 2>&1
sudo cp /tmp/falco_events.json "$report/falco_events.json" 2>/dev/null && sudo chown "$(id -u):$(id -g)" "$report/falco_events.json"
echo "== Falco events: $(wc -l <"$report/falco_events.json" 2>/dev/null || echo 0)"
exit 0
