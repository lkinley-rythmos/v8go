// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

//go:build linux && cgo

// Package teststack probes the calling native thread for Linux regression tests.
// It is not imported by the production binding.
package teststack

/*
#cgo CFLAGS: -pthread
#cgo LDFLAGS: -pthread
#define _GNU_SOURCE
#include <pthread.h>
#include <stdint.h>

static int thread_stack(uintptr_t *thread, size_t *capacity, size_t *headroom) {
    pthread_attr_t attr;
    void *base;
    volatile char marker;
    *thread = (uintptr_t)pthread_self();
    int err = pthread_getattr_np(pthread_self(), &attr);
    if (err != 0) return err;
    err = pthread_attr_getstack(&attr, &base, capacity);
    int destroy_err = pthread_attr_destroy(&attr);
    if (err != 0) return err;
    if (destroy_err != 0) return destroy_err;
    // Supported Linux amd64 and arm64 native stacks grow downwards.
    uintptr_t current = (uintptr_t)&marker;
    uintptr_t bottom = (uintptr_t)base;
    *headroom = current >= bottom && current - bottom <= *capacity
        ? current - bottom : 0;
    return 0;
}
*/
import "C"

import (
	"fmt"
	"syscall"
)

// Current reports the pthread identity, total capacity, and remaining native
// stack bytes. The caller must stay on the same locked OS thread when using it.
func Current() (thread uintptr, capacity, headroom uint64, err error) {
	var id C.uintptr_t
	var size, remaining C.size_t
	if code := C.thread_stack(&id, &size, &remaining); code != 0 {
		return 0, 0, 0, fmt.Errorf("querying pthread stack: %w", syscall.Errno(code))
	}
	return uintptr(id), uint64(size), uint64(remaining), nil
}
