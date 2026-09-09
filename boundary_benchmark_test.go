// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go_test

import (
	"fmt"
	"strings"
	"testing"

	v8 "github.com/lkinley-rythmos/v8go"
)

func boundaryCheck(b *testing.B, err error) {
	if err != nil {
		b.Helper()
		b.Fatal(err)
	}
}

// Recycling bounds the original primitive-setter and MethodCall handle leaks.
// Setup and disposal are excluded from timing; use the same chunk size before
// and after optimization. Go allocations do not measure native allocations.
func boundaryRecycled(b *testing.B, setup func(*v8.Isolate, *v8.Context) func()) {
	b.ReportAllocs()
	b.StopTimer()
	var retained, internalRetained int
	for done := 0; done < b.N; {
		iso := v8.NewIsolate()
		ctx := v8.NewContext(iso)
		op := setup(iso, ctx)
		before := ctx.RetainedValueCount()
		internalBefore := iso.InternalRetainedValueCount()
		n := b.N - done
		if n > 4096 {
			n = 4096
		}
		b.StartTimer()
		for i := 0; i < n; i++ {
			op()
		}
		b.StopTimer()
		retained += ctx.RetainedValueCount() - before
		internalRetained += iso.InternalRetainedValueCount() - internalBefore
		ctx.Close()
		iso.Dispose()
		done += n
	}
	b.ReportMetric(float64(retained)/float64(b.N), "retained/op")
	b.ReportMetric(float64(internalRetained)/float64(b.N), "internal-retained/op")
}

func boundaryContext(b *testing.B) (*v8.Isolate, *v8.Context) {
	b.Helper()
	iso := v8.NewIsolate()
	ctx := v8.NewContext(iso)
	b.Cleanup(func() { ctx.Close(); iso.Dispose() })
	b.ReportAllocs()
	return iso, ctx
}

