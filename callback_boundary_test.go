// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go_test

import (
	"fmt"
	"testing"

	v8 "github.com/lkinley-rythmos/v8go"
)

func TestCallbackBoundaryContexts(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	var expected *v8.Context
	calls := 0
	global := v8.NewObjectTemplate(iso)
	callback := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		calls++
		if info.Context() != expected {
			t.Error("callback selected the wrong context")
		}
		return nil
	})
	if err := global.Set("callback", callback); err != nil {
		t.Fatal(err)
	}
	first := v8.NewContext(iso, global)
	defer first.Close()
	// Reuse one template while another context remains alive, including after
	// repeatedly closing contexts whose native storage may be reused.
	for i := 0; i < 20; i++ {
		second := v8.NewContext(iso, global)
		for _, ctx := range []*v8.Context{first, second, first} {
			expected = ctx
			val, err := ctx.RunScript("callback()", "")
			if err != nil {
				t.Fatal(err)
			}
			val.Release()
		}
		second.Close()
	}
	if calls != 60 {
		t.Fatalf("got %d callbacks", calls)
	}
}

func TestCallbackBoundaryNestedAndRetained(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	global := v8.NewObjectTemplate(iso)
	var ctx *v8.Context
	var retained *v8.FunctionCallbackInfo
	calls := 0
	callback := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		calls++
		if info.Context() != ctx {
			t.Error("nested callback selected the wrong context")
		}
		if info.Args()[0].Int32() == 0 {
			defer info.Release()
			val, err := info.Context().RunScript("callback(42)", "")
			if err != nil {
				t.Error(err)
			} else {
				val.Release()
			}
		} else {
			retained = info
		}
		return nil
	})
	if err := global.Set("callback", callback); err != nil {
		t.Fatal(err)
	}
	ctx = v8.NewContext(iso, global)
	defer ctx.Close()
	baseline := ctx.RetainedValueCount()
	val, err := ctx.RunScript("callback(0)", "")
	if err != nil {
		t.Fatal(err)
	}
	val.Release()
	if calls != 2 || retained == nil {
		t.Fatal("nested callback did not execute")
	}
	if got := retained.Args()[0].Int32(); got != 42 {
		t.Fatalf("retained argument = %d", got)
	}
	retained.Release()
	if got := ctx.RetainedValueCount(); got != baseline {
		t.Fatalf("retained handles: got %d, want %d", got, baseline)
	}
}

func TestCallbackBoundaryDistinctIsolates(t *testing.T) {
	for worker := 0; worker < 4; worker++ {
		t.Run(fmt.Sprint(worker), func(t *testing.T) {
			t.Parallel()
			iso := v8.NewIsolate()
			defer iso.Dispose()
			ctx := v8.NewContext(iso)
			defer ctx.Close()
			calls := 0
			tmpl := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
				defer info.Release()
				calls++
				if info.Context() != ctx {
					t.Error("callback crossed isolates")
				}
				return nil
			})
			fn := tmpl.GetFunction(ctx)
			defer fn.Release()
			for i := 0; i < 100; i++ {
				val, err := fn.Call(v8.Undefined(iso))
				if err != nil {
					t.Fatal(err)
				}
				val.Release()
			}
			if calls != 100 {
				t.Fatalf("got %d callbacks", calls)
			}
		})
	}
}

func TestCallbackBoundaryPromiseContext(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	for i := 0; i < 2; i++ {
		ctx := v8.NewContext(iso)
		resolver, err := v8.NewPromiseResolver(ctx)
		if err != nil {
			t.Fatal(err)
		}
		called := false
		promise := resolver.GetPromise()
		chained := promise.Then(func(info *v8.FunctionCallbackInfo) *v8.Value {
			defer info.Release()
			called = true
			if info.Context() != ctx {
				t.Error("promise callback selected the wrong context")
			}
			return nil
		})
		resolver.Resolve(v8.Undefined(iso))
		ctx.PerformMicrotaskCheckpoint()
		if !called {
			t.Error("promise callback did not execute")
		}
		chained.Release()
		promise.Release()
		resolver.Release()
		ctx.Close()
	}
}
