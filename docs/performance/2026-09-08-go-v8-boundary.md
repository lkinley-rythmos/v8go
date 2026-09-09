# Go–V8 boundary performance implementation

Implemented the six changes from the implementation plan while retaining the existing public Go signatures. `Property` and `Object.SetMany` are additive APIs.

## Implementation

| Finding | Change | Ownership and behavior |
| --- | --- | --- |
| Duplicate callback lookup | Store the native context pointer in embedder slot 2; remove the `goContext` trampoline | Keep slot 1 and the Go context registry; callback arguments retain their existing explicit lifetime |
| Primitive setter temporaries | Construct primitive V8 locals inside a single setter bridge call | No temporary persistent wrapper or registry entry; caller-provided values are not released |
| Method lookup and call | Fuse lookup, function check, and invocation in `ObjectMethodCall` | Preserve dynamic getters/proxies, receiver binding, and Go versus JavaScript error types; retain only the returned result |
| Temporary persistent copies | Initialize persistent handles directly with `Reset(iso, local)` | Preserve the registry and explicit release mechanism |
| String copying | Borrow Go input bytes synchronously with explicit lengths; encode output directly into the native return buffer | Keep inputs alive through the call; V8 does not retain Go memory; keep Go's final output ownership copy |
| Repeated property writes | Add ordered `SetMany([]Property)` | Validate values before writes, snapshot local handles before user code, and leave earlier writes intact on an exception |

`m_ctx::nextValId` now starts at zero. A test-only Go accessor exposes the isolate's internal retained count, since the existing public context counter cannot see primitive values created in that internal context.

Legacy named-property, script, JSON-input, and template-name APIs continue truncating at their first NUL byte. `NewValue` preserves full string contents, as before; new `SetMany` keys support embedded NUL bytes.

Throwing setters now return `JSError` instead of invoking the former fatal native `.Check()`. Silent rejection remains a non-error: the pinned V8 `Object::Set` API explicitly returns only `Just(true)` or an empty result, and uses non-throwing assignment internally. Batch writes follow the same semantics.

## Correctness evidence

- Before the setter fix, each 10,000-write test retained 10,000 internal handles. Afterward, all tested primitive types and named/indexed/internal-field targets retain zero temporary handles.
- Before the method fix, 1,000 method calls retained 1,000 intermediate function handles after releasing results. Afterward, the count stays at its baseline.
- Batch preflight rejects nil and foreign-isolate values before the first write. Tests cover duplicate keys, partial completion, full-length keys, caller ownership, and struct-valued `Valuer` implementations.
- Reentrancy tests release an input wrapper and create a replacement from a Go callback invoked by a setter or getter. Both previously observed `99` in place of the original `17`; capturing V8 locals before user code fixes that behavior.
- Callback tests cover multiple contexts, context disposal/recreation, nested Go→JS→Go calls, promises, retained callback arguments, and concurrent distinct isolates.
- String tests cover empty and large strings, UTF-8, invalid bytes, embedded NUL, unpaired surrogates, and side-effecting/throwing `toString` methods.

The complete tests, `go vet`, `go test -race`, native leak sanitizer (`-tags leakcheck`), and `GOEXPERIMENT=cgocheck2` tests pass on Linux amd64. Compilation continues to emit preexisting warnings about C++ variable-length arrays on the original function/callback paths.

## Measurement method

Baseline source is commit `3c686db1d56cc7dda04dd18120a63088946e8ccf` plus initialized value IDs, the test-only retained counter, and the corrected benchmark harness. No optimization is applied to that source. The final binary includes dedicated existing-Value setter paths added after the initial measurements exposed marshalling overhead. Both binaries use the same Go/compiler/native SDK configuration.

Host: Intel Core i7-9750H, Linux amd64, Go `go1.27.0-X:nodwarf5`, Clang 22.1.8, V8 15.2.124.21 with its matching libc++ SDK. These are local microbenchmarks, not application-level throughput guarantees or arm64 measurements.

The saved binaries run sequentially with ten 200 ms samples per case, `-test.run '^$'`, and `-test.benchmem`. Successful iterations do not call `testing.B.Helper`; early exploratory files that included that overhead are excluded from the final comparison. Setup/teardown stay outside timing. Setters and method calls recycle isolates after 4096 operations on both versions to bound the original retention problem.

