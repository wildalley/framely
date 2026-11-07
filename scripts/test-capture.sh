#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
mkdir -p build
cc -O2 -Wall -Wextra -Werror -std=c11 workers/capture/pipewire_test.c -o build/capture-pixels-test $(pkg-config --cflags --libs libpipewire-0.3)
build/capture-pixels-test
