# v0.10.0-rc.3

This candidate retains **V8 15.2.124.21** and the module path
`github.com/lkinley-rythmos/v8go`.

It targets **Linux amd64 and arm64 with glibc or musl**. Musl packages are
validated on Alpine 3.24.1 and edge; macOS packages remain unavailable. The
release workflow requires every native build and consumer test to pass before
creating the draft release. This candidate is still being prepared.

## Changes since rc.2

- Restore Go's default `-O2 -g` when the native SDK installer generates
  `CGO_CXXFLAGS` and the caller has not supplied flags. Explicit user flags are
  preserved. Previously, sourcing `env.sh` compiled the C++ binding wrapper
  without optimization by default.
- In a controlled Linux amd64 comparison using the same rc.2 native SDK,
  four-argument JS→Go callbacks improved from 4.01 µs to 1.59 µs (median of five
  alternating one-second samples). Results depend on the workload and host.
- Add regression coverage for unset, empty, and custom compiler flags.

V8 and the binding API are unchanged. Install with the rc.3 module's installer
into a fresh prefix, source its `env.sh` in a clean shell, and rebuild your
application. A previously sourced SDK's flags are treated as explicit overrides. Existing
rc.2 installations can append `-O2 -g` to `CGO_CXXFLAGS` after sourcing their
environment and rebuild; the prebuilt V8 library needs no recompilation.

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
