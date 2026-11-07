#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/.."
case "$(uname -m)" in x86_64) capture_arch=amd64;; aarch64) capture_arch=arm64;; *) echo 'Unsupported architecture' >&2;exit 1;;esac
mkdir -p "resources/linux-$capture_arch/workers"
cc -O2 -Wall -Wextra -Werror -std=c11 workers/capture/pipewire.c -o "resources/linux-$capture_arch/workers/screenpin-capture" $(pkg-config --cflags --libs libpipewire-0.3)
