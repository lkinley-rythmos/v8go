from pathlib import Path
import tempfile
import unittest

from check_musl_commands import check


class CommandGraphTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        (self.root / 'clang_x64_glibc').mkdir()
        self.host = self.root / 'clang_x64_glibc/rust.ninja'
        self.target = self.root / 'rust.ninja'
        self.host.write_text('rustflags = --target=x86_64-unknown-linux-gnu\n')
        self.target.write_text('rustflags = --target=x86_64-unknown-linux-musl\n')
        self.commands = (
            'clang++ --target=x86_64-unknown-linux-gnu -c host.cc -o clang_x64_glibc/host.o\n'
            'ccache clang++ --target=x86_64-unknown-linux-musl -c target.cc -o obj/target.o\n'
        )

    def test_accepts_separate_host_and_target_libcs(self):
        self.assertEqual(check(self.commands, self.root),
                         dict(host_cpp=1, target_cpp=1, host_rust=1, target_rust=1))

    def test_rejects_glibc_target_object(self):
        with self.assertRaisesRegex(ValueError, 'wrong libc target'):
            check(self.commands.replace('linux-musl', 'linux-gnu'), self.root)

    def test_rejects_musl_host_rust_tool(self):
        self.host.write_text('rustflags = --target=x86_64-unknown-linux-musl\n')
        with self.assertRaisesRegex(ValueError, 'wrong Rust libc target'):
            check(self.commands, self.root)

    def test_rejects_bundled_bindgen(self):
        with self.assertRaisesRegex(ValueError, 'bundled Rust'):
            check(self.commands + 'python3 wrapper.py --bindgen-exe ../../third_party/rust-toolchain/bin/bindgen\n', self.root)

    def test_rejects_unsupported_musl_allocator_features(self):
        for flag in ('USE_ALLOCATOR_SHIM', 'HAS_MEMORY_TAGGING'):
            with self.subTest(flag=flag):
                (self.root / 'toolchain.ninja').write_text(f'  rspfile_content = --flags {flag}=true\n')
                with self.assertRaisesRegex(ValueError, flag):
                    check(self.commands, self.root)

    def test_allows_glibc_host_allocator_features(self):
        (self.root / 'clang_x64_glibc/toolchain.ninja').write_text(
            '  rspfile_content = --flags USE_ALLOCATOR_SHIM=true HAS_MEMORY_TAGGING=true\n')
        check(self.commands, self.root)
