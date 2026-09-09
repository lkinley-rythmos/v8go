// Copyright 2021 Roger Chapman and the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

// #include "v8go.h"
import "C"
import (
	"errors"
	"fmt"
	"runtime"
	"unsafe"
)

// Object is a JavaScript object (ECMA-262, 4.3.3)
type Object struct {
	*Value
}

func (o *Object) MethodCall(methodName string, args ...Valuer) (*Value, error) {
	methodName = legacyString(methodName)
	key, length, err := borrowedString(methodName)
	if err != nil {
		return nil, err
	}
	var argv []C.ValuePtr
	if len(args) > 0 {
		argv = make([]C.ValuePtr, len(args))
		for i, arg := range args {
			if arg == nil || nilValuer(arg) || arg.value() == nil {
				return nil, fmt.Errorf("v8go: nil method argument")
			}
			argv[i] = arg.value().ptr
		}
	}
	result := C.ObjectMethodCall(o.ptr, key, length, C.int(len(argv)), unsafe.SliceData(argv))
	runtime.KeepAlive(methodName)
	runtime.KeepAlive(args)
	if result.not_function != 0 {
		return nil, errors.New("v8go: value is not a Function")
	}
	return valueResult(o.ctx, result.result)
}

// Set sets a named property. Primitive inputs are copied into V8; caller-owned
// Values remain valid and must be released by their owner.
func (o *Object) Set(key string, val interface{}) error {
	return o.setValue(C.PropertyNamed, key, 0, val)
}

// SetIdx sets an indexed property with the same ownership rules as Set.
func (o *Object) SetIdx(idx uint32, val interface{}) error {
	if v, ok := val.(Valuer); ok {
		handle := valuerValue(v)
		if handle == nil || handle.ptr == nil {
			return fmt.Errorf("v8go: nil property value")
		}
		result := C.ObjectSetIdxHandle(o.ptr, C.uint32_t(idx), handle.ptr)
		runtime.KeepAlive(val)
		return propertyResult(result)
	}
	return o.setValue(C.PropertyIndexed, "", idx, val)
}

// SetInternalField sets an internal field. It panics if idx is out of range.
func (o *Object) SetInternalField(idx uint32, val interface{}) error {
	if v, ok := val.(Valuer); ok {
		handle := valuerValue(v)
		if handle == nil || handle.ptr == nil {
			return fmt.Errorf("v8go: nil property value")
		}
		result := C.ObjectSetInternalHandle(o.ptr, C.uint32_t(idx), handle.ptr)
		runtime.KeepAlive(val)
		if result.status == -1 {
			panic(fmt.Errorf("index out of range [%v] with length %v", idx, o.InternalFieldCount()))
		}
		return propertyResult(result)
	}
	return o.setValue(C.PropertyInternal, "", idx, val)
}

// InternalFieldCount returns the number of internal fields this Object has.
func (o *Object) InternalFieldCount() uint32 {
	count := C.ObjectInternalFieldCount(o.ptr)
	return uint32(count)
}

// Get tries to get a Value for a given Object property key.
func (o *Object) Get(key string) (*Value, error) {
	key = legacyString(key)
	ptr, length, err := borrowedString(key)
	if err != nil {
		return nil, err
	}
	rtn := C.ObjectGet(o.ptr, ptr, length)
	runtime.KeepAlive(key)
	return valueResult(o.ctx, rtn)
}

// GetInternalField gets the Value set by SetInternalField for the given index
// or the JS undefined value if the index hadn't been set.
// Panics if given an out of range index.
func (o *Object) GetInternalField(idx uint32) *Value {
	rtn := C.ObjectGetInternalField(o.ptr, C.int(idx))
	if rtn == nil {
		panic(fmt.Errorf("index out of range [%v] with length %v", idx, o.InternalFieldCount()))
	}
	return &Value{rtn, o.ctx}
}

// GetIdx tries to get a Value at a give Object index.
func (o *Object) GetIdx(idx uint32) (*Value, error) {
	rtn := C.ObjectGetIdx(o.ptr, C.uint32_t(idx))
	return valueResult(o.ctx, rtn)
}

// Has calls the abstract operation HasProperty(O, P) described in ECMA-262, 7.3.10.
// Returns true, if the object has the property, either own or on the prototype chain.
func (o *Object) Has(key string) bool {
	key = legacyString(key)
	ptr, length, err := borrowedString(key)
	if err != nil {
		return false
	}
	result := C.ObjectHas(o.ptr, ptr, length)
	runtime.KeepAlive(key)
	return result != 0
}

// HasIdx returns true if the object has a value at the given index.
func (o *Object) HasIdx(idx uint32) bool {
	return C.ObjectHasIdx(o.ptr, C.uint32_t(idx)) != 0
}

// Delete returns true if successful in deleting a named property on the object.
func (o *Object) Delete(key string) bool {
	key = legacyString(key)
	ptr, length, err := borrowedString(key)
	if err != nil {
		return false
	}
	result := C.ObjectDelete(o.ptr, ptr, length)
	runtime.KeepAlive(key)
	return result != 0
}

// DeleteIdx returns true if successful in deleting a value at a given index of the object.
func (o *Object) DeleteIdx(idx uint32) bool {
	return C.ObjectDeleteIdx(o.ptr, C.uint32_t(idx)) != 0
}
