"""Validate sync profiles with depot_tools' parser, not Python exec."""
from pathlib import Path
import sys
import unittest

DEPS = Path(__file__).resolve().parent
sys.path.insert(0, str(DEPS / 'depot_tools'))
import gclient_eval


class GclientConfigTests(unittest.TestCase):
    def test_default_profile_uses_supported_syntax(self):
        config = gclient_eval.ParseLocalConfig((DEPS / '.gclient').read_text(), '.gclient')
        self.assertEqual(config['solutions'][0]['name'], 'v8')

    def test_custom_profile_only_removes_bundled_compilers(self):
        base = gclient_eval.ParseLocalConfig((DEPS / '.gclient').read_text(), '.gclient')
        custom = gclient_eval.ParseLocalConfig((DEPS / '.gclient-custom').read_text(), '.gclient-custom')
        for name in ('v8/third_party/llvm-build/Release+Asserts',
                     'v8/third_party/rust-toolchain'):
            self.assertIsNone(custom['solutions'][0]['custom_deps'].pop(name))
        self.assertEqual(base, custom)
        self.assertEqual(base['solutions'][0]['custom_hooks'],
                         [{'name': 'wasm_spec_tests'}, {'name': 'wasm_js'}])
