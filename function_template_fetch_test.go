// Copyright 2021 Roger Chapman and the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

// Ignore leaks within Go standard libraries http/https support code.
// The getaddrinfo detected leaks can be avoided using GODEBUG=netdns=go but
// currently there are more for loading system root certificates on macOS.
//go:build !leakcheck || !darwin
// +build !leakcheck !darwin

package v8go_test

import (
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"

	v8 "github.com/lkinley-rythmos/v8go"
)

func ExampleFunctionTemplate_fetch() {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		fmt.Fprintln(w, "<!DOCTYPE html>")
	}))
	defer server.Close()

	iso := v8.NewIsolate()
	defer iso.Dispose()
	global := v8.NewObjectTemplate(iso)
	complete := make(chan func())

	fetchfn := v8.NewFunctionTemplate(iso, func(info *v8.FunctionCallbackInfo) *v8.Value {
		args := info.Args()
		url := args[0].String()

		resolver, _ := v8.NewPromiseResolver(info.Context())

		go func() {
			res, err := http.Get(url)
			var body []byte
			if err == nil {
				body, err = io.ReadAll(res.Body)
				res.Body.Close()
			}
			// Dispatch V8 work back to the isolate's goroutine.
			complete <- func() {
				if err != nil {
					val, _ := v8.NewValue(iso, err.Error())
					resolver.Reject(val)
					return
				}
				val, _ := v8.NewValue(iso, string(body))
				resolver.Resolve(val)
			}
		}()
		return resolver.GetPromise().Value
	})
	global.Set("fetch", fetchfn, v8.ReadOnly)

	ctx := v8.NewContext(iso, global)
	defer ctx.Close()
	val, _ := ctx.RunScript(fmt.Sprintf("fetch(%q)", server.URL), "")
	prom, _ := val.AsPromise()

	// Wait for the HTTP request, then resolve the promise on this goroutine.
	(<-complete)()
	fmt.Printf("%s\n", strings.Split(prom.Result().String(), "\n")[0])
	// Output:
	// <!DOCTYPE html>
}
