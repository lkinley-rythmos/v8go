// Copyright 2026 the v8go contributors. All rights reserved.
// Use of this source code is governed by a BSD-style license that can be
// found in the LICENSE file.

package v8go_test

import (
	"strings"
	"testing"

	v8 "github.com/lkinley-rythmos/v8go"
)

func TestStringBoundary(t *testing.T) {
	iso := v8.NewIsolate()
	defer iso.Dispose()
	ctx := v8.NewContext(iso)
	defer ctx.Close()
	for _, s := range []string{"", "hello", "λ😀世界", "a\x00b", strings.Repeat("λ😀", 16384)} {
		v, err := v8.NewValue(iso, s)
		if err != nil {
			t.Fatal(err)
		}
		if got := v.String(); got != s {
			t.Errorf("round trip length %d: got length %d", len(s), len(got))
		}
		v.Release()
	}
	invalid, err := v8.NewValue(iso, "a\xffb")
	if err != nil {
		t.Fatal(err)
	}
	if got := invalid.String(); got != "a�b" {
		t.Errorf("invalid UTF-8: %q", got)
	}
	invalid.Release()
	for _, tc := range []struct{ source, want string }{
		{`"\ud800"`, "�"},
		{`({toString(){return "λ😀"}})`, "λ😀"},
		{`({toString(){throw Error("boom")}})`, ""},
		{`globalThis.conversions=0; ({toString(){return String(++conversions)}})`, "1"},
		{"'prefix'\x00this is invalid", "prefix"},
	} {
		v, err := ctx.RunScript(tc.source, "origin\x00ignored")
		if err != nil {
			t.Fatal(err)
		}
		if got := v.String(); got != tc.want {
			t.Errorf("%q: got %q want %q", tc.source, got, tc.want)
		}
		v.Release()
	}
	parsed, err := v8.JSONParse(ctx, `"prefix"`+"\x00invalid")
	if err != nil {
		t.Fatal(err)
	}
	if parsed.String() != "prefix" {
		t.Fatal("JSON first-NUL behavior changed")
	}
	parsed.Release()
	script, err := iso.CompileUnboundScript("'prefix'\x00invalid", "origin\x00ignored", v8.CompileOptions{})
	if err != nil {
		t.Fatal(err)
	}
	value, err := script.Run(ctx)
	if err != nil {
		t.Fatal(err)
	}
	if value.String() != "prefix" {
		t.Fatal("compile first-NUL behavior changed")
	}
	value.Release()
	tmpl := v8.NewObjectTemplate(iso)
	if err := tmpl.Set("prefix\x00ignored", "value"); err != nil {
		t.Fatal(err)
	}
	obj, err := tmpl.NewInstance(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer obj.Release()
	value, err = obj.Get("prefix")
	if err != nil {
		t.Fatal(err)
	}
	defer value.Release()
	if value.String() != "value" {
		t.Fatal("template first-NUL behavior changed")
	}
}
