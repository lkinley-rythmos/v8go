// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

// #include "v8go.h"
import "C"

import (
	"fmt"
	"math/big"
	"reflect"
	"runtime"
	"unsafe"
)

type propertyInput struct {
	primitive C.PrimitiveValue
	handle    C.ValuePtr
	data      unsafe.Pointer
	text      string
	words     []C.uint64_t
}

func prepareProperty(val interface{}) (propertyInput, error) {
	var input propertyInput
	switch v := val.(type) {
	case string:
		ptr, n, err := borrowedString(v)
		if err != nil {
			return input, err
		}
		input.primitive.kind = C.PrimitiveString
		input.primitive.length = n
		input.text = v
		input.data = unsafe.Pointer(ptr)
	case int32:
		input.primitive.kind = C.PrimitiveInt32
		input.primitive.signed_value = C.int64_t(v)
	case uint32:
		input.primitive.kind = C.PrimitiveUint32
		input.primitive.unsigned_value = C.uint64_t(v)
	case int64:
		input.primitive.kind = C.PrimitiveInt64
		input.primitive.signed_value = C.int64_t(v)
	case uint64:
		input.primitive.kind = C.PrimitiveUint64
		input.primitive.unsigned_value = C.uint64_t(v)
	case float64:
		input.primitive.kind = C.PrimitiveNumber
		input.primitive.number = C.double(v)
	case bool:
		input.primitive.kind = C.PrimitiveBoolean
		if v {
			input.primitive.signed_value = 1
		}
	case *big.Int:
		if v == nil {
			return input, fmt.Errorf("v8go: nil BigInt property")
		}
		if v.IsInt64() {
			input.primitive.kind = C.PrimitiveInt64
			input.primitive.signed_value = C.int64_t(v.Int64())
			break
		}
		if v.IsUint64() {
			input.primitive.kind = C.PrimitiveUint64
			input.primitive.unsigned_value = C.uint64_t(v.Uint64())
			break
		}
		input.primitive.kind = C.PrimitiveWords
		if v.Sign() < 0 {
			input.primitive.signed_value = 1
		}
		bits := v.Bits()
		input.words = make([]C.uint64_t, len(bits))
		for i, word := range bits {
			input.words[i] = C.uint64_t(word)
		}
		input.primitive.length = C.int(len(input.words))
		input.data = unsafe.Pointer(&input.words[0])
	case Valuer:
		if nilValuer(v) || v.value() == nil || v.value().ptr == nil {
			return input, fmt.Errorf("v8go: nil property value")
		}
		input.primitive.kind = C.PrimitiveHandle
		input.handle = v.value().ptr
	default:
		return input, fmt.Errorf("v8go: unsupported object property type `%T`", val)
	}
	return input, nil
}

func (o *Object) setValue(target C.int, key string, idx uint32, val interface{}) error {
	input, err := prepareProperty(val)
	if err != nil {
		return err
	}
	key = legacyString(key)
	ptr, n, err := borrowedString(key)
	if err != nil {
		return err
	}
	result := C.ObjectSetValue(o.ptr, target, ptr, n, C.uint32_t(idx), input.primitive, input.handle, input.data)
	runtime.KeepAlive(input)
	runtime.KeepAlive(key)
	runtime.KeepAlive(val)
	if result.status == -1 {
		panic(fmt.Errorf("index out of range [%v] with length %v", idx, o.InternalFieldCount()))
	}
	return propertyResult(result)
}

func propertyResult(result C.RtnStatus) error {
	switch result.status {
	case 1:
		return nil
	case -2:
		return fmt.Errorf("v8go: property value belongs to a different isolate or is nil")
	default:
		return newJSError(result.error)
	}
}

// Valuer may also be a struct embedding a Value, so IsNil is only valid for
// nil-capable reflection kinds.
func nilValuer(v Valuer) bool {
	if v == nil {
		return true
	}
	rv := reflect.ValueOf(v)
	switch rv.Kind() {
	case reflect.Chan, reflect.Func, reflect.Interface, reflect.Map, reflect.Pointer, reflect.Slice:
		return rv.IsNil()
	default:
		return false
	}
}

func valuerValue(v Valuer) *Value {
	if value, ok := v.(*Value); ok {
		return value
	}
	if nilValuer(v) {
		return nil
	}
	return v.value()
}
