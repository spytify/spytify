#!/usr/bin/env bash
# CI only: apt-get install on a GitHub Ubuntu runner. Its Azure mirror
# sometimes stalls a download for over an hour (87 minutes once). apt drops
# a connection silent for 30 s and retries it itself; the update and the
# download get 5 minutes a try, three tries, the second and third from the
# main archive instead of the runner's mirror. Then the install reads the
# downloaded packages.
#   scripts/ci/apt-install.sh <package>...

opts=(-o Acquire::http::Timeout=30 -o Acquire::https::Timeout=30 -o Acquire::Retries=3)
for try in 1 2 3; do
  sudo timeout 300 apt-get "${opts[@]}" update -qq &&
    sudo timeout 300 apt-get "${opts[@]}" install -y -qq --download-only "$@" && break
  if [ "$try" = 3 ]; then
    echo "apt failed or stalled three times" >&2
    exit 1
  fi
  echo "apt failed or stalled (try $try), trying again from archive.ubuntu.com" >&2
  # ubuntu-24.04 lists its sources in deb822 form, older images in sources.list.
  for f in /etc/apt/sources.list.d/ubuntu.sources /etc/apt/sources.list; do
    if [ -f "$f" ]; then sudo sed -i 's|azure\.archive\.ubuntu\.com|archive.ubuntu.com|g' "$f"; fi
  done
done
exec sudo apt-get "${opts[@]}" install -y -qq "$@"
