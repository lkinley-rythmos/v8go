#!/bin/sh
# Fail early if the consumer compiler cannot use its sanitizer headers/runtime.
set -eu
CC=${CC:-clang-22}
go version
"$CC" --version
ld.lld --version
ldd --version 2>&1 || true

test_dir=$(mktemp -d "${TMPDIR:-/tmp}/v8go-sanitizer.XXXXXX")
trap 'rm -rf "$test_dir"' EXIT HUP INT TERM
"$CC" -x c -fsanitize=address -fuse-ld=lld -o "$test_dir/sanitizer" - <<'EOF'
#include <sanitizer/lsan_interface.h>
int main(void) {
    __lsan_do_leak_check();
    return 0;
}
EOF
"$test_dir/sanitizer"
