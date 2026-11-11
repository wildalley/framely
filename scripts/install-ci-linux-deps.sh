#!/usr/bin/env bash
# Install CI packages using a reachable Ubuntu mirror and Ubuntu's signing key.
set -euo pipefail

if [ "$#" -eq 0 ]; then
  echo "Usage: bash scripts/install-ci-linux-deps.sh PACKAGE..." >&2
  exit 2
fi
. /etc/os-release
if [ "$ID" != ubuntu ] || [ -z "${VERSION_CODENAME:-}" ]; then
  echo "This helper requires an Ubuntu runner" >&2
  exit 1
fi

work=$(mktemp -d)
trap 'rm -rf "$work"' EXIT
# apt's sandboxed download user must be able to read the temporary sources.
chmod 755 "$work"
for mirror in https://mirrors.edge.kernel.org/ubuntu https://archive.ubuntu.com/ubuntu; do
  reachable=true
  for suite in "$VERSION_CODENAME" "$VERSION_CODENAME-updates" "$VERSION_CODENAME-security"; do
    if ! curl --fail --silent --show-error --connect-timeout 5 --max-time 10 \
      "$mirror/dists/$suite/InRelease" -o "$work/InRelease"; then
      reachable=false
      break
    fi
  done
  if [ "$reachable" != true ]; then
    echo "Skipping unreachable Ubuntu mirror: $mirror" >&2
    continue
  fi
  echo "Installing CI dependencies from $mirror ($VERSION_CODENAME)"
  for suite in "$VERSION_CODENAME" "$VERSION_CODENAME-updates" "$VERSION_CODENAME-security"; do
    printf 'deb [signed-by=/usr/share/keyrings/ubuntu-archive-keyring.gpg] %s %s main restricted universe multiverse\n' "$mirror" "$suite"
  done > "$work/sources.list"
  options=(
    -o "Dir::Etc::sourcelist=$work/sources.list"
    -o Dir::Etc::sourceparts=-
    -o APT::Get::List-Cleanup=0
    -o APT::Update::Error-Mode=any
    -o Acquire::Retries=2
    -o Acquire::http::Timeout=20
    -o Acquire::https::Timeout=20
  )
  if sudo apt-get "${options[@]}" update && \
    sudo apt-get "${options[@]}" install --yes --no-install-recommends "$@"; then
    exit 0
  fi
done
echo "Unable to install CI dependencies from the configured Ubuntu mirrors" >&2
exit 1
