#!/bin/bash
# Cache layers locally for actions/cache without pushing container images.
set -euo pipefail
: "${CONSUMER:?Set CONSUMER to glibc or an Alpine version}"
mkdir -p .build/empty-context
args=()
if [ "$CONSUMER" = glibc ]; then
  dockerfile=deps/Dockerfile.test
else
  dockerfile=deps/Dockerfile.test-musl
  args+=(--build-arg "ALPINE_VERSION=$CONSUMER")
fi
if [ -f .build/docker-cache/index.json ]; then
  args+=(--cache-from type=local,src=.build/docker-cache)
fi
builder="v8go-consumer-$$"
docker buildx create --name "$builder" --driver docker-container --use
trap 'docker buildx rm "$builder" >/dev/null' EXIT
docker buildx build --load --pull -t v8go-consumer -f "$dockerfile" \
  --cache-to type=local,dest=.build/docker-cache-new,mode=max \
  "${args[@]}" .build/empty-context
# Use a fresh export directory so unreferenced historical blobs do not accumulate.
rm -rf .build/docker-cache
mv .build/docker-cache-new .build/docker-cache
