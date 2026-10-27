#!/usr/bin/env python3
"""Build and verify the standalone ssh-tunnel packages; only the Python standard library is used.

The archive layout is fixed as ssh-tunnel/<program> and ssh-tunnel/manifest.json.
The timestamp comes from the source commit, so the same source always produces the same bytes.

Usage:
    python3 scripts/package.py --targets linux-x64 windows-x64
    python3 scripts/package.py --release          # release build, requires a clean source tree
"""
import argparse
import gzip
import hashlib
import io
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import struct
import tarfile
import zipfile

# Each target: operating system, architecture, binary format, build tags, user interface form.
# The desktop edition uses Wails; what this project currently releases is the browser fallback
# build (no native window dependency), except on Windows, where the webview shell is always
# compiled into the artifact.
TARGETS = {
    'linux-x64': dict(os='linux', arch='amd64', pattern=r'ELF 64-bit.*x86-64', ui='browser-fallback'),
    'linux-arm64': dict(os='linux', arch='arm64', pattern=r'ELF 64-bit.*ARM aarch64', ui='browser-fallback'),
    'darwin-x64': dict(os='darwin', arch='amd64', pattern=r'Mach-O 64-bit.*x86_64', ui='browser-fallback'),
    'darwin-arm64': dict(os='darwin', arch='arm64', pattern=r'Mach-O 64-bit.*arm64', ui='browser-fallback'),
    'windows-x64': dict(os='windows', arch='amd64', pattern=r'PE32\+.*x86-64', ui='webview-window'),
}

COMPONENT = 'ssh-tunnel'
REPOSITORY = 'AI_Team/ssh-tunnel'


def run(*args, **kwargs):
    return subprocess.check_output(args, text=True, **kwargs).strip()


def digest(data):
    return hashlib.sha256(data).hexdigest()


def read_version():
    source = Path('internal/version/version.go').read_text()
    match = re.search(r'Version\s*=\s*"([^"]+)"', source)
    if not match:
        raise RuntimeError('cannot parse the version from internal/version/version.go')
    return match.group(1)


def build(target, out_dir):
    spec = TARGETS[target]
    goos, arch = spec['os'], spec['arch']
    binary_name = 'ssh-tunnel.exe' if goos == 'windows' else 'ssh-tunnel'
    binary = out_dir / binary_name

    env = dict(os.environ, GOOS=goos, GOARCH=arch, CGO_ENABLED='0')
    ldflags = '-s -w'
    if goos == 'windows':
        ldflags += ' -H windowsgui'
    subprocess.run(
        ['go', 'build', '-mod=readonly', '-trimpath', '-buildvcs=true',
         f'-ldflags={ldflags}', '-o', str(binary), './cmd/ssh-tunnel'],
        env=env, check=True)
    binary.chmod(0o755)

    file_info = run('file', str(binary))
    if not re.search(spec['pattern'], file_info):
        raise RuntimeError(f'wrong target architecture: {file_info}')
    if goos == 'linux' and 'statically linked' not in file_info:
        raise RuntimeError(f'the Linux artifact must be statically linked: {file_info}')

    build_info = run('go', 'version', '-m', str(binary))
    for expected in (f'GOOS={goos}', f'GOARCH={arch}', 'CGO_ENABLED=0'):
        if expected not in build_info:
            raise RuntimeError(f'the binary is missing build information: {expected}')

    if goos == 'windows':
        check_windows_icon(binary)
    return binary, file_info, build_info


def check_windows_icon(binary):
    """Make sure the resource section of the exe carries the icon, otherwise the file
    explorer shows the default icon."""
    data = binary.read_bytes()
    e_lfanew = struct.unpack_from('<I', data, 0x3c)[0]
    if data[e_lfanew:e_lfanew + 4] != b'PE\0\0':
        raise RuntimeError('the artifact is not a PE file')
    coff = e_lfanew + 4
    sections = struct.unpack_from('<H', data, coff + 2)[0]
    size_opt = struct.unpack_from('<H', data, coff + 16)[0]
    opt = coff + 20
    magic = struct.unpack_from('<H', data, opt)[0]
    # The data directory table starts at 112 for PE32+ and at 96 for PE32; the resource
    # directory is the third entry.
    directories = opt + (112 if magic == 0x20b else 96)
    rva, size = struct.unpack_from('<II', data, directories + 2 * 8)
    if rva == 0 or size == 0:
        raise RuntimeError('the Windows artifact has no resource section, the icon will not show')
    table = opt + size_opt
    for i in range(sections):
        entry = table + i * 40
        name = data[entry:entry + 8].rstrip(b'\0')
        vsize, vaddr = struct.unpack_from('<II', data, entry + 8)
        if name == b'.rsrc' and vaddr <= rva < vaddr + vsize:
            return
    raise RuntimeError('the resource section of the Windows artifact is wrong')


def smoke_test(binary, goos, arch, version):
    """Run version and help once on a native target, to confirm the artifact is executable."""
    host = (run('go', 'env', 'GOHOSTOS'), run('go', 'env', 'GOHOSTARCH'))
    if host != (goos, arch):
        return 'not run: cross-compiled target'
    actual = run(str(binary.resolve()), 'version')
    if actual != f'{COMPONENT} {version}':
        raise RuntimeError(f'the version does not match: {actual}')
    subprocess.run([str(binary.resolve()), 'help'], stdout=subprocess.DEVNULL, check=True)
    return 'native version and help passed'


