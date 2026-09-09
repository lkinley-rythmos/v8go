# Release architecture

Go wrappers call a C API in `v8go.h`; `v8go.cc` implements that API using V8.
The bindings and V8 must use the same public headers and custom libc++ ABI,
including for C++ values such as `std::shared_ptr` crossing the boundary.

The Go module ships wrapper sources and public V8 headers. Linux native
dependencies are installed separately from versioned, checksum-verified
release assets. Each package includes the monolithic V8 objects, transitive
Rust objects for Temporal, libc++/libc++abi, matching C++ headers and notices.
`deps/native.py` assembles the package and generates consumer build settings.

Separating native assets avoids GitHub's per-file Git size limit and lets
consumers download only their target architecture. The tradeoff is an explicit
native installation step before building a Go application. Applications still
use the normal import path `github.com/lkinley-rythmos/v8go`.

CI builds each Linux target, then installs and tests its package on a native
runner. Release tags must match `VERSION`. A release draft is created only
after both platforms pass; publication remains a separate review step.
