#!/bin/sh

set -e

target_cpu="$1"
target_libc="${2:-glibc}"
case "$target_libc" in
  glibc|musl) ;;
  *) echo "Unsupported libc: $target_libc" >&2; exit 1 ;;
esac

case "$target_cpu" in
  x64) build_dir="./out/release" ;;
  arm64) build_dir="./out/release-arm64" ;;
  *)
    echo "Usage: $0 {x64|arm64}" >&2
    exit 1
    ;;
esac

if [ "$target_libc" = musl ]; then
  build_dir="$build_dir-musl"
fi

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
  # GN embeds this identifier in a rustc --cfg argument; avoid shell whitespace.
  gn_args="$gn_args
clang_base_path=\"$V8_CLANG_BASE_PATH\"
clang_version=\"23\"
rust_sysroot_absolute=\"$V8_RUST_SYSROOT\"
rust_bindgen_root=\"$V8_RUST_SYSROOT\"
rustc_version=\"$("$V8_RUST_SYSROOT/bin/rustc" -V | tr -d ' ()')\"
toolchain_supports_rust_thin_lto=false"
fi

if [ "$target_libc" = musl ]; then
  : "${V8_MUSL_SYSROOT:?musl builds require an Alpine sysroot}"
  : "${V8_RUST_SYSROOT:?musl builds require a custom Rust toolchain}"
  python3 "$dir/apply_musl_patch.py"
  gn_args="$gn_args
use_musl=true
use_glib=false
target_sysroot=\"$V8_MUSL_SYSROOT\"
host_toolchain=\"//build/toolchain/linux:clang_${target_cpu}_glibc\"
v8_snapshot_toolchain=\"//build/toolchain/linux:clang_${target_cpu}_glibc\""
fi

cd "${dir}/v8"

gn gen "$build_dir" --args="$gn_args"

if [ "$target_libc" = musl ]; then
  python3 "$dir/check_musl_commands.py" "$build_dir"
fi

gn args "$build_dir" --list > "${dir}/gn-args_${os}_${target_cpu}.txt"
echo "Effective build arguments saved to ${dir}/gn-args_${os}_${target_cpu}.txt"

(
  set -x
  ninja -C "$build_dir" -j "$cores" v8_monolith libc++ libc++abi
)

ls -lh "$build_dir"/obj/libv8_*.a

cd -
