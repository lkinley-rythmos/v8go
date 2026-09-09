# Building and releasing this fork

The module is `github.com/lkinley-rythmos/v8go`. `VERSION` records the v8go
release candidate (`0.10.0-rc.2`); `deps/VERSION` records the embedded V8
version (`15.2.124.21`). Update the engine pin, dependency pin, headers, and
native packages together. This RC targets Linux amd64 and arm64 with
glibc and musl. macOS support requires matching builds and tests before it can return.

## Using a published release

Install Go 1.25, Python 3.11 or newer, and Clang/LLD 22. LLVM provides Debian
and Ubuntu packages at https://apt.llvm.org/. Use `clang-22`, `clang++-22`,
and ensure the matching `ld.lld` is on PATH (typically `/usr/lib/llvm-22/bin`).
`deps/Dockerfile.test` is a reproducible Debian consumer environment.

From an application's Go module, after the RC has been published:

```sh
go mod download github.com/lkinley-rythmos/v8go@v0.10.0-rc.2
v8go_module=$(go env GOMODCACHE)/github.com/lkinley-rythmos/v8go@v0.10.0-rc.2
python3 "$v8go_module/deps/native.py" install \
  --release v0.10.0-rc.2 \
  --prefix "$HOME/.local/share/v8go/v0.10.0-rc.2"
. "$HOME/.local/share/v8go/v0.10.0-rc.2/env.sh"
go get github.com/lkinley-rythmos/v8go@v0.10.0-rc.2
go test ./...
```

Use a prefix without whitespace. The installer detects the host architecture and libc. Explicit platform names are
`linux_amd64`, `linux_arm64`, `linux_musl_amd64`, and `linux_musl_arm64`.
Use `--platform` to override detection for cross-target installation. The native
package must match both the target architecture and libc. Cross-compilation additionally
requires a target C toolchain/sysroot and the appropriate Go and Clang target
settings. Each prefix is immutable; use a new prefix for a new installation.

The installer verifies the release asset's SHA-256 and its release, platform,
and engine version before installing. It generates `env.sh` locally. Source
it once in each build shell. It supplies matching libc++ headers and library
search paths. Existing `CC`/`CXX` values are respected; use Clang 22 for both.
Do not substitute system libstdc++ or system libc++ headers: V8 uses Chromium's
custom libc++ ABI. The Go package reports this configuration error early.

The library is statically linked, so deployed applications do not need the
SDK directory. They still need compatible system libraries such as glibc.
Building on another glibc distribution may raise the executable's minimum
glibc requirement. Alpine uses separate musl packages; a glibc archive cannot
be substituted for a musl archive.

### Alpine consumers

Alpine 3.24.1 and edge are tested on both amd64 and ARM64. Install prerequisites:

```sh
apk add --no-cache build-base clang22 lld22 go python3
```

Then use the same installer command above; it detects musl automatically. The
SDK environment enables the matching libc++ musl configuration. Applications
still dynamically link musl; fully static binaries and older Alpine releases
are not part of this validation. `deps/Dockerfile.test-musl` supplies the CI
consumer environment, including the sanitizer package for leak checks.

### Building musl packages

The Linux composite action takes `target-libc: musl`. It installs native LLVM 23,
the pinned Rust compiler, musl standard library, bindgen, and rustfmt; exports an
Alpine 3.24.1 sysroot; and invokes `v8_compile.sh <x64|arm64> musl`.
ARM64 jobs use native ARM64 runners. Executable build tools stay on glibc, while
the archive's C++, libc++, libc++abi, and Rust objects target musl.

`deps/apply_musl_patch.py` checks exact build-file hashes before applying the
patch in `deps/patches/linux-musl.patch`. Review and regenerate this patch when
upgrading V8; an unexpected source revision fails the build instead of silently
applying a partial patch. It is safe to rerun against an already patched tree.
The patch leaves glibc behavior as the default.

Musl outputs are `deps/v8/out/release-musl` and
`deps/v8/out/release-arm64-musl`. Package them with `native.py package --platform
linux_musl_amd64` or `--platform linux_musl_arm64`. Each release includes all four
platform archives; both stable and edge consumer jobs must pass before release.
The Alpine sysroot and LLVM 23 packages follow their respective package branches;
Rust component downloads use pinned SHA-256 checksums.

## Building native packages

Initialize the pinned submodules, then synchronize V8's dependencies:

```sh
git submodule update --init deps/v8 deps/depot_tools
cd deps
sh ./v8_download.sh
sh ./v8_compile.sh x64
python3 v8/build/linux/sysroot_scripts/install-sysroot.py --arch=arm64
sh ./v8_compile.sh arm64
cd ..
python3 deps/native.py package --release "v$(cat VERSION)" --platform linux_amd64
python3 deps/native.py package --release "v$(cat VERSION)" --platform linux_arm64
```

`deps/Dockerfile.upgrade` supplies the build host tools. V8 downloads its pinned
Clang and Rust toolchains during synchronization. Set `V8_BUILD_JOBS` to bound
memory use. The Linux build also builds libc++ and libc++abi. Outputs remain in
`deps/v8/out/release` and `deps/v8/out/release-arm64`.

`v8_monolith` alone is insufficient for linking this engine. `native.py`
combines it with its transitive Rust archives and custom C++ runtimes into a
regular archive. It bundles matching C++ headers and license notices, and
writes compressed packages and checksums under `.build/dist`. Native binaries
are excluded from Git; GitHub rejects individual Git files larger than 100 MiB.
Public V8 headers remain in the module for normal cgo compilation and vendoring.

To test a local package, provide its archive and checksum explicitly:

```sh
archive=.build/dist/v8go_v0.10.0-rc.2_linux_amd64.tar.gz
python3 deps/native.py install --release v0.10.0-rc.2 \
  --platform linux_amd64 --archive "$archive" \
  --sha256 "$(cut -d ' ' -f 1 "$archive.sha256")" --prefix .build/sdk
. .build/sdk/env.sh
go test -count=1 ./...
go test -count=1 -tags leakcheck .
```

## Release sequence

1. Push the preparation branch and review its PR against this fork's `master`.
2. Require the CI native builds, full binding tests, and leak checks on both
   Linux architectures to pass. The workflow uses native ARM64 runners.
3. Merge the reviewed changes, and tag the exact release commit with
   `v$(cat VERSION)`. Do not reuse a published version tag.
4. Pushing the tag runs `release.yml`: it checks the tag against `VERSION`,
   rebuilds and tests both packages, then creates a **draft prerelease** with
   the archives, checksums, and `RELEASE_NOTES.md`.
5. Review the draft and publish it. Draft assets are not publicly downloadable,
   so installation from a release URL must be smoke-tested after publication.

The release workflow never automatically publishes a stable release. For
`v0.10.0`, update the version and release notes after RC validation, then review
the prerelease designation as part of publishing. The manual V8 version-check
workflow only reports a candidate upgrade; it does not commit binary changes.

Local packages made before committing have `v8go_dirty: true` in their manifest.
Release packages should be built by CI from the tagged commit.
