import hashlib
import io
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest


SCRIPT = Path(__file__).with_name('native.py')


class InstallTests(unittest.TestCase):
    def setUp(self):
        self.temp = tempfile.TemporaryDirectory()
        self.addCleanup(self.temp.cleanup)
        self.root = Path(self.temp.name)
        self.archive = self.root / 'native.tar.gz'
        self.prefix = self.root / 'installed'

    def make_archive(self, extra=None, version='v0.10.0-rc.1'):
        files = {
            'manifest.json': json.dumps({'release': version, 'v8': '15.2.124.21',
                                         'platform': 'linux_amd64'}).encode(),
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

    def install(self, checksum):
        return subprocess.run([
            sys.executable, str(SCRIPT), 'install', '--release', 'v0.10.0-rc.1',
            '--platform', 'linux_amd64', '--archive', str(self.archive),
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

    def test_rejects_wrong_release(self):
        result = self.install(self.make_archive(version='v0.9.0'))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('release', result.stderr.lower())
        self.assertFalse(self.prefix.exists())


if __name__ == '__main__':
    unittest.main()
