#!/usr/bin/env python3
"""Fingerprint native build inputs without downloading the V8 submodules."""
import argparse
import hashlib
from pathlib import Path
import subprocess

PLATFORMS = ('linux_amd64', 'linux_arm64', 'linux_musl_amd64', 'linux_musl_arm64')


def gitlink(root, name):
    entry = subprocess.check_output(['git', 'ls-files', '--stage', '--', name],
                                    cwd=root, text=True).split()
    if not entry or entry[0] != '160000':
        raise ValueError(f'missing submodule pin: {name}')
    return entry[1]


def hash_inputs(root, paths, extra):
    digest = hashlib.sha256()
    for value in extra:
        digest.update(value.encode() + b'\0')
    for name in sorted(paths):
        path = root / name
        files = sorted(p for p in path.rglob('*') if p.is_file()) if path.is_dir() else [path]
        for file in files:
            digest.update(str(file.relative_to(root)).encode() + b'\0')
            digest.update(file.read_bytes() + b'\0')
    return digest.hexdigest()


def fingerprints(root, platform):
    if platform not in PLATFORMS:
        raise ValueError(f'unsupported platform: {platform}')
    arch = 'arm64' if platform.endswith('arm64') else 'amd64'
    compiler = 'bundled' if platform == 'linux_amd64' else 'custom'
    pins = [gitlink(root, name) for name in ('deps/v8', 'deps/depot_tools')]
    source = hash_inputs(root, ['deps/VERSION', 'deps/.gclient', 'deps/v8_download.sh'], pins)
    toolchain = hash_inputs(root, ['deps/setup-linux-toolchains.sh', 'deps/rust-toolchain.json',
                                  'deps/llvm-version'], [])
    sdk = hash_inputs(root, [
        'VERSION', 'LICENSE', 'deps/ci_cache.py', 'deps/native.py', 'deps/include',
        'deps/args/linux.gn', 'deps/v8_compile.sh', 'deps/patches',
        'deps/apply_musl_patch.py', 'deps/check_musl_commands.py',
        'deps/setup-musl-sysroot.sh', 'deps/Dockerfile.musl-sysroot',
        '.github/actions/build-linux/action.yml',
    ], [source, toolchain, platform])
    # Source trees contain host executables. Custom ARM64 sources/tools are shared
    # by glibc and musl; the SDK and compilation caches always distinguish libc.
    return {
        'source': f'v8-source-v1-Linux-{arch}-{compiler}-{source}',
        'toolchain': f'v8-tools-v1-Linux-{arch}-{toolchain}',
        'sdk': f'v8-sdk-v1-Linux-{platform}-{sdk}',
        'compiler': compiler,
    }


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--platform', required=True, choices=PLATFORMS)
    args = parser.parse_args()
    for key, value in fingerprints(Path(__file__).resolve().parent.parent, args.platform).items():
        print(f'{key}={value}')
