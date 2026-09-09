# Go–V8 Boundary Performance Implementation Plan

> **For agentic workers:** Use superpowers:executing-plans to implement this plan task by task. Steps use checkbox syntax for tracking. Implementation was authorized in the follow-up user request on 2026-09-08.

**Goal:** Reduce Go–V8 crossing overhead, temporary native allocations, and unnecessary persistent handles across the six opportunities identified in the repository review.

**Architecture:** Keep existing Go entry points and implement more complete operations inside individual C++ bridge calls. Preserve explicit value ownership and isolate locking. Introduce one additive bulk property API after the internal optimizations have been measured.

**Tech stack:** Go/cgo, C++20, the repository's pinned V8 15.2.124.21 and matching native SDK; Linux amd64 and arm64.

**Spec:** The six findings from the repository review are specified by the design decisions and acceptance criteria in this document. No separate design document exists.

## Design decisions and constraints

- Preserve the `go 1.22` module directive and existing public signatures. Validate with the supported build toolchain documented in `RELEASING.md`; do not introduce newer Go APIs without a separate compatibility decision.
- Keep the pinned V8 version and matching libc++ ABI. Avoid engine upgrades, new dependencies, or whole-file reorganizations.
- Keep `Locker`, isolate scopes, and required handle/context scopes. Go goroutines may move between OS threads, and callbacks may reenter V8.
- Existing caller-owned `Value` objects and callback arguments retain their current lifetime. Do not automatically release callback arguments or pool wrappers that callers may retain.
- Optimize temporary handles that never escape the bridge. Do not release caller-provided `Valuer` arguments.
- Preserve dynamic property lookup, getters, proxies, receiver binding, numeric conversion, and exception behavior unless an explicitly identified correction is covered by regression tests.
- A borrowed Go string pointer is read-only and valid only for the synchronous bridge call. V8 must copy it before return; no external V8 string may retain Go memory.
- Treat timings as unmeasured until a successful benchmark run. Report Go allocations separately from C++ allocations, V8 handles, and process memory.

### Alternatives considered

1. **Incremental bridge changes — selected.** Each change has a focused benchmark and can be retained or reverted independently.
2. **A new scoped/borrowed value API.** Could remove more callback handle allocations, but changes ownership rules and creates escape hazards. Defer it.
3. **An isolate worker thread or fast-call subsystem.** Would change scheduling and reentrancy substantially. Defer until profiles show the incremental changes are insufficient.

## Execution order

Baseline → finding 4 (direct handles) → finding 2 (primitive setters) → finding 1 (callbacks) → finding 5 (strings) → finding 3 (method calls) → finding 6 (batching).

The order establishes valid handle accounting first, makes shared string marshalling available to method calls and batches, and leaves the public API addition until the internal changes are measured. Keep changes reviewable by task. Do not combine unrelated fixes.

## Task 0: Establish correctness and performance baselines

**Files:** Read `RELEASING.md`, `cgo.go`, `.github/workflows/native.yml`, `leakcheck.go`; create `boundary_benchmark_test.go`; extend `export_test.go` and native bridge declarations only if needed for test-only handle accounting.

**Produces:** Repeatable `BenchmarkBoundary` sub-benchmarks and before/after result files outside tracked source.

- [x] Resolve the build environment before collecting timings. The previous attempt failed because the default Go cache was read-only and the temporary cache exhausted the disk quota. Choose writable cache and build-temp locations with sufficient free quota. Do not delete unrelated user files. Source a native SDK environment whose paths actually exist on this host; `.build/sdk-final-amd64/env.sh` currently contains `/workspace` paths and cannot be assumed usable here.
- [x] Run the existing tests before editing bridge behavior. Record compiler versions, Go version, V8 pin, CPU architecture, native SDK, and commit ID with results.
- [x] Inspect `m_ctx::nextValId`: both `new m_ctx` sites currently leave this scalar uninitialized. Initialize it to zero before relying on retained-handle IDs in new tests. Isolate this correctness prerequisite in its own commit if confirmed in the execution checkout.
- [x] Add steady-state sub-benchmarks using one isolate/context per benchmark, initialized before `b.ResetTimer()`. Release returned values inside the measured iteration. Dispose the context before the isolate afterward.

