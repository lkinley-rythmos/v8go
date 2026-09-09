# CI reuse and rc.2 implementation

The user approved the CI audit recommendations and implementation, including a
version bump. Implement on the existing PR branch.

- [x] Add tested source/toolchain/SDK fingerprints. SDK keys include release,
  engine/depot pins, packaged headers/licenses, GN flags, patches and build tools;
  Go wrapper changes do not invalidate SDKs. Dependency keys exclude musl patches.
- [x] Restore exact SDK archives before initializing submodules. Cache pristine
  synchronized dependencies and pinned custom Rust/bindgen tools on build misses.
  Omit unused bundled compilers for custom-toolchain builds.
- [x] Restore/save ccache separately, retaining successful compilation entries
  after failed builds; reset and always report statistics.
- [x] Give each platform its own build/test workflow. Keep stable and edge Alpine
  tests and all release gates. Cancel superseded PR runs and gate builds on scripts.
- [x] Cache Docker layers and Go module/build caches, isolate consumer environments,
  refresh images weekly (edge daily), and use empty build contexts. Avoid redundant
  archive compression and duplicate format jobs.
- [x] Bump VERSION and current release documentation to 0.10.0-rc.2.
- [ ] Run regression tests, actionlint, syntax and cache-flow checks, request code
  review, fix findings, commit and push, then inspect the new CI run.

Keep source caches pristine by saving them before the musl patch is applied.
Cache hits still run binding, standalone consumer, vendoring and leak tests.
Native artifacts retain their original build provenance; no release tag is pushed.

Local checks: 27 regression tests, actionlint, shell syntax, pristine download
profile checks and real cold/warm Alpine Docker-layer reuse passed. Independent
review found no concrete defects. GitHub cold/warm SDK validation is pending.
