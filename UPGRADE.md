# V8 upgrade investigation

## Baseline and target

- Working branch: `upgrade/v8-stable`, based on PR #416 at
  `e58a792fa52e0485c15c31df7d4752202bec14ab`.
- PR baseline engine: 13.1.201.19. The initial checkout bundled this version; see the release preparation
  results below for the subsequent integration.
- Target verified on 2026-09-08 using ChromiumDash Linux Stable:
  Chrome 152.0.7977.82 / V8 **15.2.124.21**.
- Target V8 checkout: `deps/v8` at
  `4323497a6a73839e6d5260f6acd7ec0212cb3321`.
- Updated `deps/depot_tools` to the target V8's pinned revision
  `f394ab2c993283e94680ca13db98b99927868e98`. The PR's 2024 revision fails
  parsing the new build DEPS with `invalid "and" operand 'checkout_riscv64'`.

## Build environment

`deps/Dockerfile.upgrade` preserves the PR's Bookworm devcontainer distribution.
It supplies Go 1.25, GCC 12, Python 3.11,
Ninja, ccache, and the Linux ARM64 cross-compiler. Go and Python differ from
the PR's older CI versions. Build processes run as UID 1000.

V8 15.2 GN generation rejects the PR's GCC/system-libstdc++ configuration:
`The sandbox requires libc++ hardening`. The Linux GN configuration now uses
the synchronized Chromium Clang and custom hardened libc++ instead. The
downloaded Rust toolchain is also used by this V8 revision. ICU data is embedded
with `icu_use_data_file=false`. These are engine build changes; final cgo
linkage and C++ ABI compatibility still require validation.

From this checkout:

```sh
docker build -f deps/Dockerfile.upgrade -t v8go-upgrade:bookworm .
docker run --rm \
  --mount type=bind,src=/home/lk/Work/v8go,dst=/workspace \
  v8go-upgrade:bookworm go test .
docker run --rm \
  --mount type=bind,src=/home/lk/Work/v8go,dst=/workspace \
  -w /workspace/deps v8go-upgrade:bookworm \
  sh ./v8_download.sh 15.2.124.21
docker run --rm \
  --mount type=bind,src=/home/lk/Work/v8go,dst=/workspace \
  -w /workspace/deps -e V8_BUILD_JOBS=8 v8go-upgrade:bookworm \
  sh ./v8_compile.sh x64
docker run --rm \
  --mount type=bind,src=/home/lk/Work/v8go,dst=/workspace \
  -w /workspace/deps -e V8_BUILD_JOBS=8 v8go-upgrade:bookworm \
  sh -c 'python3 v8/build/linux/sysroot_scripts/install-sysroot.py --arch=arm64 && sh ./v8_compile.sh arm64'
```

The download script prints the bundled VERSION before applying its argument;
the explicit argument selects 15.2.124.21. It uses `gclient sync --reset`, so
do not run it over local edits inside the V8/dependency checkouts.

Build outputs are separated by architecture: `x64` uses `deps/v8/out/release`
(preserving the completed build), and `arm64` uses `deps/v8/out/release-arm64`.
New effective-argument logs are named `deps/gn-args_<os>_<arch>.txt` so they
also remain separate. The CI artifact upload follows the same directory mapping.

## Checks performed

- Baseline `go test .` passes inside the container.
- Host baseline `go test ./...` failed once in `TestCPUProfileNode`;
  three focused reruns passed. Treat as a timing-sensitive baseline issue.
- Compile-only `go build .` with `CGO_CPPFLAGS` pointing to the 15.2 headers
  passes with both host GCC 16 and container GCC 12, with upstream
  `-Wcomment` warnings. This does not verify linkage or runtime behavior.
- The PR skips `TestIntlSupport` despite enabling i18n in its GN flags.
  Restore coverage when the rebuilt engine is installed.

## Engine compilation result (2026-09-08)

- Dependency synchronization and its hooks completed successfully.
- GN generation and Linux x64 `v8_monolith` compilation succeeded using
  bundled Clang/libc++. Started with four jobs, then resumed with eight
  after checking memory usage.
- `mksnapshot` linked and ran successfully; embedded snapshot generation passed.
- Archive: `deps/v8/out/release/obj/libv8_monolith.a`, approximately 137 MiB,
  1,888 archive members, regular archive (not a thin archive).
- SHA-256: `08bdef945dedc44cd437e176b7bd3090be00e93db7dfd5b15da5cb859b6c306a`.
- Effective GN settings: `deps/gn-args_linux.txt` and
  `deps/v8/out/release/args.gn`.
- `sh -n deps/v8_compile.sh` and `git diff --check` pass.
- At this stage, the bundled bindings were still at the PR baseline.
  Subsequent integration results are recorded below.

### Linux ARM64

- Installed the Debian Bullseye ARM64 sysroot using V8's installer.
- GN generation and all 4,459 Ninja steps completed successfully with eight
  jobs, including host tools, embedded snapshot generation, and ARM64 objects.
- Archive: `deps/v8/out/release-arm64/obj/libv8_monolith.a`, 142,196,886 bytes
  (approximately 136 MiB), regular archive containing 1,892 members.
- `readelf -h` confirms all 1,892 members are AArch64 ELF objects.
- SHA-256: `38bf6438c870d0c0d0266959061b5ef88baf4e64417ae8f99d46ee18aeaff751`.
- Effective GN settings: `deps/gn-args_linux_arm64.txt` and
  `deps/v8/out/release-arm64/args.gn`.
- The x64 archive's SHA-256 remains unchanged from the result above.
- At this stage, ARM64 binding linkage and runtime tests had not been run.
  Subsequent linkage results are recorded below.

## Release preparation

- Fork: `git@github.com:lkinley-rythmos/v8go.git` (remote `fork`).
- Module/import path: `github.com/lkinley-rythmos/v8go`.
- Candidate: `v0.10.0-rc.1`; engine `deps/VERSION` and public headers now match
  V8 15.2.124.21. The upstream changelog's v0.10.0 entry was unpublished.
- The monolith does not contain its required Rust or custom C++ runtimes.
  `deps/native.py package` combines the complete dependency closure, removes
  Rust compiler metadata, and includes matching C++ headers and notices.
- Packages for Linux amd64 and arm64 are approximately 34 MiB compressed.
  They are release assets; native binaries are no longer tracked by Git.
- `deps/Dockerfile.test` uses stable Clang/LLD 22 and Go 1.25, without requiring
  a V8 checkout. The matching libc++ headers are provided by the installed SDK.
- Linux amd64: full `go test -count=1 ./...`, leak checks, and a separate
  consumer before/after `go mod vendor` pass against the final package.
- Linux ARM64: the binding test executable cross-links successfully using
  Clang 22. All 2,415 members of its packaged native library are AArch64 ELF
  objects. Native execution is delegated to the CI ARM64 runner.
- Intl coverage is restored. The fetch example now uses a local HTTP server
  and resolves its promise on the isolate's goroutine.
- Installer tests and actionlint pass. Existing C++ variable-length arrays
  produce Clang extension warnings; they do not fail the build.
- The original raw x64/arm64 engine outputs remain unchanged.

## Remaining release gates

1. Run the fork's CI builds and native tests on both Linux architectures.
2. Review and merge the preparation PR, then tag the release commit.
3. Review the workflow-generated draft prerelease, publish it, and test the
   public download/install path. Local packages have a dirty-worktree manifest
   and are validation artifacts; tagged CI builds supply release assets.
4. Build and validate macOS separately before adding it back to the support
   matrix. This first RC supports Linux glibc only.

See `RELEASING.md` for the reproducible workflow and installation commands.
