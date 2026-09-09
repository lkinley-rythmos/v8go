# v0.10.0-rc.2

This candidate retains **V8 15.2.124.21** and the module path
`github.com/lkinley-rythmos/v8go`.

It targets **Linux amd64 and arm64 with glibc or musl**. Musl packages are
validated on Alpine 3.24.1 and edge; macOS packages remain unavailable. The
release workflow requires every native build and consumer test to pass before
creating the draft release. This candidate is still being prepared.

## Changes since rc.1

- Reduce Go/V8 boundary calls, temporary retained handles, and string copies.
- Add ordered property batching and direct primitive setters.
- Build ARM64 on native hardware with host-native LLVM/Rust/bindgen tools.
- Add separate musl packages and automatic libc detection in the installer.
- Include the full target Rust runtime, with Intl, Temporal, vendoring, and
  leak checks in consumer tests. Musl uses its own malloc and the allocator's
  existing fallback without IFUNC-based memory tagging.
- Reuse exact SDK packages when native inputs are unchanged; cache pristine
  dependencies and Rust tools; retain compiler caches after failed builds.
- Cache consumer toolchains and Go compilation, start tests per platform,
  cancel superseded PR runs, and upgrade Actions to Node 24 runtimes.

## Installation

Native dependencies are separate release assets. Install the package for your
architecture and libc, then source its `env.sh` before building Go applications.
`go get` alone does not install native dependencies. See
[installation instructions](RELEASING.md#using-a-published-release).

Packages contain V8 and its Rust dependencies, matching libc++/libc++abi and
headers, build provenance, and license notices. Each archive has a SHA-256
checksum. Consumers use Go 1.25 or newer and Clang/LLD 22; Alpine tests use the
distribution's Go toolchain. No V8 checkout is needed to use a package.

This is a prerelease for compatibility testing, not a stable release.
