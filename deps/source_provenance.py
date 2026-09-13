"""Attest the pinned V8 source state used in native SDK archives."""

import ast
import argparse
import hashlib
import json
from pathlib import Path
import subprocess


def _sha256(path):
    return hashlib.sha256(path.read_bytes()).hexdigest()


def _metadata(root):
    metadata = json.loads((root / 'deps/source-provenance.json').read_text())
    if not isinstance(metadata, dict) or type(metadata.get('schema')) is not int or metadata['schema'] != 1:
        raise ValueError('source provenance metadata schema is unsupported')
    if not _is_revision(metadata.get('v8_commit')):
        raise ValueError('source provenance metadata V8 revision is invalid')
    repositories = metadata.get('repositories')
    if not isinstance(repositories, dict) or not repositories:
        raise ValueError('source provenance metadata repositories are invalid')
    for name, revision in repositories.items():
        if not isinstance(name, str) or not name.startswith('v8') or not _is_revision(revision):
            raise ValueError('source provenance metadata repositories are invalid')
    if repositories.get('v8') != metadata['v8_commit']:
        raise ValueError('source provenance metadata V8 pin is inconsistent')
    return metadata


def _is_revision(value):
    return isinstance(value, str) and len(value) == 40 and all(c in '0123456789abcdef' for c in value)


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
    expected = expected_provenance(root, platform)
    if not isinstance(provenance, dict) or type(provenance.get('schema')) is not int \
            or provenance['schema'] != 1 or provenance != expected:
        raise ValueError('archive source provenance does not match this module')


def _git(directory, *args):
    return subprocess.check_output(['git', '-C', str(directory), *args], text=True).strip()


def _pinned_repositories(root, source, platform):
    profile = '.gclient' if platform == 'linux_amd64' else '.gclient-custom'
    entries_path = root / 'deps' / f'{profile}_entries'
    try:
        tree = ast.parse(entries_path.read_text(), filename=str(entries_path))
        assignment = next(node for node in tree.body if isinstance(node, ast.Assign)
                          and any(isinstance(target, ast.Name) and target.id == 'entries'
                                  for target in node.targets))
        entries = ast.literal_eval(assignment.value)
    except (OSError, StopIteration, SyntaxError, ValueError) as error:
        raise ValueError(f'cannot read source provenance revision pins: {error}') from error
    metadata = _metadata(root)
    expected = metadata['repositories']
    repos = {}
    for name, revision in expected.items():
        value = entries.get(name)
        if name == 'v8':
            valid_entry = isinstance(value, str) and '@' not in value
        else:
            valid_entry = isinstance(value, str) and value.rsplit('@', 1)[-1] == revision
        if not valid_entry:
            raise ValueError(f'source provenance revision pins do not match committed metadata: {name}')
        path = source / name.removeprefix('v8').lstrip('/')
        if not path.is_dir():
            raise ValueError(f'source provenance revision pins require repository: {name}')
        try:
            if Path(_git(path, 'rev-parse', '--show-toplevel')).resolve() != path.resolve():
                raise ValueError(f'source provenance revision pins require repository: {name}')
        except subprocess.CalledProcessError as error:
            raise ValueError(f'source provenance revision pins require repository: {name}') from error
        repos[path.resolve()] = revision
    return repos


def _status(repository):
    raw = subprocess.check_output(
        ['git', '-C', str(repository), 'status', '--porcelain=v1', '-z', '--untracked-files=all'])
    for entry in raw.split(b'\0'):
        if entry:
            yield entry[:2].decode(), entry[3:].decode()


def _verified_nested_boundary(repository, relative, repositories):
    """Whether an untracked status record is exactly a selected child checkout."""
    candidate = Path(relative)
    if candidate.is_absolute() or '..' in candidate.parts:
        return False
    child = repository / candidate
    # Match the exact lexical child directory from committed metadata. Resolving
    # first would let an untracked symlink alias a selected child checkout.
    return not child.is_symlink() and child in repositories and child.parent == repository


def _gitlink(root, name):
    fields = _git(root, 'ls-files', '--stage', '--', name).split()
    if len(fields) < 2 or fields[0] != '160000' or not _is_revision(fields[1]):
        raise ValueError(f'source provenance missing root gitlink: {name}')
    return fields[1]


def _verify_root(root, source, platform):
    depot = root / 'deps/depot_tools'
    for name, repository in (('deps/v8', source), ('deps/depot_tools', depot)):
        if not repository.is_dir() or _gitlink(root, name) != _git(repository, 'rev-parse', 'HEAD'):
            raise ValueError(f'source provenance root gitlink does not match checkout: {name}')
    for status, name in _status(root):
        if platform.startswith('linux_musl_') and name == 'deps/v8' and status in {' M', ' m'}:
            continue
        raise ValueError(f'source provenance found unexpected root change: {name}')
    for _, name in _status(depot):
        raise ValueError(f'source provenance found unexpected depot_tools change: {name}')


def attest(root, platform):
    """Inspect the actual pinned checkout and return its verified provenance."""
    root = Path(root)
    source = root / 'deps/v8'
    expected = expected_provenance(root, platform)
    if not source.is_dir():
        raise ValueError('source provenance requires deps/v8')
    repositories = _pinned_repositories(root, source, platform)
    for repository, revision in repositories.items():
        if _git(repository, 'rev-parse', 'HEAD') != revision:
            raise ValueError(f'source provenance revision does not match pin: {repository}')

    allowed = expected['patched_files']
    dirty = set()
    for repository in repositories:
        for status, relative in _status(repository):
            if status == '??' and _verified_nested_boundary(repository, relative, repositories):
                # gclient places selected child repositories in parents that do
                # not track their directories. The child's own iteration below
                # still verifies its revision and rejects any dirty contents.
                continue
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
    _verify_root(root, source, platform)
    return expected


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', required=True)
    parser.add_argument('--root', type=Path, default=Path(__file__).resolve().parent.parent)
    args = parser.parse_args()
    try:
        print(json.dumps(attest(args.root, args.platform), sort_keys=True))
    except (ValueError, OSError, subprocess.CalledProcessError) as error:
        parser.exit(1, f'error: {error}\n')
