"""Attest the pinned V8 source state used in native SDK archives."""

import ast
import hashlib
import json
from pathlib import Path
import subprocess


def _sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _metadata(root):
    return json.loads((root / 'deps/source-provenance.json').read_text())


def _patch_manifest(root):
    patches = root / 'deps/patches'
    return json.loads((patches / 'linux-musl.json').read_text())


def expected_provenance(root, platform):
    """Return the only archive provenance accepted for *platform*.

    This reads committed module files only, so consumers can verify an archive
    after `go mod download`, where neither the V8 checkout nor Git metadata is
    present.
    """
    root = Path(root)
    metadata = _metadata(root)
    if metadata.get('schema') != 1:
        raise ValueError('source provenance metadata schema is unsupported')
    provenance = {
        'schema': 1,
        'state': 'clean',
        'v8_commit': metadata['v8_commit'],
        'patch_sha256': None,
        'patch_manifest_sha256': None,
        'patched_files': {},
    }
    if platform.startswith('linux_musl_'):
        patches = root / 'deps/patches'
        manifest = _patch_manifest(root)
        provenance.update({
            'state': 'verified-musl-patch',
            'patch_sha256': _sha256(patches / 'linux-musl.patch'),
            'patch_manifest_sha256': _sha256(patches / 'linux-musl.json'),
            'patched_files': {name: hashes['after'] for name, hashes in sorted(manifest.items())},
        })
    return provenance


def validate_manifest_provenance(provenance, root, platform):
    """Fail closed unless an archive carries the exact local expected attestation."""
    if provenance != expected_provenance(root, platform):
        raise ValueError('archive source provenance does not match this module')


def _git(directory, *args):
    return subprocess.check_output(['git', '-C', str(directory), *args], text=True).strip()


def _pinned_repositories(root, source):
    entries_path = root / 'deps/.gclient_entries'
    try:
        tree = ast.parse(entries_path.read_text(), filename=str(entries_path))
        assignment = next(node for node in tree.body if isinstance(node, ast.Assign)
                          and any(isinstance(target, ast.Name) and target.id == 'entries'
                                  for target in node.targets))
        entries = ast.literal_eval(assignment.value)
    except (OSError, StopIteration, SyntaxError, ValueError) as error:
        raise ValueError(f'cannot read source provenance revision pins: {error}') from error
    repos = {}
    for name, value in entries.items():
        if not name.startswith('v8') or ':' in name or not isinstance(value, str) or '@' not in value:
            continue
        revision = value.rsplit('@', 1)[1]
        path = source / name.removeprefix('v8').lstrip('/')
        if len(revision) == 40 and all(c in '0123456789abcdef' for c in revision) and path.exists():
            try:
                _git(path, 'rev-parse', '--show-toplevel')
            except subprocess.CalledProcessError:
                continue
            repos[path.resolve()] = revision
    repos[source.resolve()] = _metadata(root)['v8_commit']
    return repos


def _status(repository):
    raw = subprocess.check_output(
        ['git', '-C', str(repository), 'status', '--porcelain=v1', '-z', '--untracked-files=all'])
    for entry in raw.split(b'\0'):
        if entry:
            yield entry[:2].decode(), entry[3:].decode()


def attest(root, platform):
    """Inspect the actual pinned checkout and return its verified provenance."""
    root = Path(root)
    source = root / 'deps/v8'
    expected = expected_provenance(root, platform)
    if not source.is_dir():
        raise ValueError('source provenance requires deps/v8')
    repositories = _pinned_repositories(root, source)
    for repository, revision in repositories.items():
        if _git(repository, 'rev-parse', 'HEAD') != revision:
            raise ValueError(f'source provenance revision does not match pin: {repository}')

    allowed = expected['patched_files']
    dirty = set()
    for repository in repositories:
        for status, relative in _status(repository):
            absolute = repository / relative
            try:
                name = str(absolute.resolve().relative_to(source.resolve()))
            except ValueError:
                raise ValueError(f'source provenance found unexpected source change: {relative}')
            if platform.startswith('linux_musl_') and status == ' M' and name in allowed:
                dirty.add(name)
                continue
            raise ValueError(f'source provenance found unexpected source change: {name}')

    if platform.startswith('linux_musl_'):
        if dirty != set(allowed):
            raise ValueError('source provenance requires the complete musl patch')
        for name, digest in allowed.items():
            if _sha256(source / name) != digest:
                raise ValueError(f'source provenance hash does not match approved musl patch: {name}')
    elif dirty:
        raise ValueError('source provenance requires a clean source')
    return expected
