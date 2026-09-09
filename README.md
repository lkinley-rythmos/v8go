# Execute JavaScript from Go

A fork of [rogchap/v8go](https://github.com/rogchap/v8go), preparing
**v0.10.0-rc.1** with **V8 15.2.124.21**.

[![CI](https://github.com/lkinley-rythmos/v8go/actions/workflows/test.yml/badge.svg)](https://github.com/lkinley-rythmos/v8go/actions/workflows/test.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/lkinley-rythmos/v8go.svg)](https://pkg.go.dev/github.com/lkinley-rythmos/v8go)

This candidate supports **Linux amd64 and arm64 (glibc)**. Install the matching
native release package and source its build environment before using the Go
module. See [installation and release instructions](RELEASING.md).
The RC is being prepared; release-download commands work after publication.

<img src="gopher.jpg" width="200px" alt="V8 Gopher based on original artwork from the amazing Renee French" />

## Usage

```go
import v8 "github.com/lkinley-rythmos/v8go"
```

### Running a script

```go
ctx := v8.NewContext() // creates a new V8 context with a new Isolate aka VM
ctx.RunScript("const add = (a, b) => a + b", "math.js") // executes a script on the global context
ctx.RunScript("const result = add(3, 4)", "main.js") // any functions previously added to the context can be called
val, _ := ctx.RunScript("result", "value.js") // return a value in JavaScript back to Go
fmt.Printf("addition result: %s", val)
```

### One VM, many contexts

```go
iso := v8.NewIsolate() // creates a new JavaScript VM
ctx1 := v8.NewContext(iso) // new context within the VM
ctx1.RunScript("const multiply = (a, b) => a * b", "math.js")

ctx2 := v8.NewContext(iso) // another context on the same VM
if _, err := ctx2.RunScript("multiply(3, 4)", "main.js"); err != nil {
  // this will error as multiply is not defined in this context
}
```

### JavaScript function with Go callback

```go
iso := v8.NewIsolate() // create a new VM
// a template that represents a JS function
printfn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
    fmt.Printf("%v", info.Args()) // when the JS function is called this Go callback will execute
    return nil // you can return a value back to the JS caller if required
})
global := v8.NewObjectTemplate(iso) // a template that represents a JS Object
global.Set("print", printfn) // sets the "print" property of the Object to our function
ctx := v8.NewContext(iso, global) // new Context with the global Object set to our object template
ctx.RunScript("print('foo')", "print.js") // will execute the Go callback with a single argunent 'foo'
```

### Update a JavaScript object from Go

```go
ctx := v8.NewContext() // new context with a default VM
obj := ctx.Global() // get the global object from the context
obj.Set("version", "v1.0.0") // set the property "version" on the object
val, _ := ctx.RunScript("version", "version.js") // global object will have the property set within the JS VM
fmt.Printf("version: %s", val)

if obj.Has("version") { // check if a property exists on the object
    obj.Delete("version") // remove the property from the object
}
```

### Assign several properties in one call

`SetMany` applies an ordered slice of properties using one Go-to-V8 call. Values
remain owned by the caller and can be reused in subsequent operations:

```go
iso := v8.NewIsolate()
defer iso.Dispose()
ctx := v8.NewContext(iso)
defer ctx.Close()
obj := ctx.Global()
defer obj.Release()
version, err := v8.NewValue(iso, "v1.0.0")
if err != nil {
    panic(err)
}
defer version.Release()
enabled, err := v8.NewValue(iso, true)
if err != nil {
    panic(err)
}
defer enabled.Release()
if err := obj.SetMany([]v8.Property{
    {Key: "version", Value: version},
    {Key: "enabled", Value: enabled},
}); err != nil {
    panic(err)
}
```

All values must be non-nil and belong to the object's isolate; these checks
precede writes. Duplicate keys are applied in order. If a JavaScript setter
throws, earlier writes remain and later writes are skipped. As with `Set`,
silently rejected assignments are not errors. `SetMany` accepts full-length keys,
including embedded NUL bytes; legacy named-property methods retain their existing
first-NUL truncation behavior.

### JavaScript errors

```go
val, err := ctx.RunScript(src, filename)
if err != nil {
  e := err.(*v8.JSError) // JavaScript errors will be returned as the JSError struct
  fmt.Println(e.Message) // the message of the exception thrown
  fmt.Println(e.Location) // the filename, line number and the column where the error occured
  fmt.Println(e.StackTrace) // the full stack trace of the error, if available

  fmt.Printf("javascript error: %v", e) // will format the standard error message
  fmt.Printf("javascript stack trace: %+v", e) // will format the full error stack trace
}
```

### Pre-compile context-independent scripts to speed-up execution times

For scripts that are large or are repeatedly run in different contexts,
it is beneficial to compile the script once and used the cached data from that
compilation to avoid recompiling every time you want to run it.

```go
source := "const multiply = (a, b) => a * b"
iso1 := v8.NewIsolate() // creates a new JavaScript VM
ctx1 := v8.NewContext(iso1) // new context within the VM
script1, _ := iso1.CompileUnboundScript(source, "math.js", v8.CompileOptions{}) // compile script to get cached data
val, _ := script1.Run(ctx1)

cachedData := script1.CreateCodeCache()

iso2 := v8.NewIsolate() // create a new JavaScript VM
ctx2 := v8.NewContext(iso2) // new context within the VM

script2, _ := iso2.CompileUnboundScript(source, "math.js", v8.CompileOptions{CachedData: cachedData}) // compile script in new isolate with cached data
val, _ = script2.Run(ctx2)
```

### Terminate long running scripts

```go
vals := make(chan *v8.Value, 1)
errs := make(chan error, 1)

go func() {
    val, err := ctx.RunScript(script, "forever.js") // exec a long running script
    if err != nil {
        errs <- err
        return
    }
    vals <- val
}()

select {
case val := <- vals:
    // success
case err := <- errs:
    // javascript error
case <- time.After(200 * time.Milliseconds):
    vm := ctx.Isolate() // get the Isolate from the context
    vm.TerminateExecution() // terminate the execution
    err := <- errs // will get a termination error back from the running script
}
```

### CPU Profiler

```go
func createProfile() {
	iso := v8.NewIsolate()
	ctx := v8.NewContext(iso)
	cpuProfiler := v8.NewCPUProfiler(iso)

	cpuProfiler.StartProfiling("my-profile")

	ctx.RunScript(profileScript, "script.js") # this script is defined in cpuprofiler_test.go
	val, _ := ctx.Global().Get("start")
	fn, _ := val.AsFunction()
	fn.Call(ctx.Global())

	cpuProfile := cpuProfiler.StopProfiling("my-profile")

	printTree("", cpuProfile.GetTopDownRoot()) # helper function to print the profile
}

func printTree(nest string, node *v8.CPUProfileNode) {
	fmt.Printf("%s%s %s:%d:%d\n", nest, node.GetFunctionName(), node.GetScriptResourceName(), node.GetLineNumber(), node.GetColumnNumber())
	count := node.GetChildrenCount()
	if count == 0 {
		return
	}
	nest = fmt.Sprintf("%s  ", nest)
	for i := 0; i < count; i++ {
		printTree(nest, node.GetChild(i))
	}
}

// Output
// (root) :0:0
//   (program) :0:0
//   start script.js:23:15
//     foo script.js:15:13
//       delay script.js:12:15
//         loop script.js:1:14
//       bar script.js:13:13
//         delay script.js:12:15
//           loop script.js:1:14
//       baz script.js:14:13
//         delay script.js:12:15
//           loop script.js:1:14
//   (garbage collector) :0:0
```

## Documentation

Go Reference & more examples: https://pkg.go.dev/github.com/lkinley-rythmos/v8go

### Support

If you would like to ask questions about this library or want to keep up-to-date with the latest changes and releases,
please join the [**#v8go**](https://gophers.slack.com/channels/v8go) channel on Gophers Slack. [Click here to join the Gophers Slack community!](https://invite.slack.golangbridge.org/)

### Windows

There used to be Windows binary support. For further information see, [PR #234](https://github.com/rogchap/v8go/pull/234).

The v8go library would welcome contributions from anyone able to get an external windows
build of the V8 library linking with v8go, using the version of V8 checked out in the
`deps/v8` git submodule, and documentation of the process involved. This process will likely
involve passing a linker flag when building v8go (e.g. using the `CGO_LDFLAGS` environment
variable.

## V8 dependency

V8 version: **15.2.124.21**.

Native dependencies are distributed as per-architecture release assets. They
include the V8 library, its Rust and C++ runtimes, matching C++ headers and
license notices. `go get` does not download these assets automatically; use
the [native installer](RELEASING.md#using-a-published-release). Building V8
from source is only necessary when changing the engine or its configuration.

## Project Goals

To provide a high quality, idiomatic, Go binding to the [V8 C++ API](https://v8.github.io/api/head/index.html).

The API should match the original API as closely as possible, but with an API that Gophers (Go enthusiasts) expect. For
example: using multiple return values to return both result and error from a function, rather than throwing an
exception.

This project also aims to keep up-to-date with the latest (stable) release of V8.

## License

v8go retains its [BSD-style license](LICENSE). Native packages include V8 and
third-party license notices in their `licenses/` directory.

## Development

### Building and upgrading V8

See [RELEASING.md](RELEASING.md) for pinned toolchains, package generation,
consumer setup, and the release workflow. Engine and wrapper releases are
versioned separately. Update headers, native dependencies, and tests together.

For a debug engine, adjust `deps/args/linux.gn` before rebuilding and generate
a separate native package. Preserve the sandbox and matching libc++ settings.

### Flushing after C/C++ standard library printing for debugging

When using the C/C++ standard library functions for printing (e.g. `printf`), then the output will be buffered by default.
This can cause some confusion, especially because the test binary (created through `go test`) does not flush the buffer
at exit (at the time of writing). When standard output is the terminal, then it will use line buffering and flush when
a new line is printed, otherwise (e.g. if the output is redirected to a pipe or file) it will be fully buffered and not even
flush at the end of a line. When the test binary is executed through `go test .` (e.g. instead of
separately compiled with `go test -c` and run with `./v8go.test`) Go may redirect standard output internally, resulting in
standard output being fully buffered.

A simple way to avoid this problem is to flush the standard output stream after printing with the `fflush(stdout);` statement.
Not relying on the flushing at exit can also help ensure the output is printed before a crash.

### Local leak checking

After installing the native package and sourcing its `env.sh`, run:

```sh
go test -count=1 -tags leakcheck .
```

This uses LLVM's LeakSanitizer. The consumer Dockerfile includes the sanitizer
runtime. Native CI runs the same check for both Linux architectures.
