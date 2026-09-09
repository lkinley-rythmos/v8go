#!/usr/bin/env python3
"""Package or install the native Linux dependencies for a v8go release."""

import argparse
import hashlib
import json
import os
from pathlib import Path, PurePosixPath
import platform
import re
import shlex
import shutil
import subprocess
import tarfile
import tempfile
import urllib.request


ROOT = Path(__file__).resolve().parent.parent
PLATFORMS = {'linux_amd64': ('release', 'x64'),
             'linux_arm64': ('release-arm64', 'arm64'),
             'linux_musl_amd64': ('release-musl', 'x64'),
             'linux_musl_arm64': ('release-arm64-musl', 'arm64')}


def host_platform():
    arch = {'x86_64': 'amd64', 'amd64': 'amd64',
            'aarch64': 'arm64', 'arm64': 'arm64'}.get(platform.machine().lower())
    if platform.system() != 'Linux' or arch is None:
        raise ValueError('supported platforms are Linux amd64/arm64 with glibc or musl; use --platform for cross-target installation')
    libc = platform.libc_ver()[0]
    if libc == 'glibc':
        return 'linux_' + arch
    if libc == 'musl' or any(Path('/lib').glob('ld-musl-*.so.1')):
        return 'linux_musl_' + arch
    raise ValueError('could not detect Linux libc; specify --platform explicitly')


def digest(path):
    result = hashlib.sha256()
    with path.open('rb') as stream:
        for chunk in iter(lambda: stream.read(1024 * 1024), b''):
            result.update(chunk)
    return result.hexdigest()


def asset_name(release, target):
    if not re.fullmatch(r'v\d+\.\d+\.\d+(?:-[A-Za-z0-9.-]+)?', release):
        raise ValueError('invalid release tag')
    return f'v8go_{release}_{target}.tar.gz'


def rust_archives(build, target):
    tokens = shlex.split((build / 'obj/v8_monolith.ninja').read_text())
    archives = list(dict.fromkeys(p for p in tokens if p.endswith('.rlib')))
    # With a custom Rust compiler, GN supplies prebuilt stdlibs through ldflags.
    # Static-library metadata drops those flags, so include the copied TARGET
    # stdlib explicitly. Host toolchain directories must never enter the package.
    if 'phony/build/rust/std/prebuilt_rustc_copy_to_sysroot' in tokens:
        cpu = 'aarch64' if target.endswith('_arm64') else 'x86_64'
        libc = 'musl' if target.startswith('linux_musl_') else 'gnu'
        stdlib = build / f'prebuilt_rustc_sysroot/lib/rustlib/{cpu}-unknown-linux-{libc}/lib'
        if not all((stdlib / name).is_file() for name in ('libstd.rlib', 'libcore.rlib', 'liballoc.rlib')):
            raise ValueError(f'missing target prebuilt Rust standard library: {stdlib}')
        archives += [str(path.relative_to(build)) for path in sorted(stdlib.glob('*.rlib'))]
    if not archives:
        raise ValueError('no Rust dependencies found in v8_monolith build metadata')
    return list(dict.fromkeys(archives))


