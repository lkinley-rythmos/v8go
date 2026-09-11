// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build linux && cgo && (amd64 || arm64)

package nativestack_test

import (
	"errors"
	"runtime"
	"strings"
	"sync"
	"testing"

	v8 "github.com/lkinley-rythmos/v8go"
	"github.com/lkinley-rythmos/v8go/internal/teststack"
)

func init() {
	// Keep the test executable's main goroutine on the initial process thread
	// while testing runs its worker goroutines. musl reports only the initial
	// mapping for that growable stack; this gate targets cgo-created pthreads
	// whose fixed default capacity is controlled by the SDK linker setting.
	runtime.LockOSThread()
}

// Removing the musl SDK stack setting must fail the capacity gate before any
// unbounded JavaScript recursion can overflow the native thread's stack.
func TestNativeThreadStackRecursion(t *testing.T) {
	const workers = 8
	const minimumHeadroom = 4 << 20
	type stack struct {
		thread             uintptr
		capacity, headroom uint64
		err                error
	}
	ready := make(chan stack, workers)
	start := make(chan struct{})
	var wg sync.WaitGroup
	safe := true
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			runtime.LockOSThread()
			defer runtime.UnlockOSThread()
			id, capacity, headroom, err := teststack.Current()
			ready <- stack{id, capacity, headroom, err}
			<-start
			if safe {
				checkRecursion(t)
			}
		}()
	}
	threads := make(map[uintptr]bool, workers)
	for i := 0; i < workers; i++ {
		s := <-ready
		t.Logf("pthread=%x capacity=%d headroom=%d", s.thread, s.capacity, s.headroom)
		if s.err != nil || s.capacity < minimumHeadroom || s.headroom < minimumHeadroom {
			t.Errorf("unsafe native stack: capacity=%d headroom=%d need >=%d: %v", s.capacity, s.headroom, minimumHeadroom, s.err)
			safe = false
		}
		if threads[s.thread] {
			t.Errorf("pthread %x is shared by simultaneously locked goroutines", s.thread)
			safe = false
		}
		threads[s.thread] = true
	}
	close(start)
	wg.Wait()
}

func checkRecursion(t *testing.T) {
	t.Helper()
	iso := v8.NewIsolate()
	defer iso.Dispose()
	global := v8.NewObjectTemplate(iso)
	const recursion = `(function recurse() { return recurse(); })()`
	calls := 0
	callback := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		calls++
		// Recheck at the deeper native callback boundary before recursing.
		_, _, headroom, err := teststack.Current()
		if err != nil || headroom < 4<<20 {
			t.Errorf("unsafe callback stack: headroom=%d: %v", headroom, err)
			return nil
		}
		val, err := info.Context().RunScript(recursion, "callback-recursion.js")
		checkRangeError(t, val, err)
		checkRecovery(t, info.Context())
		return nil
	})
	if err := global.Set("reenter", callback); err != nil {
		t.Error(err)
		return
	}
	ctx := v8.NewContext(iso, global)
	defer ctx.Close()
	val, err := ctx.RunScript(recursion, "ordinary-recursion.js")
	checkRangeError(t, val, err)
	checkRecovery(t, ctx)
	script, err := iso.CompileUnboundScript(recursion, "unbound-recursion.js", v8.CompileOptions{})
	if err != nil {
		t.Error(err)
		return
	}
	val, err = script.Run(ctx)
	checkRangeError(t, val, err)
	checkRecovery(t, ctx)
	val, err = ctx.RunScript("reenter(); 42", "reenter.js")
	if err != nil {
		t.Errorf("callback invocation: %v", err)
	} else {
		if val.Int32() != 42 {
			t.Errorf("callback invocation returned %s", val)
		}
		val.Release()
	}
	if calls != 1 {
		t.Errorf("callback executed %d times, want 1", calls)
	}
	checkRecovery(t, ctx)
}

func checkRangeError(t *testing.T, val *v8.Value, err error) {
	t.Helper()
	if val != nil {
		val.Release()
	}
	var jsErr *v8.JSError
	if !errors.As(err, &jsErr) {
		t.Errorf("recursion returned %T (%v), want *v8.JSError", err, err)
		return
	}
	if !strings.HasPrefix(jsErr.Message, "RangeError:") {
		t.Errorf("recursion exception = %q, want RangeError", jsErr.Message)
	}
}

func checkRecovery(t *testing.T, ctx *v8.Context) {
	t.Helper()
	val, err := ctx.RunScript("6 * 7", "recovery.js")
	if err != nil {
		t.Errorf("evaluation after recursion: %v", err)
		return
	}
	defer val.Release()
	if val.Int32() != 42 {
		t.Errorf("evaluation after recursion = %s, want 42", val)
	}
}