| Benchmark family | Cases | Required measurement |
| --- | --- | --- |
| Call | Cached JS function; 0, 1, 4, 16 arguments | Time and Go allocations per call |
| Callback | JS loop invoking Go; 0, 1, 4, 16 arguments | Time per callback; retained handles before/after |
| Set | `int32`, `float64`, string, existing `Value`; named/indexed/internal-field variants | Time; internal-context handle growth |
| MethodCall | Direct method call and cached function call | Time; retained handles after releasing results |
| String | Input and output at 0, 16, 1024, 65536 bytes; ASCII and Unicode | Time, bytes processed, allocation behavior |
| Batch | 1, 8, 64 properties using repeated scalar calls initially | Comparable baseline for the final API |

- [x] Use `defer info.Release()` in callback benchmarks returning `nil`. Do not release a callback argument before returning that same argument to C++. Keep setup, compilation, and console output outside measured loops.
- [x] Add an internal-context retained-count test hook if required: public `Context.RetainedValueCount()` does not see values created by `NewValue` in the isolate's internal context. Acquire the isolate lock for native accounting. Keep the Go hook in `export_test.go`; do not add a production public diagnostic API.
- [x] Run and save the baseline:

```sh
go test -count=1 ./...
go test -run '^$' -bench '^BenchmarkBoundary/' -benchmem -benchtime=200ms -count=10 .
```

**Acceptance:** Tests pass in a configured environment; benchmark loops have bounded intentional retention; results are reproducible enough to compare repeated samples. If disk/toolchain setup remains blocked, record the blocker and do not substitute estimated performance numbers.

## Task 1: Construct persistent handles directly — finding 4

**Files:** Modify `v8go.cc`; inspect `v8go.h` and `deps/include/v8-persistent-handle.h`; exercise `value_test.go`, `function_test.go`, `function_template_test.go`, `promise_test.go`.

**Interfaces:** No public or C ABI change. Existing `m_value::ptr` and `tracked_value` remain.

- [x] Enumerate temporary-persistent assignment and nested `Reset` patterns. The custom `CopyablePersistentTraits` resets in its destructor; the problem is allocation/copy churn, not a leaked temporary persistent.
- [x] Replace creation followed by persistent-copy assignment with direct reset from the original local handle:

```cpp
// Before
val->ptr = Persistent<Value, CopyablePersistentTraits<Value>>(iso, result);
// After
val->ptr.Reset(iso, result);

// Callback argument equivalent
val->ptr.Reset(iso, info[i]);
```

- [x] Preserve `id`, `iso`, `ctx`, tracking, and explicit release behavior. Do not migrate the entire repository to another handle class in this task.
- [x] Run the existing value/function/callback/promise tests and leak checks. Compare call, callback, and setter benchmarks against Task 0.

**Acceptance:** Equivalent values and lifetimes; no new leak reports; eliminated temporary global-reference copies on the changed paths; no repeatable material performance regression.

## Task 2: Avoid primitive setter temporaries — finding 2

**Files:** Modify `object.go`, `v8go.h`, `v8go.cc`; extend `object_test.go` and `boundary_benchmark_test.go`; reuse Task 0's internal-context accounting.

**Interfaces:** Keep `Set`, `SetIdx`, and `SetInternalField` signatures. Add private native setter entry points for primitive inputs. Continue using the existing value-pointer path for caller-owned `Valuer` inputs.

