# Alpine musl support implementation plan

**Goal:** Add Linux musl amd64 and ARM64 packages without changing existing glibc packages.
**Architecture:** Build musl library objects on matching Ubuntu CPU runners using an Alpine sysroot, with separate glibc host tools. Validate installed artifacts in Alpine stable and edge containers.
**Spec:** ../specs/2026-09-09-alpine-musl-support-design.md

- [ ] Add installer platform detection and tests for glibc/musl identity and overrides.
- [ ] Add a version-checked patch to the pinned build configuration for target-scoped musl C++, Rust, libc++, and sysroot selection; preserve glibc host tools.
- [ ] Add sysroot and matching musl Rust standard-library setup; inspect generated command graphs and build an amd64 archive locally.
- [ ] Link/run an installed amd64 consumer in Alpine stable and edge, including Intl/Temporal, normal and vendored builds, and ELF checks.
- [ ] Add native CPU CI jobs, musl package outputs, and stable/edge test matrix entries. Run unit tests, shell syntax checks, actionlint, and diff checks.
- [ ] Update documentation and PR with verified results and any pending native CI validation.

Use the existing feature branch and execute sequentially. Keep compiler setup, archive naming, and consumer validation reviewable. Do not claim musl support validated until the corresponding build and runtime checks pass.
