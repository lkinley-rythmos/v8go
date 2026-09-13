# v0.10.0-rc.4

This prerelease retains **V8 15.2.124.21** and the module path
`github.com/lkinley-rythmos/v8go`. It targets **Linux amd64 and arm64 with
glibc or musl**; macOS packages are unavailable.

## Changes since rc.3

- Add trusted library snapshots. A snapshot artifact restores independent
  library objects and closure state into each context, while ordinary contexts
  remain clean.
- Reject snapshot global templates with internal fields before context creation.
- Size the default musl pthread stack reservation at 8 MiB for supported SDK
  consumers, and correct V8's lower stack-bound handling for the initial,
  growable Linux musl thread.

Library snapshots are for trusted local artifacts from trusted producers only.
Checksums detect corruption but do not authenticate a producer or make an
attacker-controlled snapshot safe. Rebuild snapshots when the library source,
runtime, V8 flags, platform, or compatible CPU changes.

Install the rc.4 native package into a fresh prefix, source its `env.sh` in a
clean shell, and rebuild the application. `go get` alone does not install native
dependencies. See [installation instructions](RELEASING.md#using-a-published-release).

Native packages are separate assets for the matching architecture and libc.
Each archive includes matching runtimes and headers, build provenance, license
notices, and a SHA-256 checksum. This is a prerelease for compatibility testing,
not a stable release.
