#!/bin/sh
# Run with the installed native package's env.sh already sourced.
set -eu
repo=$(CDPATH= cd -- "$(dirname -- "$0")/.." && pwd)
engine=$(cat "$repo/deps/VERSION")
test_dir=$(mktemp -d "${TMPDIR:-/tmp}/v8go-consumer.XXXXXX")
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
cd "$test_dir"
go mod init example.com/v8go-consumer
go mod edit -replace "github.com/lkinley-rythmos/v8go=$repo"
cat > main.go <<'EOF'
package main

import (
    "fmt"
    "os"
    v8 "github.com/lkinley-rythmos/v8go"
)

func main() {
    if v8.Version() != os.Args[1] {
        panic("unexpected V8 version: " + v8.Version())
    }
    ctx := v8.NewContext()
    defer ctx.Isolate().Dispose()
    defer ctx.Close()
    value, err := ctx.RunScript("new Intl.NumberFormat('de-DE').format(1234.5)", "smoke.js")
    if err != nil || value.String() != "1.234,5" {
        panic(fmt.Sprintf("Intl smoke test failed: %v, %v", value, err))
    }
    fmt.Println("consumer OK, V8 " + v8.Version())
}
EOF
go mod tidy
go run . "$engine"
go mod vendor
go run -mod=vendor . "$engine"