Go allocation metrics exclude C++ allocations. The extra retention metrics measure native bridge registry entries. Baseline `SetMany` does not exist; compare the new batch API with scalar calls in the optimized binary using the same pre-created values and properties, including batch packing in the timed operation.

Raw results, regression failure logs, and build artifacts are kept under ignored `.build/performance/`. The final comparison uses `baseline-final.txt` and `after-complete.txt`. Short-string output experiments are separate exploratory evidence, not the final result set.

## Results

Median elapsed time per operation from ten 200 ms samples. Percentage changes
refer to elapsed time, so negative numbers are improvements. Small differences
are not claims of statistical significance.

| Workload | Baseline | Final | Time change |
| --- | ---: | ---: | ---: |
| Cached function call, no arguments | 1.458 µs | 1.454 µs | -0.3% |
| 64 Go callbacks, no arguments | 70.71 µs | 65.32 µs | -7.6% |
| Named `int32` setter | 2.069 µs | 1.022 µs | -50.6% |
| Named string setter | 2.332 µs | 1.004 µs | -56.9% |
| Indexed setter, existing Value | 738.6 ns | 747.5 ns | +1.2% |
| Internal-field setter, existing Value | 657.7 ns | 676.6 ns | +2.9% |
| Dynamic method call | 3.838 µs | 1.773 µs | -53.8% |
| Cached method function call | 1.515 µs | 1.519 µs | +0.2% |
| String input, 16 ASCII bytes | 1.311 µs | 1.098 µs | -16.3% |
| String input, 64 KiB ASCII | 10.28 µs | 9.27 µs | -9.8% |
| String output, 64 KiB ASCII | 12.94 µs | 11.28 µs | -12.8% |
| String output, 64 KiB UTF-8 | 76.30 µs | 54.46 µs | -28.6% |
| Get + Number + Release | 2.368 µs | 2.156 µs | -9.0% |

Across the measured primitive setter combinations, elapsed-time improvements
range from 50.6% to 58.7%. Their Go allocations drop from 16 B / one allocation
per write to zero; native temporary handle retention drops from one per write
to zero. Returned values from normal reads and calls still require ownership
and release; this work does not remove those persistent handles.

Batch comparisons use the final implementation on both sides:

| Properties | Repeated Set | SetMany | Relative result |
| --- | ---: | ---: | --- |
| 1 | 1.037 µs | 1.348 µs | SetMany takes 30% more time |
| 8 | 8.181 µs | 3.861 µs | SetMany is 2.12× faster |
| 64 | 68.16 µs | 24.22 µs | SetMany is 2.81× faster |

Batch marshalling adds three Go allocations per call for these nonempty-key
cases. Use scalar Set for a single property; the batch benefit appears when
crossing overhead can be spread over multiple writes.

### Regression investigation

The first shared setter implementation added approximately 15% to existing-Value
indexed/internal writes. Dedicated direct-handle paths reduced that overhead to
1.2% and 2.9% in the final full run.

Two full-run medians exceeded the 5% investigation threshold: a 16-argument
function call (+7.8%) and 1 KiB ASCII string output (+5.2%). Ten longer, alternating
one-second baseline/final samples did not reproduce either regression:

| Rechecked workload | Baseline median | Final median | Time change |
| --- | ---: | ---: | ---: |
| Function call, 16 arguments | 1.974 µs | 1.945 µs | -1.5% |
| String output, 1 KiB ASCII | 1.014 µs | 924.2 ns | -8.8% |

The recheck also measured 16-argument callbacks, improving 7.8%. Raw samples are
in `recheck-before.txt` and `recheck-after.txt`; retain both the original and
recheck results rather than interpreting one favorable sample as a guarantee.

A separate 100,000-write run sampled peak process RSS using Python's
`resource.getrusage`: 28,172 KiB baseline and 26,236 KiB final. This is a single
process-wide diagnostic with isolate recycling, not a native-allocation profile
or an application memory-saving guarantee. The retained-handle regression tests
provide the direct evidence that the lifetime-long temporary roots were removed.

## Validation limits

Native arm64 validation remains for the repository's existing native CI runner. At the time of these local measurements, no remote workflow had been dispatched. The sandbox denied worktree creation, so implementation and measurements used the original feature checkout. Timings measure the combined implementation; they do not isolate the contribution of every individual change.
