# Library snapshots

The snapshot API serializes a trusted JavaScript library into a local artifact.
Each restored context receives independent library objects and closure state.
Ordinary `NewContext` calls on the same isolate use a clean default context.
This extension uses the existing rc.3 native SDK; it does not add a new native
binary or a persistent request context.

## API and lifecycle

1. Set the required process-wide V8 flags before creating isolates or snapshots.
2. Call `CreateSnapshot(source, exports)` in a build process. The source runs in
   a separate library context. Each named global must be defined, non-undefined,
   and deletable. Its value is saved in export order and removed from the global.
   Export names must be nonempty, unique, and contain no NUL bytes. Source must
   be nonempty and contain no NUL bytes. The default context remains clean.
3. In the consumer, call `SnapshotSourceHash(blob)` and compare the result with
   the SHA-256 of the exact expected source. Call `NewIsolateWithSnapshot(blob)`
   to load it. The isolate copies and owns the native bytes until `Dispose`, so
   the caller may release or reuse the input slice after the call returns.
4. Call `NewContextFromSnapshot(iso, global)` for each independent realm. The
   optional global object template must belong to that isolate. Its properties
   and Go callbacks are instantiated in the restored realm, with rc.3 builtin
   collision precedence, descriptors, and property ordering preserved.
5. Call `ctx.SnapshotData(index)` once for each desired export, using the
   zero-based order passed to `CreateSnapshot`. Bind or use the returned value
   in that context. Repeated retrieval of an index is an error.
6. Close every context before disposing its isolate. Context values become
   invalid when the context closes. As with the rest of v8go, callers must
   serialize access to each isolate, including creation, retrieval and disposal.

`ErrSnapshot` identifies invalid envelopes and API misuse through `errors.Is`.
JavaScript builder or restoration exceptions are returned as wrapped `JSError`
values. Each isolate has its own Go callback registry; callbacks are supplied
after restoration and are never stored in the artifact.

## Trust and compatibility

Load only artifacts generated from trusted local source by a trusted producer.
The envelope checks magic, length, JSON metadata, V8 version, OS, architecture,
the V8 cached-data compatibility tag (flags and CPU), and SHA-256 checksums.
Checksums detect corruption; they do not authenticate the producer or make
attacker-controlled V8 snapshots safe. The source hash records an identity;
it does not prove that the payload was generated from that source. Do not expose
snapshot deserialization as an upload API.

Build and consume with the same native SDK, V8 flags, platform and compatible
CPU. Keep flags unchanged after initialization. The envelope does not encode
every native build setting or distinguish glibc from musl, so its validation
does not replace matching the build environment. Regenerate artifacts whenever
the library source or runtime changes. The binding release supports Linux;
qualification for each architecture and libc remains a separate release check.

## Runtime limitations

Snapshot construction executes source synchronously without a timeout. Use only
trusted library initialization code that terminates. Do not include Go callbacks,
request data, native pointers, pending asynchronous work, or external resources
in source. Export removal does not undo other global or builtin mutations made
by the library. Sources should expose their library through the requested
deletable globals and avoid other global mutations. Function machine code is
cleared when the blob is created; functions compile as used after restoration.
This API provides fresh realms, not isolation from hostile snapshot producers.
