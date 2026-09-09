// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

// #include "v8go.h"
import "C"

import (
	"fmt"
	"runtime"
	"unsafe"
)

// Property is a named, caller-owned value to assign with SetMany.
// Unlike the legacy Set API, Key supports embedded NUL bytes.
type Property struct {
	Key   string
	Value Valuer
}

// SetMany sets properties in slice order using one native call. Duplicate keys
// are allowed. Values must be non-nil and belong to the object's isolate; these
// conditions are checked before any writes. A JavaScript exception stops the
// batch, leaving earlier writes in place. Like Set, silently rejected writes
// (for example, a proxy set trap returning false) are not reported as errors.
// Values remain owned by the caller. An empty batch does nothing.
func (o *Object) SetMany(properties []Property) error {
	if len(properties) == 0 {
		return nil
	}
	names := make([]C.PropertyName, len(properties))
	values := make([]C.ValuePtr, len(properties))
	total := 0
	for i, p := range properties {
		if p.Value == nil || nilValuer(p.Value) || p.Value.value() == nil || p.Value.value().ptr == nil {
			return fmt.Errorf("v8go: property %d has a nil value", i)
		}
		_, length, err := borrowedString(p.Key)
		if err != nil {
			return fmt.Errorf("v8go: property %d: %w", i, err)
		}
		if len(p.Key) > int(^uint(0)>>1)-total {
			return fmt.Errorf("v8go: property keys exceed addressable size")
		}
		names[i] = C.PropertyName{offset: C.size_t(total), length: length}
		total += len(p.Key)
		values[i] = p.Value.value().ptr
	}
	keys := make([]byte, 0, total)
	for _, p := range properties {
		keys = append(keys, p.Key...)
	}
	result := C.ObjectSetMany(o.ptr, (*C.char)(unsafe.Pointer(unsafe.SliceData(keys))), unsafe.SliceData(names), unsafe.SliceData(values), C.size_t(len(properties)))
	runtime.KeepAlive(properties)
	return propertyResult(result)
}