func BenchmarkBoundary(b *testing.B) {
	for _, arity := range []int{0, 1, 4, 16} {
		b.Run(fmt.Sprintf("Call/Arity%d", arity), func(b *testing.B) {
			iso, ctx := boundaryContext(b)
			val, err := ctx.RunScript("(function() { return arguments.length; })", "bench.js")
			boundaryCheck(b, err)
			defer val.Release()
			fn, err := val.AsFunction()
			boundaryCheck(b, err)
			arg, err := v8.NewValue(iso, int32(1))
			boundaryCheck(b, err)
			defer arg.Release()
			args := make([]v8.Valuer, arity)
			for i := range args {
				args[i] = arg
			}
			recv := v8.Undefined(iso)
			defer b.StopTimer()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := fn.Call(recv, args...)
				boundaryCheck(b, err)
				result.Release()
			}
		})
		b.Run(fmt.Sprintf("Callback/Arity%d", arity), func(b *testing.B) {
			iso := v8.NewIsolate()
			defer iso.Dispose()
			global := v8.NewObjectTemplate(iso)
			callback := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value { defer info.Release(); return nil })
			boundaryCheck(b, global.Set("callback", callback))
			ctx := v8.NewContext(iso, global)
			defer ctx.Close()
			args := make([]string, arity)
			for i := range args {
				args[i] = "1"
			}
			val, err := ctx.RunScript("(function() { for (let i=0;i<64;i++) callback("+strings.Join(args, ",")+"); })", "bench.js")
			boundaryCheck(b, err)
			defer val.Release()
			fn, err := val.AsFunction()
			boundaryCheck(b, err)
			recv := v8.Undefined(iso)
			before := ctx.RetainedValueCount()
			b.ReportAllocs()
			defer b.StopTimer()
			b.ResetTimer()
			for i := 0; i < b.N; i++ {
				result, err := fn.Call(recv)
				boundaryCheck(b, err)
				result.Release()
			}
			b.StopTimer()
			b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/64, "ns/callback")
			b.ReportMetric(float64(ctx.RetainedValueCount()-before), "retained")
		})
	}
	for _, target := range []string{"Named", "Indexed", "Internal"} {
		for _, kind := range []string{"Int32", "Float64", "String", "Value"} {
			b.Run("Set/"+target+"/"+kind, func(b *testing.B) {
				boundaryRecycled(b, func(iso *v8.Isolate, ctx *v8.Context) func() {
					tmpl := v8.NewObjectTemplate(iso)
					tmpl.SetInternalFieldCount(1)
					obj, err := tmpl.NewInstance(ctx)
					boundaryCheck(b, err)
					var value interface{}
					switch kind {
					case "Int32":
						value = int32(42)
					case "Float64":
						value = float64(42.5)
					case "String":
						value = "boundary"
					case "Value":
						value, err = v8.NewValue(iso, int32(42))
						boundaryCheck(b, err)
					}
					return func() {
						var err error
						switch target {
						case "Named":
							err = obj.Set("value", value)
						case "Indexed":
							err = obj.SetIdx(0, value)
						case "Internal":
							err = obj.SetInternalField(0, value)
						}
						boundaryCheck(b, err)
					}
				})
			})
		}
	}
	for _, kind := range []string{"Dynamic", "Cached"} {
		b.Run("MethodCall/"+kind, func(b *testing.B) {
			boundaryRecycled(b, func(_ *v8.Isolate, ctx *v8.Context) func() {
				val, err := ctx.RunScript("({value: 42, method() { return this.value; }})", "bench.js")
				boundaryCheck(b, err)
				obj, err := val.AsObject()
				boundaryCheck(b, err)
				prop, err := obj.Get("method")
				boundaryCheck(b, err)
				fn, err := prop.AsFunction()
				boundaryCheck(b, err)
				return func() {
					var result *v8.Value
					var err error
					if kind == "Dynamic" {
						result, err = obj.MethodCall("method")
					} else {
						result, err = fn.Call(obj)
					}
					boundaryCheck(b, err)
					result.Release()
				}
			})
		})
	}
	for _, encoding := range []string{"ASCII", "Unicode"} {
		for _, size := range []int{0, 16, 1024, 65536} {
			input := strings.Repeat("a", size)
			if encoding == "Unicode" {
				input = strings.Repeat("é", size/2)
			}
			for _, direction := range []string{"Input", "Output"} {
				b.Run(fmt.Sprintf("String/%s/%s/Bytes%d", direction, encoding, size), func(b *testing.B) {
					iso, _ := boundaryContext(b)
					val, err := v8.NewValue(iso, input)
					boundaryCheck(b, err)
					defer val.Release()
					b.SetBytes(int64(len(input)))
					defer b.StopTimer()
					b.ResetTimer()
					for i := 0; i < b.N; i++ {
						if direction == "Input" {
							value, err := v8.NewValue(iso, input)
							boundaryCheck(b, err)
							value.Release()
						} else {
							boundaryStringSink = val.String()
						}
					}
				})
			}
		}
	}
	for _, size := range []int{1, 8, 64} {
		for _, method := range []string{"Scalar", "SetMany"} {
			b.Run(fmt.Sprintf("Batch/%s/Properties%d", method, size), func(b *testing.B) {
				iso, ctx := boundaryContext(b)
				obj := ctx.Global()
				defer obj.Release()
				val, err := v8.NewValue(iso, int32(42))
				boundaryCheck(b, err)
				defer val.Release()
				properties := make([]v8.Property, size)
				for i := range properties {
					properties[i] = v8.Property{Key: fmt.Sprintf("key%d", i), Value: val}
					boundaryCheck(b, obj.Set(properties[i].Key, val))
				}
				// Descriptors and caller-owned values are reused in both paths;
				// SetMany's native marshalling remains inside the measured call.
				defer b.StopTimer()
				b.ResetTimer()
				for i := 0; i < b.N; i++ {
					if method == "Scalar" {
						for _, property := range properties {
							boundaryCheck(b, obj.Set(property.Key, property.Value))
						}
					} else {
						boundaryCheck(b, obj.SetMany(properties))
					}
				}
			})
		}
	}
	b.Run("Read/GetNumberRelease", func(b *testing.B) {
		iso, ctx := boundaryContext(b)
		obj := ctx.Global()
		defer obj.Release()
		val, err := v8.NewValue(iso, float64(42.5))
		boundaryCheck(b, err)
		defer val.Release()
		boundaryCheck(b, obj.Set("value", val))
		before := ctx.RetainedValueCount()
		defer b.StopTimer()
		b.ResetTimer()
		for i := 0; i < b.N; i++ {
			value, err := obj.Get("value")
			boundaryCheck(b, err)
			boundaryNumberSink = value.Number()
			value.Release()
		}
		b.StopTimer()
		b.ReportMetric(float64(ctx.RetainedValueCount()-before), "retained")
	})
}

var boundaryStringSink string
var boundaryNumberSink float64
