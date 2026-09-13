import hashlib
import json
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

import source_provenance


def sha256(data):
    return hashlib.sha256(data).hexdigest()


class SourceProvenanceTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.deps = self.root / 'deps'
        self.v8 = self.deps / 'v8'
        self.patches = self.deps / 'patches'
        self.patches.mkdir(parents=True)
        self.files = {
            'src/base/platform/platform.cc': (b'base before\n', b'base after\n'),
            'build/config/BUILDCONFIG.gn': (b'build before\n', b'build after\n'),
            'buildtools/third_party/libc++/__config_site': (b'tools before\n', b'tools after\n'),
            'third_party/partition_alloc/partition_alloc.gni': (b'pa before\n', b'pa after\n'),
        }
        self._init_repo(self.v8, ['src/base/platform/platform.cc'])
        self._init_repo(self.v8 / 'build', ['config/BUILDCONFIG.gn'])
        self._init_repo(self.v8 / 'buildtools', ['third_party/libc++/__config_site'])
        self._init_repo(self.v8 / 'third_party/partition_alloc', ['partition_alloc.gni'])
        self.extra = self.v8 / 'third_party/libc++/src'
        self._init_repo(self.extra, ['README'])
        self.nested_parent = self.v8 / 'third_party/fuzztest'
        self._init_repo(self.nested_parent, ['README'])
        self.nested_child = self.nested_parent / 'src'
        self._init_repo(self.nested_child, ['README'])
        self.depot = self.deps / 'depot_tools'
        self._init_repo(self.depot, ['README'])
        (self.v8 / '.gitignore').write_text('build/\nbuildtools/\nthird_party/\n')
        subprocess.run(['git', 'add', '.gitignore'], cwd=self.v8, check=True)
        subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                        'commit', '-qm', 'ignore gclient checkouts'], cwd=self.v8, check=True)
        self._write_patch_metadata()
        self._write_revision_pins()
        self._init_root_repo()

    def _init_repo(self, directory, names):
        directory.mkdir(parents=True)
        for name in names:
            path = directory / name
            path.parent.mkdir(parents=True, exist_ok=True)
            source = next((before for key, (before, _) in self.files.items()
                           if key.endswith(name)), b'initial\n')
            path.write_bytes(source)
        subprocess.run(['git', 'init', '-q'], cwd=directory, check=True)
        subprocess.run(['git', 'add', '.'], cwd=directory, check=True)
        subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                        'commit', '-qm', 'initial'], cwd=directory, check=True)

    def _write_patch_metadata(self):
        manifest = {name: {'before': sha256(before), 'after': sha256(after)}
                    for name, (before, after) in self.files.items()}
        (self.patches / 'linux-musl.json').write_text(json.dumps(manifest, sort_keys=True))
        (self.patches / 'linux-musl.patch').write_text('approved patch\n')

    def _write_revision_pins(self):
        entries = {}
        for path in (self.v8, self.v8 / 'build', self.v8 / 'buildtools',
                     self.v8 / 'third_party/partition_alloc', self.extra):
            name = 'v8' + ('' if path == self.v8 else '/' + str(path.relative_to(self.v8)))
            entries[name] = ('https://example.invalid/' + name if path == self.v8
                             else 'https://example.invalid/' + name + '@' + self._head(path))
        for path in (self.nested_parent, self.nested_child):
            name = 'v8/' + str(path.relative_to(self.v8))
            entries[name] = 'https://example.invalid/' + name + '@' + self._head(path)
        for profile in ('.gclient_entries', '.gclient-custom_entries'):
            (self.deps / profile).write_text('entries = ' + repr(entries) + '\n')
        (self.deps / '.gclient').write_text('default')
        (self.deps / '.gclient-custom').write_text('custom')
        (self.deps / 'source-provenance.json').write_text(json.dumps({
            'schema': 1,
            'v8_commit': self._head(self.v8),
            'repositories': {'v8': self._head(self.v8), **{
                name: value.rsplit('@', 1)[1] for name, value in entries.items() if name != 'v8'}},
        }))

    def _init_root_repo(self):
        subprocess.run(['git', 'init', '-q'], cwd=self.root, check=True)
        subprocess.run(['git', 'add', 'deps/patches', 'deps/.gclient', 'deps/.gclient-custom',
                        'deps/.gclient_entries', 'deps/.gclient-custom_entries',
                        'deps/source-provenance.json'], cwd=self.root, check=True)
        for name, path in (('deps/v8', self.v8), ('deps/depot_tools', self.depot)):
            subprocess.run(['git', 'update-index', '--add', '--cacheinfo',
                            f'160000,{self._head(path)},{name}'], cwd=self.root, check=True)
        subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                        'commit', '-qm', 'root'], cwd=self.root, check=True)

    @staticmethod
    def _head(path):
        return subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=path, text=True).strip()

    def _write_after(self, name, data=None):
        path = self.v8 / name
        path.write_bytes(self.files[name][1] if data is None else data)

    def test_clean_glibc_source_is_attested_without_raw_dirty_state(self):
        provenance = source_provenance.attest(self.root, 'linux_amd64')
        self.assertEqual(provenance, source_provenance.expected_provenance(self.root, 'linux_amd64'))

    def test_exact_musl_patch_is_attested_while_its_owner_repositories_are_dirty(self):
        for name in self.files:
            self._write_after(name)
        provenance = source_provenance.attest(self.root, 'linux_musl_amd64')
        self.assertEqual(provenance, source_provenance.expected_provenance(self.root, 'linux_musl_amd64'))
        self.assertTrue(subprocess.check_output(['git', 'status', '--porcelain'],
                                                cwd=self.v8 / 'build', text=True))

    def test_allows_only_a_clean_verified_nested_repository_boundary(self):
        self.assertEqual(source_provenance.attest(self.root, 'linux_amd64')['state'], 'clean')

    def test_rejects_dirty_wrong_pinned_extra_or_escaping_nested_boundaries(self):
        cases = {
            'dirty-child': lambda: (self.nested_child / 'README').write_text('dirty'),
            'wrong-pin': self._commit_nested_child,
            'extra-directory': lambda: (self.nested_parent / 'other').mkdir() or
            (self.nested_parent / 'other/file').write_text('untracked'),
            'escape': lambda: (self.nested_parent / 'escape').symlink_to(self.root),
        }
        for name, mutate in cases.items():
            with self.subTest(name=name):
                mutate()
                with self.assertRaisesRegex(ValueError, 'source provenance'):
                    source_provenance.attest(self.root, 'linux_amd64')
                self.tearDown()
                self.setUp()

    def _commit_nested_child(self):
        (self.nested_child / 'README').write_text('different revision')
        subprocess.run(['git', 'add', 'README'], cwd=self.nested_child, check=True)
        subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                        'commit', '-qm', 'different revision'], cwd=self.nested_child, check=True)

    def test_uses_custom_entries_for_custom_toolchain_without_default_fallback(self):
        (self.deps / '.gclient_entries').unlink()
        self._commit_root('deps/.gclient_entries', 'remove default entries')
        self.assertEqual(source_provenance.attest(self.root, 'linux_arm64')['state'], 'clean')
        with self.assertRaisesRegex(ValueError, 'revision pins'):
            source_provenance.attest(self.root, 'linux_amd64')

    def test_rejects_root_or_depot_tools_changes(self):
        for name, mutate in {
            'root': lambda: (self.root / 'deps/patches/linux-musl.patch').write_text('edited'),
            'depot': lambda: (self.depot / 'README').write_text('edited'),
        }.items():
            with self.subTest(name=name):
                mutate()
                with self.assertRaisesRegex(ValueError, 'source provenance'):
                    source_provenance.attest(self.root, 'linux_amd64')
                self.tearDown()
                self.setUp()

    def test_rejects_mutated_or_deleted_generated_pin_records_and_missing_repositories(self):
        entries_path = self.deps / '.gclient_entries'
        entries = eval(entries_path.read_text().split('=', 1)[1], {'__builtins__': {}})
        entries['v8/build'] = 'https://example.invalid/v8/build@' + '0' * 40
        entries_path.write_text('entries = ' + repr(entries) + '\n')
        self._commit_root(entries_path.relative_to(self.root), 'mutated generated pin')
        with self.assertRaisesRegex(ValueError, 'revision pins'):
            source_provenance.attest(self.root, 'linux_amd64')
        self.tearDown()
        self.setUp()
        (self.deps / '.gclient_entries').unlink()
        self._commit_root('deps/.gclient_entries', 'deleted generated pins')
        with self.assertRaisesRegex(ValueError, 'revision pins'):
            source_provenance.attest(self.root, 'linux_amd64')
        self.tearDown()
        self.setUp()
        shutil.rmtree(self.v8 / 'buildtools')
        with self.assertRaisesRegex(ValueError, 'revision pins'):
            source_provenance.attest(self.root, 'linux_amd64')

    def _commit_root(self, path, message):
        subprocess.run(['git', 'add', '-A', str(path)], cwd=self.root, check=True)
        subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                        'commit', '-qm', message], cwd=self.root, check=True)

    def test_rejects_unexpected_source_changes_and_partial_or_changed_patch_files(self):
        cases = {
            'untracked': lambda: (self.v8 / 'unexpected').write_text('bad'),
            'staged': lambda: self._stage_unexpected(),
            'partial': lambda: self._write_after('src/base/platform/platform.cc'),
            'changed-allowed': lambda: self._write_after('build/config/BUILDCONFIG.gn', b'not approved'),
        }
        for name, mutate in cases.items():
            with self.subTest(name=name):
                with self.assertRaisesRegex(ValueError, 'source provenance'):
                    mutate()
                    source_provenance.attest(self.root, 'linux_musl_amd64')
                self.tearDown()
                self.setUp()

    def _stage_unexpected(self):
        path = self.v8 / 'src/unexpected.cc'
        path.parent.mkdir(exist_ok=True)
        path.write_text('bad')
        subprocess.run(['git', 'add', str(path.relative_to(self.v8))], cwd=self.v8, check=True)

    def test_rejects_changed_revision_pin(self):
        self._write_after('build/config/BUILDCONFIG.gn')
        subprocess.run(['git', 'add', '.'], cwd=self.v8 / 'build', check=True)
        subprocess.run(['git', '-c', 'user.name=test', '-c', 'user.email=test@example.invalid',
                        'commit', '-qm', 'wrong revision'], cwd=self.v8 / 'build', check=True)
        with self.assertRaisesRegex(ValueError, 'revision'):
            source_provenance.attest(self.root, 'linux_musl_amd64')

    def test_rejects_missing_or_extra_patch_targets(self):
        for name in self.files:
            self._write_after(name)
        manifest_path = self.patches / 'linux-musl.json'
        manifest = json.loads(manifest_path.read_text())
        manifest['missing/file.gn'] = {'before': '0' * 64, 'after': '1' * 64}
        manifest_path.write_text(json.dumps(manifest))
        with self.assertRaisesRegex(ValueError, 'source provenance'):
            source_provenance.attest(self.root, 'linux_musl_amd64')

    def test_git_free_validator_rejects_missing_or_wrong_attestation(self):
        expected = source_provenance.expected_provenance(self.root, 'linux_musl_amd64')
        source_provenance.validate_manifest_provenance(expected, self.root, 'linux_musl_amd64')
        with self.assertRaisesRegex(ValueError, 'source provenance'):
            source_provenance.validate_manifest_provenance(None, self.root, 'linux_musl_amd64')
        expected['v8_commit'] = '0' * 40
        with self.assertRaisesRegex(ValueError, 'source provenance'):
            source_provenance.validate_manifest_provenance(expected, self.root, 'linux_musl_amd64')

    def test_rejects_boolean_or_float_schema_values(self):
        metadata_path = self.deps / 'source-provenance.json'
        metadata = json.loads(metadata_path.read_text())
        for value in (True, 1.0):
            with self.subTest(value=value):
                metadata['schema'] = value
                metadata_path.write_text(json.dumps(metadata))
                with self.assertRaisesRegex(ValueError, 'schema'):
                    source_provenance.expected_provenance(self.root, 'linux_amd64')
        metadata['schema'] = 1
        metadata_path.write_text(json.dumps(metadata))
        expected = source_provenance.expected_provenance(self.root, 'linux_amd64')
        for value in (True, 1.0):
            with self.subTest(value=value):
                received = dict(expected)
                received['schema'] = value
                with self.assertRaisesRegex(ValueError, 'source provenance'):
                    source_provenance.validate_manifest_provenance(received, self.root, 'linux_amd64')


if __name__ == '__main__':
    unittest.main()
