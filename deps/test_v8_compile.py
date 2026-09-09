"""Check custom toolchain routing without downloading or compiling V8."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest


class CustomToolchainTests(unittest.TestCase):
    def test_binding_generator_uses_the_custom_rust_toolchain(self):
        with tempfile.TemporaryDirectory() as temp:
            root = Path(temp)
            deps = root / 'deps'
            tools = deps / 'depot_tools'
            tools.mkdir(parents=True)
            (tools / 'python3_bin_reldir.txt').touch()
            (deps / 'args').mkdir()
            (deps / 'args/linux.gn').write_text('is_debug=false\n')
            output = deps / 'v8/out/release-arm64/obj'
            output.mkdir(parents=True)
            (output / 'libv8_monolith.a').touch()
            shutil.copyfile(Path(__file__).with_name('v8_compile.sh'),
                            deps / 'v8_compile.sh')
            rust = root / 'rust'
            (rust / 'bin').mkdir(parents=True)
            for path, body in [
                (rust / 'bin/rustc', 'echo "rustc 1.98.0-nightly (revision date)"'),
                (tools / 'gn', 'if [ "$1" = gen ]; then printf "%s\\n" "$3" > "$GN_CAPTURE"; fi'),
                (tools / 'ninja', 'exit 0'),
            ]:
                path.write_text('#!/bin/sh\n' + body + '\n')
                path.chmod(0o755)
            capture = root / 'gn-args'
            env = dict(os.environ, V8_CLANG_BASE_PATH=str(root / 'clang'),
                       V8_RUST_SYSROOT=str(rust), GN_CAPTURE=str(capture))
            subprocess.run(['sh', str(deps / 'v8_compile.sh'), 'arm64'],
                           env=env, check=True, capture_output=True, text=True)
            args = capture.read_text()
            # rust_sysroot_absolute alone leaves bindgen/rustfmt pointing at
            # Chromium's x86 prebuilts, which cannot run on an ARM64 host.
            self.assertIn(f'rust_sysroot_absolute="{rust}"', args)
            self.assertIn(f'rust_bindgen_root="{rust}"', args)
            self.assertIn('rustc_version="rustc1.98.0-nightlyrevisiondate"', args)
