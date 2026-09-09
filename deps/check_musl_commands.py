#!/usr/bin/env python3
"""Reject mixed libc/compiler command graphs before an expensive musl build."""
import argparse
from pathlib import Path
import shlex
import subprocess


def check(commands, build_dir):
    counts = dict(host_cpp=0, target_cpp=0, host_rust=0, target_rust=0)
    for line in commands.splitlines():
        if 'third_party/rust-toolchain/' in line:
            raise ValueError('musl build references bundled Rust host tools')
        tokens = shlex.split(line)
        cpp = any(Path(token).name in ('clang', 'clang++') for token in tokens[:2])
        if not cpp or '-o' not in tokens:
            continue
        output = tokens[tokens.index('-o') + 1]
        host = output.startswith('clang_')
        targets = [token.partition('=')[2] for token in tokens if token.startswith('--target=')]
        if '--target' in tokens:
            targets.append(tokens[tokens.index('--target') + 1])
        suffix = 'linux-gnu' if host else 'linux-musl'
        if not targets or any(not target.endswith(suffix) for target in targets):
            raise ValueError(f'wrong libc target for {output}: {targets}; expected {suffix}')
        counts[('host_' if host else 'target_') + 'cpp'] += 1
    for ninja in Path(build_dir).rglob('*.ninja'):
        for line in ninja.read_text().splitlines():
            host = str(ninja.relative_to(build_dir)).startswith('clang_')
            if not host and line.strip().startswith('rspfile_content = '):
                for flag in ('USE_ALLOCATOR_SHIM', 'HAS_MEMORY_TAGGING'):
                    if f'{flag}=true' in line.split():
                        raise ValueError(f'musl target enables unsupported allocator feature {flag}')
            if not line.startswith('rustflags = '):
                continue
            targets = [word.split('=', 1)[1] for word in line.split() if word.startswith('--target=')]
            host = str(ninja.relative_to(build_dir)).startswith('clang_')
            suffix = 'linux-gnu' if host else 'linux-musl'
            if not targets or any(not target.endswith(suffix) for target in targets):
                raise ValueError(f'wrong Rust libc target in {ninja}: {targets}')
            counts['host_rust' if host else 'target_rust'] += 1
    if not all(counts.values()):
        raise ValueError(f'incomplete musl command graph: {counts}')
    return counts


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('build_dir')
    args = parser.parse_args()
    commands = subprocess.check_output([
        'ninja', '-C', args.build_dir, '-t', 'commands',
        'v8_monolith', 'libc++', 'libc++abi'], text=True)
    print('Verified musl target / glibc host separation:', check(commands, args.build_dir))
