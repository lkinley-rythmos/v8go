# v0.10.0-rc.5

This prerelease retains **V8 15.2.124.21** and the module path
`github.com/lkinley-rythmos/v8go`. It targets **Linux amd64 and arm64 with
glibc or musl**; macOS packages are unavailable.

## Changes since rc.4

- Add fail-closed native-source provenance. Musl archives attest the exact
  committed musl patch and every patched file hash, while glibc archives attest
  a clean pinned source checkout. `v8go_dirty` remains the unmodified raw Git
  observation; a verified musl patch is intentionally still dirty.
- Reject cached or downloaded archives whose provenance is absent or does not
  exactly match this module's committed V8 revision and musl patch metadata.
- Require a clean v8go and depot_tools checkout and the full pinned V8 source
  repository set; the musl patch is the sole allowed source dirtiness.

Library snapshots are for trusted local artifacts from trusted producers only.
Checksums detect corruption but do not authenticate a producer or make an
attacker-controlled snapshot safe. Rebuild snapshots when the library source,
runtime, V8 flags, platform, or compatible CPU changes.

Install the rc.5 native package into a fresh prefix, source its `env.sh` in a
clean shell, and rebuild the application. `go get` alone does not install native
dependencies. See [installation instructions](RELEASING.md#using-a-published-release).

Native packages are separate assets for the matching architecture and libc.
Each archive includes matching runtimes and headers, build provenance, license
notices, and a SHA-256 checksum. This is a prerelease for compatibility testing,
not a stable release. rc.4 tags and assets are untouched; rc.5 is a new
candidate.