- [x] Add regression tests that overwrite the same property 10,000 times with each supported primitive, read back the final value, and require internal-context retained count to return to its baseline. Run this test against the original setter path and confirm it exposes growth.
- [x] Add ownership tests: set a caller-created `Value`, then reuse it in another operation and release it exactly once. Include named/indexed/internal-field targets and existing internal-field bounds behavior.
- [x] Implement private tagged primitive input marshalling for string, bool, `int32`, `uint32`, `float64`, `int64`, `uint64`, and `*big.Int`. Construct the corresponding V8 `Local<Value>` inside the setter's scope; assign it without `new m_value` or `tracked_value`.
- [x] Preserve current numeric meanings: `int64`/`uint64` are BigInt, not Number. For large `*big.Int`, reuse the existing sign/word conversion in `NewValue`, including architecture-dependent word packing. Keep its word storage alive through the native call.
- [x] Use the common pointer/length string transport from Task 4 for primitive strings. The implementation combined these dependent steps instead of adding a temporary copied-string stage.
- [x] Return native exceptions through `RtnError` and Go's existing `newJSError` conversion. Explicitly test a throwing setter and a proxy that silently rejects assignment (which remains a non-error). The current native `.Check()` may terminate on failure; converting that into a returned error is an intentional correction and must be called out in the change description.
- [x] Keep existing validation for unsupported inputs and internal-field bounds. Do not broaden accepted Go types.
- [x] Compare primitive and existing-`Value` setter benchmarks and long-lived-isolate retention. Run leak checks after context close and isolate disposal.

**Acceptance:** Primitive writes create no persistent bridge temporary, caller values remain usable, numeric semantics are unchanged, and repeated overwrites do not grow the internal handle registry. Existing-`Value` writes do not regress materially.

## Task 3: Eliminate duplicate callback context lookup — finding 1

**Files:** Modify `v8go.cc`, `context.go`, and declarations as needed in `v8go.h`; extend `function_template_test.go`, `context_test.go`, and callback benchmarks.

**Interfaces:** Preserve `FunctionCallback`, `FunctionCallbackInfo`, `goFunctionCallback`, and the integer context registry. Remove the exported `goContext` trampoline only after verifying it has no remaining callers.

- [x] Add tests for one function template used from multiple contexts of the same isolate, nested Go→JS→Go calls, and promise callbacks. Assert `info.Context()` identifies the executing context, not the template's original context.
- [x] Retain the existing tests proving callback arguments can outlive callback return. Do not introduce implicit argument release.
- [x] Reserve embedder slot 2 for `m_ctx*`; retain slot 1's integer Go reference and leave slot 0 alone. Allocate/initialize `m_ctx` during `NewContext`, store its native address with `SetAlignedPointerInEmbedderData`, and use the matching tag-aware getter supported by the pinned headers.
- [x] Replace `goContext(ctx_ref)` in `FunctionTemplateCallback` with the native embedder-data lookup. Keep the single Go registry lookup in `goFunctionCallback`.
- [x] Audit disposal so the native context pointer cannot be observed after the context is closed under the existing lifecycle contract. Test sequential creation/disposal and concurrent callbacks on distinct isolates; do not imply concurrent close of an active context is newly supported.
- [x] Run callback/context/promise tests, race checks, and callback benchmarks at all argument counts.

**Acceptance:** Exactly one C→Go callback dispatch per JS callback rather than the current lookup-plus-dispatch pair; correct context selection and reentrancy; unchanged argument lifetime and no new races.

## Task 4: Reduce string copies — finding 5

**Files:** Modify string-input paths in `value.go`, `object.go`, `json.go`, `context.go`, `isolate.go`, `template.go`, corresponding declarations in `v8go.h`, and implementations in `v8go.cc`; extend their existing tests and string benchmarks.

**Interfaces:** Use private pointer-plus-length arguments for synchronous string input. Keep public Go strings and existing output ownership. `RtnString` remains the explicit-length owned-output representation where already used.

- [x] Add tests for empty strings, multibyte UTF-8, invalid UTF-8, embedded NUL, and large input/output. For V8 output include unpaired UTF-16 surrogates and objects with throwing or side-effecting `toString` methods.
- [x] Document current NUL behavior before changing transport. `NewValue` already passes the full Go length; legacy keys/source/JSON paths use NUL termination. Preserve their first-NUL truncation in this performance series using the prefix length, and track full-string semantics as a separate compatibility correction. Newly added APIs may explicitly support full-length strings.
- [x] Borrow Go string bytes with `unsafe.StringData`, passing pointer and length to the native call. Handle empty input with a valid empty C++ string pointer when V8 requires one. Keep the Go input live through the call with `runtime.KeepAlive`. Apply V8's maximum-length checks before converting Go lengths to native integer lengths.

