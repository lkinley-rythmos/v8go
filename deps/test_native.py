import hashlib
import io
import json
import os
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest import mock
import native


SCRIPT = Path(__file__).with_name('native.py')


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root / 'native.tar.gz'
        self.prefix = self.root / 'installed'

    def make_archive(self, extra=None, version='v0.10.0-rc.1', platform='linux_amd64'):
        files = {
            'manifest.json': json.dumps({'release': version, 'v8': '15.2.124.21',
                                         'platform': platform}).encode(),
            'lib/libv8.a': b'!<arch>\n',
            'include/libcxx/vector': b'header',
        }
        if extra:
            files.update(extra)
        with tarfile.open(self.archive, 'w:gz') as tar:
            for name, data in files.items():
                member = tarfile.TarInfo(name)
                member.size = len(data)
                tar.addfile(member, io.BytesIO(data))
        return hashlib.sha256(self.archive.read_bytes()).hexdigest()

    def install(self, checksum, platform='linux_amd64'):
        return subprocess.run([
            sys.executable, str(SCRIPT), 'install', '--release', 'v0.10.0-rc.1',
            '--platform', platform, '--archive', str(self.archive),
            '--sha256', checksum, '--prefix', str(self.prefix),
        ], text=True, capture_output=True)

    def test_installs_verified_archive_and_writes_usable_environment(self):
        result = self.install(self.make_archive())
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertEqual((self.prefix / 'lib/libv8.a').read_bytes(), b'!<arch>\n')
        result = subprocess.run(['sh', '-c', '. "$1/env.sh"; printf "%s" "$CGO_LDFLAGS"',
                                 'sh', str(self.prefix)], text=True, capture_output=True)
        self.assertEqual(result.returncode, 0, result.stderr)
        self.assertIn(str(self.prefix / 'lib'), result.stdout)

    def test_rejects_corrupt_download_before_installing(self):
        self.make_archive()
        result = self.install('0' * 64)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('checksum', result.stderr.lower())
        self.assertFalse(self.prefix.exists())

    def test_rejects_archive_path_traversal(self):
        result = self.install(self.make_archive({'../escaped': b'bad'}))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('unsafe', result.stderr.lower())
        self.assertFalse((self.root / 'escaped').exists())
        self.assertFalse(self.prefix.exists())

    def test_ci_rejects_sdk_with_another_input_fingerprint(self):
        with mock.patch.dict(os.environ, {'V8_BUILD_INPUT_KEY': 'expected-key'}):
            result = self.install(self.make_archive())
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('fingerprint', result.stderr)
        self.assertFalse(self.prefix.exists())

    def test_ci_accepts_matching_sdk_fingerprint(self):
        manifest = {'release': 'v0.10.0-rc.1', 'v8': '15.2.124.21',
                    'platform': 'linux_amd64', 'build_input_key': 'expected-key'}
        with mock.patch.dict(os.environ, {'V8_BUILD_INPUT_KEY': 'expected-key'}):
            result = self.install(self.make_archive({'manifest.json': json.dumps(manifest).encode()}))
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_rejects_wrong_release(self):
        result = self.install(self.make_archive(version='v0.9.0'))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('release', result.stderr.lower())
        self.assertFalse(self.prefix.exists())

    def test_installs_musl_archive_with_matching_libcxx_configuration(self):
        for arch in ('amd64', 'arm64'):
            with self.subTest(arch=arch):
                target = 'linux_musl_' + arch
                result = self.install(self.make_archive(platform=target), target)
                self.assertEqual(result.returncode, 0, result.stderr)
                self.assertIn('-DV8GO_USE_MUSL', (self.prefix / 'env.sh').read_text())
                import shutil
                shutil.rmtree(self.prefix)

    def test_rejects_glibc_archive_requested_as_musl(self):
        result = self.install(self.make_archive(), 'linux_musl_amd64')
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('release/platform', result.stderr)
        self.assertFalse(self.prefix.exists())


class PlatformTests(unittest.TestCase):
    def test_detects_both_libcs_and_architectures(self):
        for cpu, arch in [('x86_64', 'amd64'), ('aarch64', 'arm64')]:
            for libc, prefix in [('glibc', 'linux_'), ('musl', 'linux_musl_')]:
                with self.subTest(cpu=cpu, libc=libc), \
                     mock.patch.object(native.platform, 'system', return_value='Linux'), \
                     mock.patch.object(native.platform, 'machine', return_value=cpu), \
                     mock.patch.object(native.platform, 'libc_ver', return_value=(libc, '')):
                    self.assertEqual(native.host_platform(), prefix + arch)

    def test_detects_musl_loader_when_python_does_not_identify_libc(self):
        with mock.patch.object(native.platform, 'system', return_value='Linux'), \
             mock.patch.object(native.platform, 'machine', return_value='x86_64'), \
             mock.patch.object(native.platform, 'libc_ver', return_value=('', '')), \
             mock.patch.object(native.Path, 'glob', return_value=iter([Path('/lib/ld-musl-x86_64.so.1')])):
            self.assertEqual(native.host_platform(), 'linux_musl_amd64')


class RustArchiveTests(unittest.TestCase):
    def test_collects_target_prebuilt_stdlib_but_not_host_stdlib(self):
        with tempfile.TemporaryDirectory() as temp:
            build = Path(temp)
            (build / 'obj').mkdir()
            (build / 'obj/v8_monolith.ninja').write_text(
                'build monolith: alink obj/project.rlib || phony/build/rust/std/prebuilt_rustc_copy_to_sysroot\n')
            target = build / 'prebuilt_rustc_sysroot/lib/rustlib/x86_64-unknown-linux-musl/lib'
            target.mkdir(parents=True)
            for name in ('libstd.rlib', 'libcore.rlib', 'liballoc.rlib'):
                (target / name).touch()
            host = build / 'clang_x64_glibc/prebuilt_rustc_sysroot/lib/rustlib/x86_64-unknown-linux-gnu/lib'
            host.mkdir(parents=True)
            (host / 'libstd.rlib').touch()
            archives = native.rust_archives(build, 'linux_musl_amd64')
            self.assertEqual(len(archives), 4)
            self.assertIn('obj/project.rlib', archives)
            self.assertTrue(any(p.endswith('/libcore.rlib') for p in archives))
            self.assertFalse(any(p.startswith('clang_') for p in archives))

    def test_fails_when_prebuilt_target_runtime_is_missing(self):
        with tempfile.TemporaryDirectory() as temp:
            build = Path(temp)
            (build / 'obj').mkdir()
            (build / 'obj/v8_monolith.ninja').write_text(
                'build monolith: alink obj/project.rlib || phony/build/rust/std/prebuilt_rustc_copy_to_sysroot\n')
            with self.assertRaisesRegex(ValueError, 'prebuilt Rust'):
                native.rust_archives(build, 'linux_musl_amd64')


if __name__ == '__main__':
    unittest.main()
