#!/bin/bash
set -euo pipefail
: "${GITHUB_ENV:?This script configures the GitHub Actions build environment}"
root="$(cd "$(dirname "$0")/.." && pwd)"
sysroot="$root/.build/musl-sysroot"
mkdir -p "$sysroot"
docker build -t v8go-musl-sysroot - < "$root/deps/Dockerfile.musl-sysroot"
container=$(docker create v8go-musl-sysroot)
trap 'docker rm "$container" >/dev/null' EXIT
docker export "$container" | tar --no-same-owner -xf - -C "$sysroot"
echo "V8_MUSL_SYSROOT=$sysroot" >> "$GITHUB_ENV"
