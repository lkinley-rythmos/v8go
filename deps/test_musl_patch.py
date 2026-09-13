import difflib
import hashlib
import json
from pathlib import Path
import tempfile
import unittest

from apply_musl_patch import apply


class MuslPatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.patches = self.root / 'patches'
        self.patches.mkdir()
        before, after = 'original\n', 'musl\n'
        (self.root / 'config').write_text(before)
        (self.patches / 'linux-musl.json').write_text(json.dumps({'config': {
            'before': hashlib.sha256(before.encode()).hexdigest(),
            'after': hashlib.sha256(after.encode()).hexdigest(),
        }}))
        (self.patches / 'linux-musl.patch').write_text(''.join(difflib.unified_diff(
            before.splitlines(True), after.splitlines(True),
            fromfile='a/config', tofile='b/config')))

    def test_applies_once_and_accepts_repeat_builds(self):
        apply(self.root, self.patches)
        apply(self.root, self.patches)
        self.assertEqual((self.root / 'config').read_text(), 'musl\n')

    def test_rejects_changed_upstream_file_without_overwriting_it(self):
        (self.root / 'config').write_text('new upstream version\n')
        with self.assertRaisesRegex(ValueError, 'does not match'):
            apply(self.root, self.patches)
        self.assertEqual((self.root / 'config').read_text(), 'new upstream version\n')


class ReservedStackPatchTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.patches = self.root / 'patches'
        self.patches.mkdir()
        deps = Path(__file__).resolve().parent
        self.name = 'src/base/platform/platform.cc'
        self.source = self.root / self.name
        self.source.parent.mkdir(parents=True)
        self.before = (deps / 'testdata/platform.cc').read_bytes()
        self.source.write_bytes(self.before)
        shipped = json.loads((deps / 'patches/linux-musl.json').read_text())
        self.assertIn(self.name, shipped, 'reserved-stack source must be hash-verified')
        self.hashes = shipped[self.name]
        self.assertEqual(hashlib.sha256(self.before).hexdigest(), self.hashes['before'])
        (self.patches / 'linux-musl.json').write_text(json.dumps({self.name: self.hashes}))
        sections = (deps / 'patches/linux-musl.patch').read_text().split('--- a/')
        selected = [section for section in sections
                    if section.startswith(self.name + '\n')]
        self.assertEqual(len(selected), 1)
        (self.patches / 'linux-musl.patch').write_text('--- a/' + selected[0])

    def test_applies_real_reserved_stack_patch_and_accepts_repeat(self):
        apply(self.root, self.patches)
        self.assertNotEqual(self.source.read_bytes(), self.before)
        self.assertEqual(hashlib.sha256(self.source.read_bytes()).hexdigest(),
                         self.hashes['after'])
        apply(self.root, self.patches)
        self.assertEqual(hashlib.sha256(self.source.read_bytes()).hexdigest(),
                         self.hashes['after'])

    def test_rejects_changed_reserved_stack_source_without_overwriting(self):
        changed = self.before + b'// upstream changed\n'
        self.source.write_bytes(changed)
        with self.assertRaisesRegex(ValueError, 'does not match.*platform.cc'):
            apply(self.root, self.patches)
        self.assertEqual(self.source.read_bytes(), changed)

    def test_rejects_partial_patch_without_changing_reserved_stack_source(self):
        manifest = json.loads((self.patches / 'linux-musl.json').read_text())
        manifest['config'] = {
            'before': hashlib.sha256(b'before\n').hexdigest(),
            'after': hashlib.sha256(b'after\n').hexdigest(),
        }
        (self.patches / 'linux-musl.json').write_text(json.dumps(manifest))
        (self.root / 'config').write_bytes(b'after\n')
        with self.assertRaisesRegex(ValueError, 'partially applied'):
            apply(self.root, self.patches)
        self.assertEqual(self.source.read_bytes(), self.before)
