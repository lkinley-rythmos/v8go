#!/bin/bash
# Host-native tools for Linux ARM64 and musl builders. V8's pinned Linux prebuilts
# are x86-64; use its supported custom-toolchain GN arguments instead.
set -euo pipefail

arch="$(uname -m)"
case "$arch" in
  x86_64|aarch64) ;;
  *) echo "Unsupported host architecture: $arch" >&2; exit 1 ;;
esac
: "${GITHUB_ENV:?This script configures the GitHub Actions build environment}"
root="$(cd "$(dirname "$0")/.." && pwd)"

sudo apt-get install -yq ca-certificates gnupg
curl -fsSL https://apt.llvm.org/llvm-snapshot.gpg.key \
  | sudo gpg --dearmor --yes -o /usr/share/keyrings/llvm.gpg
printf '%s\n' 'deb [signed-by=/usr/share/keyrings/llvm.gpg] https://apt.llvm.org/noble/ llvm-toolchain-noble-23 main' \
  | sudo tee /etc/apt/sources.list.d/llvm.list
sudo apt-get update
llvm_version=$(cat "$root/deps/llvm-version")
sudo apt-get install -yq "clang-23=$llvm_version" "lld-23=$llvm_version" \
  "llvm-23=$llvm_version" "libclang-rt-23-dev=$llvm_version" "libclang-23-dev=$llvm_version"

# Chromium expects the compiler runtime under a target-triple directory;
# Debian packages use lib/linux and put the architecture in the filename.
resource_dir="$(/usr/lib/llvm-23/bin/clang --print-resource-dir)"
sudo mkdir -p "$resource_dir/lib/${arch}-unknown-linux-gnu"
for runtime in builtins profile; do
  source="$resource_dir/lib/linux/libclang_rt.$runtime-$arch.a"
  test -f "$source"
  sudo ln -sf "$source" "$resource_dir/lib/${arch}-unknown-linux-gnu/libclang_rt.$runtime.a"
done

# Pin Rust by date and verify the components against the upstream manifest.
rust_dir="$root/.build/toolchains/rust"
mkdir -p "$rust_dir"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
date=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["date"])' "$root/deps/rust-toolchain.json")
install_component() {
  local component="$1" target="$2" archive checksum
  archive="$component-nightly-$target"
  checksum=$(python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))["targets"][sys.argv[2]][sys.argv[3]])' \
    "$root/deps/rust-toolchain.json" "$target" "$component")
  curl -fsSL "https://static.rust-lang.org/dist/$date/$archive.tar.xz" -o "$temp_dir/$archive.tar.xz"
  printf '%s  %s\n' "$checksum" "$temp_dir/$archive.tar.xz" | sha256sum --check
  tar -xJf "$temp_dir/$archive.tar.xz" -C "$temp_dir"
  "$temp_dir/$archive/install.sh" --prefix="$rust_dir" --disable-ldconfig
}
# An exact toolchain cache contains both target standard libraries so ARM64
# glibc and musl jobs can share it. Still configure LLVM and exports on a hit.
if [ ! -f "$rust_dir/.v8go-complete" ]; then
  for component in rustc rust-std rustfmt cargo; do
    install_component "$component" "$arch-unknown-linux-gnu"
  done
  install_component rust-std "$arch-unknown-linux-musl"

  # bindgen is a separate host executable, not part of rustc. Match the
  # bindgen version pinned in V8's tools/rust/build_bindgen.py.
  PATH="$rust_dir/bin:$PATH" CARGO_HOME="$root/.build/toolchains/cargo" \
    "$rust_dir/bin/cargo" install bindgen-cli --version 0.72.1 --locked \
      --root "$rust_dir" --jobs 4
  touch "$rust_dir/.v8go-complete"
fi
ln -sf /usr/lib/llvm-23/lib/libclang.so "$rust_dir/lib/libclang.so"
"$rust_dir/bin/bindgen" --version
"$rust_dir/bin/rustfmt" --version

/usr/lib/llvm-23/bin/clang --version
"$rust_dir/bin/rustc" -Vv
{
  echo 'V8_CLANG_BASE_PATH=/usr/lib/llvm-23'
  echo "V8_RUST_SYSROOT=$rust_dir"
  echo 'V8_LLVM_AR=/usr/lib/llvm-23/bin/llvm-ar'
} >> "$GITHUB_ENV"
