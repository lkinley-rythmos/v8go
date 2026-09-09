// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

// #include "v8go.h"
import "C"

import (
	"errors"
	"strings"
	"unsafe"
)

// legacyString preserves the first-NUL truncation of the original C-string APIs.
func legacyString(s string) string {
	if n := strings.IndexByte(s, 0); n >= 0 {
		return s[:n]
	}
	return s
}

// borrowedString is read-only and must only be used during a synchronous native
// call. The caller keeps s alive until that call returns; V8 copies its bytes.
func borrowedString(s string) (*C.char, C.int, error) {
	// Matches v8::String::kMaxLength for the supported 64-bit native builds.
	const maxStringLength = (1 << 29) - 24
	if len(s) > maxStringLength {
		return nil, 0, errors.New("v8go: string exceeds V8 maximum length")
	}
	return (*C.char)(unsafe.Pointer(unsafe.StringData(s))), C.int(len(s)), nil
}
