#!/bin/bash
set -euo pipefail
: "${TARGET_PLATFORM:?Set TARGET_PLATFORM}"
: "${V8_BUILD_INPUT_KEY:?Set the expected native SDK fingerprint}"
mkdir -p .build/go-cache .build/go-mod
docker run --rm --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$PWD,dst=/workspace" \
  -e HOME=/tmp -e TARGET_PLATFORM -e V8_BUILD_INPUT_KEY \
  -e GOCACHE=/workspace/.build/go-cache -e GOMODCACHE=/workspace/.build/go-mod \
  v8go-consumer sh -ec '
    release="v$(cat VERSION)"
    archive=".build/dist/v8go_${release}_${TARGET_PLATFORM}.tar.gz"
    checksum=$(cut -d " " -f 1 "${archive}.sha256")
    python3 deps/native.py install --release "$release" --platform "$TARGET_PLATFORM" \
      --archive "$archive" --sha256 "$checksum" --prefix .build/sdk
    . .build/sdk/env.sh
    go test -count=1 ./...
    sh deps/test_consumer.sh
    go test -count=1 -tags leakcheck .
  '
