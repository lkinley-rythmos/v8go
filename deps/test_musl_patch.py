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