```go
data := unsafe.StringData(s)
// Native entry point consumes data synchronously using an explicit length.
// It neither writes to the bytes nor retains their address.
runtime.KeepAlive(s) // place after the actual native call
```

- [x] Replace `CopyString(String::Utf8Value&)`'s intermediate `std::string` with one owned allocation and a direct copy of the known byte length. Preserve each caller's empty-string/null convention.
- [x] Prototype output encoding directly into the owned native return buffer using the pinned V8 `String::Utf8Length` and `WriteUtf8` APIs. Perform JS-to-string conversion exactly once, match the existing invalid-surrogate replacement behavior, and retain the existing `Value.String()` failure semantics. Keep Go's final ownership copy.
- [x] Benchmark direct output encoding against `String::Utf8Value`: a length pass plus encoding is not automatically faster. Retain only the approach supported by measurements for short and large strings.
- [x] Run affected tests, leak checks, and a separate `GOEXPERIMENT=cgocheck2 go test ./...` run where supported by the selected toolchain.

**Acceptance:** Removed input malloc/copy/free on targeted paths; no native retention of Go pointers; compatible string/error behavior; output simplification supported by benchmarks across sizes.

## Task 5: Fuse method lookup and invocation — finding 3

**Files:** Modify `object.go`, `v8go.h`, `v8go.cc`; extend `object_test.go` and method-call benchmarks.

**Consumes:** Task 4's pointer/length key transport and Task 1's direct handle construction.

**Produces:** Private `ObjectMethodCall` bridge returning `RtnValue`; unchanged Go `MethodCall(methodName string, args ...Valuer) (*Value, error)`.

- [x] Add tests for own and inherited methods, a getter returning a function, a proxy, a throwing getter, a throwing method, and a missing/non-function property. Assert the method receives the original object as `this` and the getter executes exactly once.
- [x] Add a retention regression: call a simple method repeatedly, release each result, and assert the context's retained count stays at baseline. Verify the existing implementation fails this check because it retains the intermediate function wrapper.
- [x] Marshal arguments using the existing function-call pattern. In one `LOCAL_OBJECT` scope, decode the name, get the property, test callability, build local arguments, and call the function with the original receiver.
- [x] Preserve the current Go error category/message for a non-function property using a private result status if necessary. Return actual JS getter/call exceptions through `RtnError`. Avoid manufacturing a JS exception for what was previously a Go type error.
- [x] Create only the returned result's persistent wrapper. Never wrap the temporary method function in `m_value`.
- [x] Run object/function tests and compare fused `MethodCall` against its baseline and cached `Function.Call`. Do not cache method lookup across calls, since JavaScript may replace the property.

**Acceptance:** One functional native entry instead of get/type-check/call; no retained intermediate method handle; preserved lookup, receiver, and exception semantics.

## Task 6: Batch property writes — finding 6

**Files:** Modify `object.go`, `v8go.h`, `v8go.cc`, `README.md`; extend `object_test.go` and batch benchmarks.

**Consumes:** Existing caller-owned `Value` handles, Task 4's explicit-length key transport, and current object-setting semantics.

**Produces:** This additive public API:

```go
type Property struct {
    Key   string
    Value Valuer
}

func (o *Object) SetMany(properties []Property) error
```

- [x] Add tests for empty input, 1/8/64 properties, duplicate names, full-length NUL-containing names, caller-owned values, and a throwing setter in the middle of a batch.
- [x] Specify ordered semantics: validate all inputs before writing; then apply writes in slice order. Duplicate keys are allowed. Stop on the first JS exception; earlier writes remain. Preserve V8 Object::Set silent-rejection behavior. An empty batch is a no-op. Nil or wrong-isolate values produce a Go error before any writes.
- [x] Pack key bytes into one Go byte buffer, offsets/lengths into pointer-free numeric arrays, and native value handles into a separate array. Pass these buffers as separate native parameters; do not embed Go string/slice headers or pointers to Go buffers inside a descriptor passed to C. Keep input storage live until return and validate aggregate lengths before native casts.
- [x] Perform native preflight for value-isolate compatibility before mutation where Go wrappers lack sufficient ownership metadata. Use one locker/context/exception scope for the ordered write loop. Return the first failure through the established error bridge.
- [x] Reuse caller-owned handles directly and create no per-property persistent wrappers. This initial API accepts `Valuer`, so it does not add an independent bulk primitive-conversion system.
- [x] Add a README example creating/reusing values, calling `SetMany`, releasing values afterward, and describing partial completion on a setter exception.
- [x] Compare repeated `Set` calls against `SetMany` for 1/8/64 entries with pre-created values. Report bytes allocated for packing and native crossings separately. Keep single-property performance visible.
- [x] Evaluate typed property reads only as a follow-on measurement: compare `Get` + `Number` + `Release` in the harness. Do not add a broad typed API family without evidence that these reads are a significant remaining cost.

