// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go_test

import (
	"errors"
	"math/big"
	"testing"

	v8 "github.com/lkinley-rythmos/v8go"
)

func TestBoundaryPrimitiveSetRetention(t *testing.T) {
	large, _ := new(big.Int).SetString("-123456789012345678901234567890", 10)
	for _, input := range []struct {
		name  string
		value any
		want  string
	}{
		{"int32", int32(-7), "-7"}, {"uint32", uint32(4000000000), "4000000000"},
		{"int64", int64(-9223372036854775807), "-9223372036854775807"},
		{"uint64", uint64(18446744073709551615), "18446744073709551615"},
		{"float", 1.25, "1.25"}, {"bool", true, "true"}, {"string", "hello\x00世界", "hello\x00世界"},
		{"big", large, large.String()},
	} {
		for _, target := range []string{"named", "indexed", "internal"} {
			t.Run(input.name+"/"+target, func(t *testing.T) {
				iso := v8.NewIsolate()
				defer iso.Dispose()
				ctx := v8.NewContext(iso)
				defer ctx.Close()
				tmpl := v8.NewObjectTemplate(iso)
				tmpl.SetInternalFieldCount(1)
				obj, err := tmpl.NewInstance(ctx)
				if err != nil {
					t.Fatal(err)
				}
				before := iso.InternalRetainedValueCount()
				for i := 0; i < 10000; i++ {
					switch target {
					case "named":
						err = obj.Set("key", input.value)
					case "indexed":
						err = obj.SetIdx(0, input.value)
					case "internal":
						err = obj.SetInternalField(0, input.value)
					}
					if err != nil {
						t.Fatal(err)
					}
				}
				var got *v8.Value
				switch target {
				case "named":
					got, err = obj.Get("key")
				case "indexed":
					got, err = obj.GetIdx(0)
				case "internal":
					got = obj.GetInternalField(0)
				}
				if err != nil {
					t.Fatal(err)
				}
				if got.String() != input.want {
					t.Fatalf("got %q, want %q", got.String(), input.want)
				}
				got.Release()
				if delta := iso.InternalRetainedValueCount() - before; delta != 0 {
					t.Fatalf("retained %d primitive temporaries", delta)
				}
			})
		}
	}
}

func TestBoundaryMethodRetention(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	val, err := ctx.RunScript("({n: 3, m(){return this.n}})", "method.js")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := val.AsObject()
	if err != nil {
		t.Fatal(err)
	}
	before := ctx.RetainedValueCount()
	for i := 0; i < 1000; i++ {
		out, err := obj.MethodCall("m")
		if err != nil {
			t.Fatal(err)
		}
		if out.Int32() != 3 {
			t.Fatal("wrong receiver")
		}
		out.Release()
	}
	if delta := ctx.RetainedValueCount() - before; delta != 0 {
		t.Fatalf("retained %d intermediate methods", delta)
	}
}

func TestBoundarySetterExceptions(t *testing.T) {
	for _, script := range []string{"({set x(v){throw new Error('setter')}})"} {
		t.Run(script, func(t *testing.T) {
			iso := v8.NewIsolate()
			defer iso.Dispose()
			ctx := v8.NewContext(iso)
			defer ctx.Close()
			val, err := ctx.RunScript(script, "setter.js")
			if err != nil {
				t.Fatal(err)
			}
			obj, err := val.AsObject()
			if err != nil {
				t.Fatal(err)
			}
			err = obj.Set("x", int32(1))
			if err == nil {
				t.Fatal("expected assignment error")
			}
			var jsErr *v8.JSError
			if !errors.As(err, &jsErr) {
				t.Fatalf("expected JSError, got %T", err)
			}
		})
	}
}

func TestBoundaryMethodSemantics(t *testing.T) {
	for _, test := range []struct {
		name, script, key, want string
		jsError, goError        bool
	}{
		{"inherited", "Object.assign(Object.create({m(){return this.n}}), {n:7})", "m", "7", false, false},
		{"getter", "({n:0,get m(){this.n++;return function(){return this.n}}})", "m", "1", false, false},
		{"proxy", "new Proxy({n:8}, {get(o,k){if(k==='m')return function(){return this.n};return o[k]}})", "m", "8", false, false},
		{"throw getter", "({get m(){throw new Error('getter')}})", "m", "", true, false},
		{"throw method", "({m(){throw new Error('method')}})", "m", "", true, false},
		{"missing", "({})", "m", "", false, true},
		{"not function", "({m:1})", "m", "", false, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			iso := v8.NewIsolate()
			defer iso.Dispose()
			ctx := v8.NewContext(iso)
			defer ctx.Close()
			value, err := ctx.RunScript(test.script, "method.js")
			if err != nil {
				t.Fatal(err)
			}
			obj, err := value.AsObject()
			if err != nil {
				t.Fatal(err)
			}
			got, err := obj.MethodCall(test.key)
			if test.jsError || test.goError {
				if err == nil {
					t.Fatal("expected error")
				}
				var js *v8.JSError
				if errors.As(err, &js) != test.jsError {
					t.Fatalf("unexpected error type %T", err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			defer got.Release()
			if got.String() != test.want {
				t.Fatalf("got %q, want %q", got.String(), test.want)
			}
		})
	}
}

func TestBoundarySetterSilentRejection(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	val, err := ctx.RunScript("new Proxy({x:1}, {set(){return false}})", "proxy.js")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := val.AsObject()
	if err != nil {
		t.Fatal(err)
	}
	if err := obj.Set("x", int32(2)); err != nil {
		t.Fatal(err)
	}
	got, err := obj.Get("x")
	if err != nil {
		t.Fatal(err)
	}
	defer got.Release()
	if got.Int32() != 1 {
		t.Fatal("rejected property was changed")
	}
}

func TestBoundaryStructValuer(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	value, _ := v8.NewValue(iso, int32(17))
	defer value.Release()
	wrapped := struct{ *v8.Value }{value}
	obj := ctx.Global()
	if err := obj.Set("wrapped", wrapped); err != nil {
		t.Fatal(err)
	}
	if err := obj.SetMany([]v8.Property{{"batch", wrapped}}); err != nil {
		t.Fatal(err)
	}
	fn, err := ctx.RunScript("globalThis.identity = function(x){return x};", "")
	if err != nil {
		t.Fatal(err)
	}
	fn.Release()
	got, err := obj.MethodCall("identity", wrapped)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Release()
	if got.Int32() != 17 {
		t.Fatal("struct Valuer changed")
	}
}

func TestBoundaryMethodGetterReentrantRelease(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	arg, err := v8.NewValue(iso, int32(17))
	if err != nil {
		t.Fatal(err)
	}
	var replacement *v8.Value
	global := v8.NewObjectTemplate(iso)
	callback := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		defer info.Release()
		arg.Release()
		replacement, err = v8.NewValue(iso, int32(99))
		if err != nil {
			t.Fatal(err)
		}
		return nil
	})
	if err := global.Set("releaseArgument", callback); err != nil {
		t.Fatal(err)
	}
	ctx := v8.NewContext(iso, global)
	defer ctx.Close()
	raw, err := ctx.RunScript("({get m(){releaseArgument();return function(x){return x}}})", "")
	if err != nil {
		t.Fatal(err)
	}
	obj, err := raw.AsObject()
	if err != nil {
		t.Fatal(err)
	}
	got, err := obj.MethodCall("m", arg)
	if err != nil {
		t.Fatal(err)
	}
	defer got.Release()
	if replacement != nil {
		defer replacement.Release()
	}
	if got.Int32() != 17 {
		t.Fatalf("argument changed in getter: %d", got.Int32())
	}
}
