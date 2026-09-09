#!/usr/bin/env python3
"""Apply the musl patch only to the exact supported build-file revisions."""
import hashlib
import json
from pathlib import Path
import subprocess


def apply(root, patches):
    manifest = json.loads((patches / 'linux-musl.json').read_text())
    states = set()
    for name, hashes in manifest.items():
        digest = hashlib.sha256((root / name).read_bytes()).hexdigest()
        if digest == hashes['before']:
            states.add('before')
        elif digest == hashes['after']:
            states.add('after')
        else:
            raise ValueError(f'musl patch does not match {name}; review it for this V8 revision')
    if states == {'after'}:
        return
    if states != {'before'}:
        raise ValueError('musl patch is partially applied; restore the build checkout first')
    patch = str((patches / 'linux-musl.patch').resolve())
    subprocess.run(['git', 'apply', '--check', patch], cwd=root, check=True)
    subprocess.run(['git', 'apply', patch], cwd=root, check=True)


if __name__ == '__main__':
    deps = Path(__file__).resolve().parent
    apply(deps / 'v8', deps / 'patches')