**Acceptance:** One native operation for a nonempty valid batch; deterministic ordered/error behavior; unchanged caller ownership; a demonstrated benefit for multi-property workloads without hiding marshalling costs.

## Final validation and delivery

- [x] Run formatting checks for changed Go files and the repository's Chromium-style C++ formatting command. Verify the diff contains only intended formatting changes.
- [x] Run the following after implementation in the configured native environment:

```sh
go vet ./...
go test -count=1 ./...
go test -race -count=1 ./...
go test -count=1 -tags leakcheck .
go test -run '^$' -bench '^BenchmarkBoundary/' -benchmem -benchtime=200ms -count=10 .
```

- [ ] Run Linux amd64 and native arm64 validation through the repository's native build/test workflow. Keep architecture results separate. Sanitizer builds are for correctness, not performance comparisons.
- [x] Compare repeated before/after samples with `benchstat` if available. Investigate repeatable regressions above 5% on unaffected paths; treat that threshold as a review trigger, not proof of statistical significance. Do not set a promised speedup before the baseline exists.
- [x] Use native allocation profiling or process-memory sampling for representative long-lived-isolate workloads. Go `-benchmem` alone cannot establish reduced C++ memory use, and leak checks at process exit cannot prove absence of lifetime-long handle retention.
- [x] Deliver per-task benchmark comparisons, retention results, test results, compatibility corrections, and any environment-limited checks. Document whether each change was retained based on memory correctness, measured throughput, or both.

**Completion criteria:** All six findings have an implemented and validated outcome; ownership regressions are covered; no unsupported performance claim is made; new API semantics are documented; each change is independently reviewable.

## Execution decisions (2026-09-08)

- Ruling: work in the existing feature checkout after sandbox denial of worktree creation. At the end of local implementation, no branch, commit, push, or CI dispatch had been performed.
- Ruling: preserve silent assignment rejection. The pinned V8 public Object::Set API returns only Just(true) or Empty; proxy false results are not observable through its return value. Tests and SetMany documentation reflect this contract.
- Ruling: validate with ten 200 ms samples per case on this host, keeping the same harness on baseline and optimized binaries. The successful-path testing.B.Helper overhead was removed before final measurements; earlier exploratory timing files are not used for final claims.
- Ruling: baseline is HEAD plus initialized value IDs, test-only internal retained-count access, and the corrected benchmark harness. Scratch baseline source and native build artifacts stay under ignored .build/performance.
- Ruling: snapshot batch inputs and method arguments into local V8 handles before any setter/getter can reenter Go. Regression tests reproduced argument replacement after a Go callback released a bridge wrapper.
- Ruling: retain existing caller ownership; accepting struct-valued Valuer implementations is covered by a regression test after review caught a nil-check panic.
- Native arm64 execution requires the existing native CI runner and remains an external validation step; the local host is amd64.
- Ruling: keep dedicated native paths for caller-owned values in indexed/internal setters. Measurements found approximately 15% overhead when these inputs used the general primitive marshaller; the direct paths removed most of that overhead.

## Delivery status

All six implementation tasks are complete and reviewed. Full tests, vet, race,
leak sanitizer, and strict cgo-pointer checks pass on the local amd64 host.
Final ten-sample benchmarks and longer regression rechecks are recorded in
[the implementation report](../../performance/2026-09-08-go-v8-boundary.md).
Native arm64 CI is the remaining platform validation item.