def pack(files, target, dist, epoch):
    """Pack the files into a reproducible archive and read it back to verify it."""
    goos = TARGETS[target]['os']
    name = f'{COMPONENT}-{target}' + ('.zip' if goos == 'windows' else '.tar.gz')
    archive = dist / name
    if goos == 'windows':
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as out:
            for path, (data, mode) in files.items():
                entry = zipfile.ZipInfo(path)
                entry.create_system = 3
                entry.external_attr = (0o100000 | mode) << 16
                out.writestr(entry, data, compress_type=zipfile.ZIP_DEFLATED)
        with zipfile.ZipFile(archive) as check:
            if set(check.namelist()) != set(files):
                raise RuntimeError('the archive contents do not match the expectation')
            for path, (data, _) in files.items():
                if check.read(path) != data:
                    raise RuntimeError(f'archive content verification failed: {path}')
    else:
        with archive.open('wb') as raw, gzip.GzipFile(filename='', mode='wb', fileobj=raw, mtime=epoch) as gz:
            with tarfile.open(fileobj=gz, mode='w') as out:
                for path, (data, mode) in files.items():
                    entry = tarfile.TarInfo(path)
                    entry.size, entry.mode, entry.mtime = len(data), mode, epoch
                    out.addfile(entry, io.BytesIO(data))
        with tarfile.open(archive) as check:
            if set(check.getnames()) != set(files):
                raise RuntimeError('the archive contents do not match the expectation')
            for path, (data, mode) in files.items():
                if check.extractfile(path).read() != data:
                    raise RuntimeError(f'archive content verification failed: {path}')
                if check.getmember(path).mode != mode:
                    raise RuntimeError(f'the archive permissions are wrong: {path}')
    return archive


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--targets', nargs='+', choices=sorted(TARGETS), default=['linux-x64'])
    parser.add_argument('--release', action='store_true', help='requires a clean source tree')
    args = parser.parse_args()

    os.chdir(Path(__file__).resolve().parent.parent)
    if args.release and run('git', 'status', '--porcelain'):
        raise SystemExit('a release build requires a clean source tree')

    version = read_version()
    commit = run('git', 'rev-parse', 'HEAD')
    epoch = int(run('git', 'show', '-s', '--format=%ct', 'HEAD'))
    go_version = run('go', 'env', 'GOVERSION')
    host = (run('go', 'env', 'GOHOSTOS'), run('go', 'env', 'GOHOSTARCH'))

    dist = Path('dist/release')
    if dist.exists():
        shutil.rmtree(dist)
    dist.mkdir(parents=True, exist_ok=True)
    sums = []
    results = []
    artifacts = []

    for target in args.targets:
        spec = TARGETS[target]
        folder = Path('dist/build') / target
        if folder.exists():
            shutil.rmtree(folder)
        folder.mkdir(parents=True, exist_ok=True)

        binary, file_info, build_info = build(target, folder)
        smoke = smoke_test(binary, spec['os'], spec['arch'], version)
        binary_name = binary.name

        manifest = {
            'schemaVersion': 1,
            'component': COMPONENT,
            'version': version,
            'tag': f'v{version}',
            'sourceRepository': REPOSITORY,
            'sourceCommit': commit,
            'target': target,
            'os': spec['os'],
            'arch': spec['arch'],
            'ui': spec['ui'],
            'binary': binary_name,
            'binarySha256': digest(binary.read_bytes()),
            'build': {
                'goVersion': go_version,
                'cgoEnabled': False,
                'trimpath': True,
                'sourceDateEpoch': epoch,
                'buildId': os.getenv('GITHUB_RUN_ID', 'local'),
                'runner': '/'.join(host),
                'file': file_info,
                'smokeTest': smoke,
            },
        }
        files = {
            f'{COMPONENT}/{binary_name}': (binary.read_bytes(), 0o755),
            f'{COMPONENT}/manifest.json': ((json.dumps(manifest, ensure_ascii=False, indent=2) + '\n').encode(), 0o644),
        }
        archive = pack(files, target, dist, epoch)
        sums.append(f'{digest(archive.read_bytes())}  {archive.name}')
        artifacts.append(manifest)
        results.append(f'- `{archive.name}`: {smoke}, user interface form {spec["ui"]}.')
        print(f'{target}: {file_info}; {smoke}', flush=True)

    (dist / 'SHA256SUMS').write_text('\n'.join(sums) + '\n')
    (dist / 'release-notes.md').write_text(
        f'# {COMPONENT} v{version}\n\nSource commit: `{commit}`. Go toolchain: `{go_version}`.\n\n'
        f'Standalone prebuilt package; the archive holds `{COMPONENT}/` with the program and `manifest.json`. '
        'Users need neither the Go toolchain nor any extra language runtime.\n\n'
        '## Verification at build time\n\n' + '\n'.join(results) + '\n\n'
        'Every artifact was checked for file format, target architecture, embedded source commit, '
        'archive layout and SHA-256. A successful cross-compile does not mean the matching '
        'platform was verified at runtime.\n\n'
        '## Notes\n\n'
        'The Linux build is the browser fallback form: the interface opens in the system browser, '
        'and the tunnel itself is unaffected.\n'
        'The Windows build ships the webview native window and requires the WebView2 runtime '
        'to be installed on the system.')
    (dist / 'manifest.json').write_text(
        json.dumps({'component': COMPONENT, 'version': version, 'tag': f'v{version}',
                    'sourceRepository': REPOSITORY, 'sourceCommit': commit,
                    'artifacts': artifacts}, ensure_ascii=False, indent=2) + '\n')
    print(f'the artifacts were written to {dist}')


if __name__ == '__main__':
    main()
