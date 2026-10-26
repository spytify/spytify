#!/usr/bin/env bash
# CI only: apt-get install on a GitHub Ubuntu runner. Its mirror sometimes
# stalls a download for over an hour (87 minutes once), and apt waits: the
# update and the download get 5 minutes a try, three tries, then the
# install reads the downloaded packages.
#   scripts/ci/apt-install.sh <package>...

for try in 1 2 3; do
  sudo timeout 300 apt-get update -qq &&
    sudo timeout 300 apt-get install -y -qq --download-only "$@" && break
  if [ "$try" = 3 ]; then
    echo "apt failed or stalled three times" >&2
    exit 1
  fi
  echo "apt failed or stalled (try $try), trying again" >&2
done
exec sudo apt-get install -y -qq "$@"
