// Copyright 2019 Roger Chapman and the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go

//go:generate clang-format -i --verbose -style=Chromium v8go.h v8go.cc

// #cgo CXXFLAGS: -fno-rtti -fPIC -std=c++20 -DV8_COMPRESS_POINTERS -DV8_31BIT_SMIS_ON_64BIT_ARCH -I${SRCDIR}/deps/include -Wall -DV8_ENABLE_SANDBOX
// #cgo LDFLAGS: -pthread -lv8
// #cgo linux LDFLAGS: -ldl
import "C"

// This import forces `go mod vendor` to include V8's public headers.
// Native libraries and matching libc++ headers are installed separately.
// DO NOT REMOVE
import (
	_ "github.com/lkinley-rythmos/v8go/deps/include"
)
