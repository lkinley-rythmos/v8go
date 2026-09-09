// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go_test

import (
	"errors"
	"fmt"
	v8 "github.com/lkinley-rythmos/v8go"
	"testing"
)

func TestObjectSetMany(t *testing.T) {
	for _, n := range []int{0, 1, 8, 64} {
		t.Run(fmt.Sprint(n), func(t *testing.T) {
			iso := v8.NewIsolate()
			defer iso.Dispose()
			ctx := v8.NewContext(iso)
			defer ctx.Close()
			obj := ctx.Global()
			val, err := v8.NewValue(iso, int32(42))
			if err != nil {
				t.Fatal(err)
			}
			defer val.Release()
			props := make([]v8.Property, n)
			for i := range props {
				props[i] = v8.Property{Key: fmt.Sprint("key", i), Value: val}
			}
			before := ctx.RetainedValueCount()
			internalBefore := iso.InternalRetainedValueCount()
			if err := obj.SetMany(props); err != nil {
				t.Fatal(err)
			}
			if ctx.RetainedValueCount() != before || iso.InternalRetainedValueCount() != internalBefore {
				t.Fatal("batch retained handles")
			}
			for _, p := range props {
				got, err := obj.Get(p.Key)
				if err != nil {
					t.Fatal(err)
				}
				if got.Int32() != 42 {
					t.Fatal("wrong property value")
				}
				got.Release()
			}
			if val.Int32() != 42 {
				t.Fatal("caller value released")
			}
		})
	}
}

func TestObjectSetManyOrderAndErrors(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	one, _ := v8.NewValue(iso, int32(1))
	defer one.Release()
	two, _ := v8.NewValue(iso, int32(2))
	defer two.Release()
	raw, err := ctx.RunScript("({set bad(v){throw new Error('stop')}})", "batch.js")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := raw.AsObject()
	if err != nil {
		t.Fatal(err)
	}
	err = obj.SetMany([]v8.Property{{"a", one}, {"a", two}, {"bad", one}, {"after", one}})
	var js *v8.JSError
	if !errors.As(err, &js) {
		t.Fatalf("want JS error, got %v", err)
	}
	got, err := obj.Get("a")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Release()
	if got.Int32() != 2 || obj.Has("after") {
		t.Fatal("batch order or partial completion violated")
	}
	if err := obj.SetMany([]v8.Property{{"nul\x00key", one}}); err != nil {
		t.Fatal(err)
	}
	// Existing Get deliberately truncates NUL keys; use JS access for the new API.
	if err := ctx.Global().Set("batchObject", obj); err != nil {
		t.Fatal(err)
	}
	gotNUL, err := ctx.RunScript("batchObject['nul\\0key']", "")
	if err != nil {
		t.Fatal(err)
	}
	defer gotNUL.Release()
	if gotNUL.Int32() != 1 || obj.Has("nul") {
		t.Fatal("batch key was truncated")
	}
}

func TestObjectSetManyPreflight(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	obj := ctx.Global()
	other := v8.NewIsolate()
	defer other.Dispose()
	valid, _ := v8.NewValue(iso, int32(1))
	defer valid.Release()
	foreign, _ := v8.NewValue(other, int32(2))
	defer foreign.Release()
	var nilValue *v8.Value
	var nilObject *v8.Object
	for _, invalid := range []v8.Valuer{nil, nilValue, nilObject, foreign} {
		err := obj.SetMany([]v8.Property{{"first", valid}, {"invalid", invalid}})
		if err == nil {
			t.Fatal("expected preflight error")
		}
		if obj.Has("first") {
			t.Fatal("mutation occurred before validation")
		}
	}
}

func TestObjectSetManyReentrantRelease(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	later, err := v8.NewValue(iso, int32(17))
	if err != nil {
		t.Fatal(err)
	}
	var replacement *v8.Value
	global := v8.NewObjectTemplate(iso)
	callback := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		later.Release()
		replacement, err = v8.NewValue(iso, int32(99))
		if err != nil {
			t.Fatal(err)
		}
		return nil
	})
	if err := global.Set("releaseLater", callback); err != nil {
		t.Fatal(err)
	}
	ctx := v8.NewContext(iso, global)
	defer ctx.Close()
	raw, err := ctx.RunScript("({set first(v){releaseLater()}})", "")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := raw.AsObject()
	if err != nil {
		t.Fatal(err)
	}
	if err := obj.SetMany([]v8.Property{{"first", v8.Undefined(iso)}, {"later", later}}); err != nil {
		t.Fatal(err)
	}
	if replacement != nil {
		defer replacement.Release()
	}
	got, err := obj.Get("later")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Release()
	if got.Int32() != 17 {
		t.Fatalf("batch argument changed during setter callback: got %d", got.Int32())
	}
}
