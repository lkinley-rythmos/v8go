// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// This standalone consumer must execute on the initial process thread. On
// musl, caching its initial stack mapping as a reserved bound makes V8 abort
// in IsOnCentralStack when recursion later grows past the cached mapping.
package main

import (
	"errors"
	"fmt"
	"runtime"
	"strings"

	v8 "github.com/lkinley-rythmos/v8go"
)

func init() { runtime.LockOSThread() }

func main() {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	// Prime the reserved-bound cache at shallow native depth before recursion.
	value, err := ctx.RunScript(`try { throw new Error("warm"); } catch (_) {} ; 42`, "warm.js")
	if err != nil {
		panic(err)
	}
	value.Release()
	for range 100 {
		value, err = ctx.RunScript(`(function recurse() { return recurse(); })()`, "recursion.js")
		if value != nil {
			value.Release()
		}
		var jsErr *v8.JSError
		if !errors.As(err, &jsErr) || !strings.HasPrefix(jsErr.Message, "RangeError:") {
			panic(fmt.Sprintf("expected RangeError, got %v", err))
		}
		value, err = ctx.RunScript("6 * 7", "recover.js")
		if err != nil {
			panic(err)
		}
		if value.Int32() != 42 {
			panic("recovery mismatch")
		}
		value.Release()
	}
	fmt.Println("initial-thread recursion and recovery: 100/100")
}
