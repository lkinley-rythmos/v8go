# Native CI cache design

The native build matrix still covers glibc and musl on native amd64/ARM64.
Each platform calls `native-platform.yml`, whose consumer jobs depend only on
that platform's build. Musl runs both Alpine 3.24.1 and edge. The release job
continues to require the entire reusable workflow, including edge, to succeed.

## Cache boundaries

| Cache | Key inputs | Saved when |
| --- | --- | --- |
| Native SDK archive | Release, platform, V8/depot_tools gitlinks, sync configuration, LLVM/Rust pins, build scripts/action, GN flags, patches, packaged headers and license | Packaging succeeds |
| Pristine V8 dependencies | V8/depot_tools gitlinks, engine version, sync script/config, host architecture and custom/bundled compiler profile | Synchronization and ARM64 sysroot preparation succeed, before patching/compilation |
| Rust and bindgen | Host architecture, LLVM package pin, Rust checksum manifest, setup script | Both GNU and musl target stdlibs plus native bindgen install successfully |
| Compiler outputs | Host architecture, target architecture/libc; latest compatible cache restored, unique run/attempt saved | Compilation succeeds or fails, unless cancelled |
| Musl sysroot | Architecture and sysroot recipe/setup script | Sysroot creation succeeds |
| Consumer image layers | Architecture, consumer distribution, Dockerfile hash, refresh generation | Docker build succeeds |
| Consumer Go caches | Platform, distribution, Go/Clang/libc versions, SDK key, go.sum; unique run/attempt saved | Tests finish or fail, unless cancelled |

SDK keys deliberately omit Go wrapper source: all Go tests still execute against
the cached SDK. They include VERSION because the archives embed the release name.
A version bump therefore requires new packages. Cached manifests retain the
original native build commit and additionally record their input fingerprint;
CI installation verifies that fingerprint as well as the archive checksum.

Dependency caches exclude the top-level V8 `.git` and `out` directories. Cache
paths select immediate children, rather than recursively archiving the parent
and accidentally including excluded directories. The pinned root submodules
are initialized before restoration; nested public dependency repositories and
host tool bootstrap data are retained. No source cache restore-key fallback is
used. ARM64 glibc/musl share pristine sources and Rust tools, while compiled SDKs
and ccache remain separated by libc. Concurrent initial misses can still perform
duplicate setup; later runs reuse the winner's immutable cache.

Custom toolchain jobs select the declarative `deps/.gclient-custom` profile to
skip V8's unused bundled x86-64 Clang/Rust downloads. The default profile remains
`deps/.gclient`. Both profiles are checked with the pinned depot_tools parser in
the script gate; Python imports and conditionals are not valid gclient syntax. All
jobs suppress the two upstream WebAssembly test archive hooks, which are not
needed for v8go's own tests. LLVM is pinned in `deps/llvm-version`; update that
file intentionally when updating the custom compiler. Rust components retain
SHA-256 verification. Reinstalled compilers are identified by content in ccache,
and counters reset before each build so the reported hits belong to that run.

Consumer images use an empty context and BuildKit local layer export/import.
A fresh export replaces old blobs. Stable images refresh on the next run in a
new UTC ISO week; edge refreshes on the next UTC date. There is no cross-generation
layer fallback, so package installation reruns at refresh time. Go caches are
mounted into the test container and `-count=1` continues to disable test-result
reuse. Already compressed native artifacts upload with compression disabled.

## Operation and validation

Pushes to master populate caches reusable by PRs. PR-created caches remain scoped
to that PR; a subsequent master build may need to populate its own cache. GitHub
cache eviction remains possible and must only make builds slower, not incorrect.
Avoid changing key namespaces casually; old per-run caches expire through GitHub's
retention policy. Inspect cache storage if frequent native-input changes cause
churn. Exact SDK reuse reduces the number of runs writing new compiler caches.

The main CI workflow gates native work on script checks and cancels superseded
PR runs. It does not automatically cancel release runs. The duplicate formatting
workflow was removed; the script gate retains its checks.

Local validation covers fingerprint invalidation, SDK mismatch rejection,
installer safety, source profile configuration, actionlint and shell syntax.
A real Alpine image was built with an empty context, its layers exported, then
rebuilt using the exported cache; the package-install layer was reported CACHED.
Cold/warm source and SDK reuse, and failure-path saving, require GitHub Actions
validation. Expected warm SDK behavior: no submodule initialization, source sync,
compiler setup or V8 compilation; all six consumer jobs still run.
