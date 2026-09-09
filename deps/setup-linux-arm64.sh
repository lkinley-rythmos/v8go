#!/bin/bash
# Host-native tools for the Linux ARM64 builder. V8's pinned Linux prebuilts
# are x86-64; use its supported custom-toolchain GN arguments instead.
set -euo pipefail

test "$(uname -m)" = aarch64
: "${GITHUB_ENV:?This script configures the GitHub Actions build environment}"
root="$(cd "$(dirname "$0")/.." && pwd)"

sudo apt-get install -yq ca-certificates gnupg
curl -fsSL https://apt.llvm.org/llvm-snapshot.gpg.key \
  | sudo gpg --dearmor --yes -o /usr/share/keyrings/llvm.gpg
printf '%s\n' 'deb [signed-by=/usr/share/keyrings/llvm.gpg] https://apt.llvm.org/noble/ llvm-toolchain-noble-23 main' \
  | sudo tee /etc/apt/sources.list.d/llvm.list
sudo apt-get update
sudo apt-get install -yq clang-23 lld-23 llvm-23 libclang-rt-23-dev libclang-23-dev

# Chromium expects the compiler runtime under a target-triple directory;
# Debian packages use lib/linux and put the architecture in the filename.
resource_dir="$(/usr/lib/llvm-23/bin/clang --print-resource-dir)"
sudo mkdir -p "$resource_dir/lib/aarch64-unknown-linux-gnu"
for runtime in builtins profile; do
  source="$resource_dir/lib/linux/libclang_rt.$runtime-aarch64.a"
  test -f "$source"
  sudo ln -sf "$source" "$resource_dir/lib/aarch64-unknown-linux-gnu/libclang_rt.$runtime.a"
done

# Pin Rust by date and verify the components against the upstream manifest.
rust_dir="$root/.build/toolchains/rust"
mkdir -p "$rust_dir"
temp_dir="$(mktemp -d)"
trap 'rm -rf "$temp_dir"' EXIT
for component in rustc rust-std rustfmt cargo; do
  archive="$component-nightly-aarch64-unknown-linux-gnu"
  case "$component" in
    rustfmt) checksum=4d67e002139f95eb07f269034a5cdff86061cfdaf241a9718659a2c50fa7f92b ;;
    cargo) checksum=be8de77366f457ae50d6ab073ef415e87fb3ab22bd158d9c61fc426b6473345f ;;
    rustc) checksum=8cce5a404ddb0351a4b5d16fb7b1bb58911b3189d8218195d59076be291a44a6 ;;
    rust-std) checksum=a10344341ded91a0edf4e1c98e85753cd39cf2af72ef0121eb5270998e26af16 ;;
  esac
  curl -fsSL "https://static.rust-lang.org/dist/2026-07-01/$archive.tar.xz" -o "$temp_dir/$archive.tar.xz"
  printf '%s  %s\n' "$checksum" "$temp_dir/$archive.tar.xz" | sha256sum --check
  tar -xJf "$temp_dir/$archive.tar.xz" -C "$temp_dir"
  "$temp_dir/$archive/install.sh" --prefix="$rust_dir" --disable-ldconfig
done

# bindgen is a separate host executable, not part of rustc. Match the
# bindgen version pinned in V8's tools/rust/build_bindgen.py.
PATH="$rust_dir/bin:$PATH" CARGO_HOME="$root/.build/toolchains/cargo" \
  "$rust_dir/bin/cargo" install bindgen-cli --version 0.72.1 --locked \
    --root "$rust_dir" --jobs 4
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