def package(args):
    v8 = ROOT / 'deps/v8'
    build_name, _ = PLATFORMS[args.platform]
    build = v8 / 'out' / build_name
    ar = Path(os.environ['V8_LLVM_AR']) if 'V8_LLVM_AR' in os.environ else (
        v8 / 'third_party/llvm-build/Release+Asserts/bin/llvm-ar')
    output = args.output.resolve()
    output.mkdir(parents=True, exist_ok=True)
    archive = output / asset_name(args.release, args.platform)
    # GN lists Rust archives separately from v8_monolith. Include its entire
    # transitive Rust link closure, together with the matching custom libc++.
    rust = rust_archives(build, args.platform)
    libraries = [build / 'obj/libv8_monolith.a',
                 build / 'obj/buildtools/third_party/libc++/libc++.a',
                 build / 'obj/buildtools/third_party/libc++abi/libc++abi.a']
    libraries += [build / p for p in rust]
    with tempfile.TemporaryDirectory(dir=output) as temp:
        stage = Path(temp)
        (stage / 'lib').mkdir()
        library = stage / 'lib/libv8.a'
        if any(any(c.isspace() for c in str(p)) for p in libraries + [library]):
            raise ValueError('build paths must not contain whitespace (ar MRI format)')
        script = f'create {library}\n'
        script += ''.join(f'addlib {p}\n' for p in libraries)
        script += 'save\nend\n'
        subprocess.run([str(ar), '-M'], input=script, text=True, check=True)
        # Rust compiler metadata is not needed by a native linker.
        # llvm-ar deletes only the first occurrence of each duplicate name.
        for _ in rust:
            subprocess.run([str(ar), 'd', str(library), 'lib.rmeta', 'lib.rmeta-link'], check=True)
        for source, dest in [
            (v8 / 'third_party/libc++/src/include', 'libcxx'),
            (v8 / 'third_party/libc++abi/src/include', 'libcxxabi'),
            (v8 / 'buildtools/third_party/libc++', 'libcxx-config'),
            (ROOT / 'deps/include', 'v8'),
        ]:
            shutil.copytree(source, stage / 'include' / dest)
        # Preserve notices from V8 and its synchronized third-party sources.
        for tree in [v8, v8 / 'third_party', v8 / 'buildtools']:
            for directory, dirs, files in os.walk(tree):
                dirs[:] = [d for d in dirs if d not in {'.git', 'out', 'node_modules', '__pycache__'}]
                if tree == v8:
                    dirs[:] = []
                for name in files:
                    if name.upper().startswith(('LICENSE', 'COPYING', 'COPYRIGHT', 'NOTICE')) or name == 'README.chromium':
                        source = Path(directory) / name
                        dest = stage / 'licenses' / source.relative_to(v8)
                        dest.parent.mkdir(parents=True, exist_ok=True)
                        shutil.copyfile(source, dest)
        shutil.copyfile(ROOT / 'LICENSE', stage / 'licenses/LICENSE.v8go')
        manifest = {
            'release': args.release, 'platform': args.platform,
            'v8': (ROOT / 'deps/VERSION').read_text().strip(),
            'v8_commit': subprocess.check_output(['git', '-C', str(v8), 'rev-parse', 'HEAD'], text=True).strip(),
            'v8go_commit': subprocess.check_output(['git', '-C', str(ROOT), 'rev-parse', 'HEAD'], text=True).strip(),
            'v8go_dirty': bool(subprocess.check_output(['git', '-C', str(ROOT), 'status', '--porcelain'])),
            'library_sha256': digest(library),
            'rust_archives': rust,
        }
        (stage / 'manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
        shutil.copyfile(build / 'args.gn', stage / 'args.gn')
        with tarfile.open(archive, 'w:gz') as tar:
            for path in sorted(stage.iterdir()):
                tar.add(path, arcname=path.name)
    checksum = digest(archive)
    archive.with_name(archive.name + '.sha256').write_text(f'{checksum}  {archive.name}\n')
    print(archive)


def environment(prefix, target='linux_amd64'):
    # cgo parses its own flags after the shell, so reject whitespace rather than
    # generating flags whose meaning changes at the second parsing boundary.
    if any(c.isspace() for c in str(prefix)):
        raise ValueError('installation prefix must not contain whitespace')
    cxx = (f'-nostdinc++ -isystem{prefix}/include/libcxx '
           f'-isystem{prefix}/include/libcxxabi -I{prefix}/include/libcxx-config '
           '-D_LIBCPP_HARDENING_MODE=_LIBCPP_HARDENING_MODE_EXTENSIVE '
           '-D_LIBCPP_DISABLE_VISIBILITY_ANNOTATIONS')
    if target.startswith('linux_musl_'):
        cxx += ' -DV8GO_USE_MUSL'
    return ('# Source this file before building a v8go application.\n'
            'export CC="${CC:-clang-22}"\n'
            'export CXX="${CXX:-clang++-22}"\n'
            f'export CGO_CXXFLAGS={shlex.quote(cxx)}" ${{CGO_CXXFLAGS:-}}"\n'
            f'export CGO_LDFLAGS={shlex.quote(f"-L{prefix}/lib -fuse-ld=lld")}" ${{CGO_LDFLAGS:-}}"\n'
            'export CGO_ENABLED=1\n')


def install(args):
    name = asset_name(args.release, args.platform)
    prefix = args.prefix.expanduser().resolve()
    env = environment(prefix, args.platform)
    if prefix.exists():
        raise ValueError(f'installation prefix already exists: {prefix}')
    prefix.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(dir=prefix.parent) as temp:
        temp = Path(temp)
        archive = args.archive
        checksum = args.sha256
        if archive is None:
            base = f'https://github.com/lkinley-rythmos/v8go/releases/download/{args.release}'
            with urllib.request.urlopen(f'{base}/{name}.sha256', timeout=60) as response:
                checksum = response.read().decode().split()[0]
            archive = temp / name
            with urllib.request.urlopen(f'{base}/{name}', timeout=60) as response, archive.open('wb') as dest:
                shutil.copyfileobj(response, dest)
        if not checksum or digest(archive) != checksum:
            raise ValueError('archive checksum mismatch (local archives require --sha256)')
        stage = temp / 'stage'
        stage.mkdir()
        with tarfile.open(archive) as tar:
            members = tar.getmembers()
            for member in members:
                path = PurePosixPath(member.name)
                if path.is_absolute() or '..' in path.parts or not (member.isfile() or member.isdir()):
                    raise ValueError(f'unsafe archive member: {member.name}')
            # Only regular files/directories are accepted, including on Python 3.11.
            for member in members:
                dest = stage / member.name
                if member.isdir():
                    dest.mkdir(parents=True, exist_ok=True)
                else:
                    dest.parent.mkdir(parents=True, exist_ok=True)
                    with tar.extractfile(member) as src, dest.open('wb') as output:
                        shutil.copyfileobj(src, output)
        manifest = json.loads((stage / 'manifest.json').read_text())
        if manifest['release'] != args.release or manifest['platform'] != args.platform:
            raise ValueError('archive release/platform does not match the requested installation')
        if manifest['v8'] != (ROOT / 'deps/VERSION').read_text().strip():
            raise ValueError('archive V8 version does not match this module')
        if not (stage / 'lib/libv8.a').is_file():
            raise ValueError('archive is missing lib/libv8.a')
        (stage / 'env.sh').write_text(env)
        stage.rename(prefix)
    print(f'Installed {args.release} for {args.platform}. Run:')
    print(f'. {shlex.quote(str(prefix / "env.sh"))}')


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    commands = parser.add_subparsers(dest='command', required=True)
    for name in ['package', 'install']:
        command = commands.add_parser(name)
        command.add_argument('--release', required=True)
        command.add_argument('--platform', choices=PLATFORMS)
        if name == 'package':
            command.add_argument('--output', type=Path, default=ROOT / '.build/dist')
        else:
            command.add_argument('--archive', type=Path)
            command.add_argument('--sha256')
            command.add_argument('--prefix', type=Path, required=True)
    args = parser.parse_args()
    try:
        if args.platform is None:
            args.platform = host_platform()
        (package if args.command == 'package' else install)(args)
    except (ValueError, OSError, KeyError, tarfile.TarError, subprocess.CalledProcessError) as error:
        parser.exit(1, f'error: {error}\n')


if __name__ == '__main__':
    main()
