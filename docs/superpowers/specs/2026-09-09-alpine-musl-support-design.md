# Alpine Linux / musl package support

Status: approved; implementation and validation in progress.

## Outcome

Publish musl packages for Linux amd64 and ARM64 alongside the existing glibc
packages. Existing asset names and glibc installation commands remain valid.
Use Alpine 3.24.1 as the stable validation baseline and also test edge. This establishes tested Alpine support, not compatibility with every
musl distribution or fully static Go applications.

## Package contract

Retain `linux_amd64` and `linux_arm64` for glibc. Add `linux_musl_amd64` and
`linux_musl_arm64` as explicit package platforms, each with separate output
folders, compiler caches, CI artifacts, release assets, and manifest identity.

The installer must distinguish musl from glibc when automatically selecting a
package. Architecture alone is insufficient. Keep an explicit `--platform`
override for cross-target installation, and reject archive/manifest mismatches.
Use tests for both libc detection paths, explicit platform selection, archive
identity, and generated consumer compiler flags.

## Build strategy

Use Ubuntu runners matching the target CPU, including native ARM64 hardware.
Provide a pinned Alpine sysroot for the target C headers and libraries. Compile
V8, its matching libc++/libc++abi, and its transitive Rust dependencies for musl.
Do not reuse glibc-built native archives in the musl package.

Keep executable build tools (Torque, mksnapshot, Rust procedural macros,
bindgen, and rustfmt) on the host's glibc toolchain. Separate host toolchains
are required even when the host and target CPU match: the libc differs.

The pinned Chromium build checkout has no musl switch and hardcodes GNU Rust
target triples. Add a narrowly scoped, version-checked build patch covering
musl target triples, libc++ configuration, sysroot selection, and separate
host/snapshot toolchains. The patch must fail clearly if a V8 upgrade makes it
inapplicable. Inspect the complete generated command graph for accidental
host/target toolchain mixing before performing the expensive build.

Use the existing pinned Rust toolchain with matching musl standard libraries
where supported. Validate the standard-library link closure and unwind/runtime
requirements in the produced archive, rather than assuming a successful static
archive operation proves link compatibility.

An alternative is to build entirely inside Alpine. That avoids host/target libc
separation but requires replacing Chromium's glibc host tools and supplying a
newer Clang than Alpine 3.24's LLVM 22. Prefer the sysroot approach because it
preserves the current host compiler setup and isolates the new target behavior.

## CI and acceptance

Add musl build jobs for amd64 and ARM64. Run consumer validation in a pinned
Alpine container on the matching CPU without a glibc compatibility package.
Keep the current glibc build and consumer jobs.

For each musl artifact:

- Install the packaged SDK using the public installer.
- Run the Go test suite and leak checks against that installed SDK.
- Build and run the standalone consumer both normally and with vendoring.
- Exercise Intl and Temporal so C++, ICU, and Rust dependencies are covered.
- Inspect the consumer ELF interpreter and dynamic dependencies to confirm musl
  usage and absence of glibc requirements.

The release workflow must collect all four successfully tested package variants.
Document Alpine compiler prerequisites, installation, explicit platform names,
and the tested Alpine baseline. A separate minimal-runtime container can verify
that the consumer does not accidentally depend on build-only packages.

## Implementation sequence

1. Add libc-aware platform selection and archive identity tests.
2. Establish the musl sysroot and scoped V8 build configuration; inspect all
   generated host and target commands.
3. Build and link an amd64 consumer locally in Alpine; resolve libc-specific
   source or runtime issues with focused patches and tests.
4. Add the native ARM64 musl job and Alpine consumers for both architectures.
5. Require passing glibc and musl jobs before claiming release support.

## Evidence

- `deps/native.py` currently selects packages only by OS and architecture.
- `deps/v8/build/config/rust.gni` hardcodes Linux GNU triples.
- `deps/v8/build/toolchain/linux/BUILD.gn` uses GNU Linux compiler targets.
- The current native ARM64 work establishes separate paths for Clang, Rust,
  bindgen, rustfmt, and the packaging archiver.
- Alpine 3.24 release notes list LLVM 22 and Rust 1.96:
  https://www.alpinelinux.org/posts/Alpine-3.24.0-released.html
- rusty_v8 provides a relevant maintained example of target-scoped musl builds
  with separate glibc host tools; any adaptation must be reviewed against this
  repository's exact V8/build revisions:
  https://github.com/denoland/rusty_v8/blob/main/build.rs

## Allocator portability

Musl targets retain PartitionAlloc for explicit V8 allocations, but disable its
Linux malloc shim, which assumes glibc headers, exception declarations, and
`mallinfo`. Standard malloc calls use musl's allocator. ARM64 musl also disables
IFUNC-based memory tagging and uses the existing no-tagging fallback. The
AArch64 helper includes IFUNC headers only where they exist. Glibc host and
target settings retain their existing defaults.

The command-graph preflight rejects enabled allocator shims or memory tagging
in musl targets while allowing them in glibc host tools. Both architecture
graphs and the ARM64 tagging/page allocator sources were checked locally after
the initial CI failures; full build and Alpine consumer validation remain
required before release.
