#!/bin/sh

set -e

dir="$(cd "$(dirname "$0")" && pwd)"

PATH="${dir}/depot_tools:$PATH"
export PATH

export DEPOT_TOOLS_UPDATE=0

version="$(head -n1 "${dir}/VERSION" | cut -d'-' -f1)"

printf "Checking out V8 version %s\n" "$version"

branch="${1:-"$version"}"

test -n "$branch"

config=.gclient
if [ "${V8_CUSTOM_TOOLCHAIN:-0}" = 1 ]; then
  config=.gclient-custom
fi

(
  set -x
  gclient sync --gclientfile "$config" --no-history --reset -r "$branch"
)
