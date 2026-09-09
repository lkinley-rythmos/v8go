# v0.10.0-rc.1

This fork upgrades V8 to **15.2.124.21**, based on the build setup in
upstream PR #416. Import the module as `github.com/lkinley-rythmos/v8go`.

This candidate supports **Linux amd64 and arm64 with glibc**. macOS and musl
are not supported by this candidate.

## Installation change

Native libraries are now separate release assets. Install the package for
your architecture, then source its `env.sh` before building Go applications.
`go get` alone does not install native dependencies. See
[installation instructions](RELEASING.md#using-a-published-release).

The packages contain V8, its Rust dependencies, custom libc++/libc++abi,
matching headers, build metadata, and third-party license notices. Each
archive has a `.sha256` checksum file. Go 1.25 and Clang/LLD 22 are the
tested consumer toolchain; no V8 source checkout is needed to use a package.

## Changes

- Restore Intl tests, including non-English ICU data.
- Keep amd64 and arm64 engine build outputs in separate directories.
- Test the linked engine version against `deps/VERSION`.
- Make the fetch example independent of external websites and dispatch
  promise resolution on the isolate's goroutine.
- Preserve nested public headers when using `go mod vendor`.

The release workflow requires full binding tests and leak checks on native
amd64 and arm64 runners before creating this draft release. This is a
prerelease for compatibility testing, not a stable release.
