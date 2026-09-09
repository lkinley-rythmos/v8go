from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

from ci_cache import fingerprints


class CacheFingerprintTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        for name in ('VERSION', 'LICENSE', 'deps/VERSION', 'deps/.gclient', 'deps/.gclient-custom',
                     'deps/v8_download.sh', 'deps/v8_compile.sh', 'deps/native.py',
                     'deps/setup-linux-toolchains.sh', 'deps/rust-toolchain.json',
                     'deps/llvm-version', 'deps/setup-musl-sysroot.sh',
                     'deps/Dockerfile.musl-sysroot', 'deps/apply_musl_patch.py',
                     'deps/check_musl_commands.py', 'deps/args/linux.gn',
                     'deps/patches/linux-musl.patch', 'deps/include/v8.h',
                     '.github/actions/build-linux/action.yml', 'deps/ci_cache.py'):
            p = self.root / name
            p.parent.mkdir(parents=True, exist_ok=True)
            p.write_text('initial')
        self.pins = {'deps/v8': 'v8-pin', 'deps/depot_tools': 'depot-pin'}
        mock = patch('ci_cache.gitlink', side_effect=lambda root, name: self.pins[name])
        mock.start()
        self.addCleanup(mock.stop)

    def keys(self, platform='linux_musl_arm64'):
        return fingerprints(self.root, platform)

    def test_wrapper_changes_reuse_sdk(self):
        before = self.keys()
        (self.root / 'value.go').write_text('changed wrapper')
        self.assertEqual(before, self.keys())

    def test_packaged_header_and_release_invalidate_sdk_only(self):
        for name in ('deps/include/v8.h', 'VERSION'):
            before = self.keys()
            (self.root / name).write_text('changed')
            after = self.keys()
            self.assertNotEqual(before['sdk'], after['sdk'])
            self.assertEqual(before['source'], after['source'])

    def test_patch_does_not_invalidate_pristine_source(self):
        before = self.keys()
        (self.root / 'deps/patches/linux-musl.patch').write_text('fix')
        after = self.keys()
        self.assertNotEqual(before['sdk'], after['sdk'])
        self.assertEqual(before['source'], after['source'])

    def test_engine_pin_invalidates_source_and_sdk(self):
        before = self.keys()
        self.pins['deps/v8'] = 'new-pin'
        after = self.keys()
        for kind in ('source', 'sdk'):
            self.assertNotEqual(before[kind], after[kind])

    def test_toolchain_pin_invalidates_sdk_and_toolchain(self):
        before = self.keys()
        (self.root / 'deps/llvm-version').write_text('new compiler')
        after = self.keys()
        for kind in ('toolchain', 'sdk'):
            self.assertNotEqual(before[kind], after[kind])

    def test_targets_are_isolated_but_arm_sources_are_shared(self):
        musl, glibc = self.keys(), self.keys('linux_arm64')
        self.assertNotEqual(musl['sdk'], glibc['sdk'])
        self.assertEqual(musl['source'], glibc['source'])
        self.assertEqual(musl['toolchain'], glibc['toolchain'])
        self.assertNotEqual(musl['source'], self.keys('linux_musl_amd64')['source'])
        self.assertNotEqual(self.keys('linux_amd64')['source'], self.keys('linux_musl_amd64')['source'])

    def test_custom_sync_profile_invalidates_source_and_sdk(self):
        before = self.keys()
        (self.root / 'deps/.gclient-custom').write_text('changed')
        after = self.keys()
        for kind in ('source', 'sdk'):
            self.assertNotEqual(before[kind], after[kind])
