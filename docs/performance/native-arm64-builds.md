# Native ARM64 builds

The Linux ARM64 build and consumer tests use GitHub's `ubuntu-24.04-arm`
runner. The amd64 jobs use `ubuntu-24.04`. Both builds use four compiler jobs.

The pinned V8 checkout supplies x86-64 Linux compiler binaries, so the ARM64
builder installs Clang/LLVM 23 from apt.llvm.org and Rust nightly 2026-07-01
from rust-lang.org. Rust downloads have pinned SHA-256 checksums. LLVM packages
are verified through the signed apt repository and follow its version 23 branch.
The setup step prints the exact compiler versions in the build log.

`deps/setup-linux-arm64.sh` exports the custom Clang and Rust paths through
GitHub's environment file. `deps/v8_compile.sh` passes V8's supported GN
arguments for custom toolchains and disables Rust ThinLTO interoperability,
since the Rust and C++ compilers do not share a pinned LLVM revision. V8's
matching libc++ and libc++abi sources are still built and packaged. Packaging
uses the native LLVM archiver through `V8_LLVM_AR`.

Compiler caches include the host architecture as well as the target architecture,
so the native ARM64 build cannot restore the earlier x86 cross-build cache.
Its first build therefore starts cold. The composite action retains support for
ARM64 cross-compilation when called on an x86 runner.

The native consumer test job installs the resulting archive and runs the Go
suite, standalone consumer checks, and leak checks on ARM64. A successful source
build alone does not establish that the resulting package works for consumers.
