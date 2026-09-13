# v0.10.0-rc.6

This prerelease retains **V8 15.2.124.21** and the module path
`github.com/lkinley-rythmos/v8go`. It targets **Linux amd64 and arm64 with
glibc or musl**; macOS packages are unavailable.

## Changes since rc.5

- Accept a gclient nested repository directory only when it is itself an exact,
  clean, selected and pinned child checkout. This covers V8's independently
  pinned source children without ignoring untracked content or child failures.
- Run source attestation before GN/Ninja for every Linux target, after applying
  the musl patch where required, while retaining package-time verification.

Library snapshots are for trusted local artifacts from trusted producers only.
Checksums detect corruption but do not authenticate a producer or make an
attacker-controlled snapshot safe. Rebuild snapshots when the library source,
runtime, V8 flags, platform, or compatible CPU changes.

Install the rc.6 native package into a fresh prefix, source its `env.sh` in a
clean shell, and rebuild the application. `go get` alone does not install native
dependencies. See [installation instructions](RELEASING.md#using-a-published-release).

Native packages are separate assets for the matching architecture and libc.
Each archive includes matching runtimes and headers, build provenance, license
notices, and a SHA-256 checksum. This is a prerelease for compatibility testing,
not a stable release. rc.4 and rc.5 tags and assets are untouched; rc.6 is a
new candidate.
