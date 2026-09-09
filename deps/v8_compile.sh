#!/bin/sh

set -e

target_cpu="$1"

case "$target_cpu" in
  x64) build_dir="./out/release" ;;
  arm64) build_dir="./out/release-arm64" ;;
  *)
    echo "Usage: $0 {x64|arm64}" >&2
    exit 1
    ;;
esac

dir="$(cd "$(dirname "$0")" && pwd)"
v8_dir="${dir}/v8"

if [ ! -d "$v8_dir" ]; then
  echo "v8 not found at $v8_dir"
  exit 1
fi

depot_tools_dir="${v8_dir}/third_party/depot_tools"

if [ ! -d "$depot_tools_dir" ]; then
  depot_tools_dir="${dir}/depot_tools"
fi

PATH="${depot_tools_dir}:$PATH"
export PATH

# The V8 checkout contains a separate depot_tools checkout whose Python
# launcher must be initialized before invoking GN.
if [ ! -f "${depot_tools_dir}/python3_bin_reldir.txt" ]; then
  "${depot_tools_dir}/ensure_bootstrap"
fi

os=""
case "$(uname -s)" in
  Linux)
    os="linux"
    ;;
  Darwin)
    os="darwin"
    ;;
  *)
    echo "Unknown OS type"
    exit 1
esac

cores="2"

if [ "$os" = "linux" ]; then
  cores="$(grep -c processor /proc/cpuinfo)"
elif [ "$os" = "darwin" ]; then
  cores="$(sysctl -n hw.logicalcpu)"
fi

echo "Building V8 for $os $target_cpu"

# Allow memory-constrained builders to limit concurrent compiler processes.
cores="${V8_BUILD_JOBS:-$cores}"

cc_wrapper=""
if command -v ccache >/dev/null 2>&1 ; then
  cc_wrapper="ccache"
fi

gn_args="$(grep -v "^#" "${dir}/args/${os}.gn" | grep -v "^$")
cc_wrapper=\"$cc_wrapper\"
target_cpu=\"$target_cpu\"
v8_target_cpu=\"$target_cpu\""

# Native Linux ARM64 uses host-native tools instead of Chromium's x64 prebuilts.
if [ -n "${V8_CLANG_BASE_PATH:-}" ]; then
  gn_args="$gn_args
clang_base_path=\"$V8_CLANG_BASE_PATH\"
clang_version=\"23\"
rust_sysroot_absolute=\"$V8_RUST_SYSROOT\"
rustc_version=\"$("$V8_RUST_SYSROOT/bin/rustc" -V)\"
toolchain_supports_rust_thin_lto=false"
fi

cd "${dir}/v8"

gn gen "$build_dir" --args="$gn_args"

gn args "$build_dir" --list > "${dir}/gn-args_${os}_${target_cpu}.txt"
echo "Effective build arguments saved to ${dir}/gn-args_${os}_${target_cpu}.txt"

(
  set -x
  ninja -C "$build_dir" -j "$cores" v8_monolith libc++ libc++abi
)

ls -lh "$build_dir"/obj/libv8_*.a

cd -
