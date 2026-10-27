#!/usr/bin/env python3
"""Publish a fixed version on Gitea: create a draft, read every attachment back and
verify it, then make the release public. An existing version must never be overwritten.
"""
import hashlib
import json
import os
from pathlib import Path
import subprocess
import tarfile
import urllib.error
import urllib.parse
import urllib.request

COMPONENT = 'ssh-tunnel'
# A release must cover all three platforms.
EXPECTED_TARGETS = ('linux-x64', 'linux-arm64', 'windows-x64')


def main():
    base = os.environ['GITEA_SERVER_URL'].rstrip('/')
    repo = os.environ['GITEA_REPOSITORY']
    token = os.environ['GITEA_TOKEN']
    root = f'{base}/api/v1/repos/{repo}'

    def request(url, method='GET', data=None, content_type='application/json'):
        headers = {'Authorization': f'token {token}', 'Content-Type': content_type}
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        with urllib.request.urlopen(req, timeout=120) as response:
            return response.read()

    def absent(url):
        try:
            request(url)
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return
            raise
        raise RuntimeError('the version already exists; overwriting is forbidden, '
                           'read it back first to confirm it is not an unfinished release')

    dist = Path('dist/release')
    # The version and the source commit come from the archive manifest, never from
    # an intermediate state elsewhere.
    with tarfile.open(dist / f'{COMPONENT}-linux-x64.tar.gz') as archive:
        manifest = json.load(archive.extractfile(f'{COMPONENT}/manifest.json'))
    tag, commit = manifest['tag'], manifest['sourceCommit']
    if subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() != commit:
        raise RuntimeError('the source commit of the artifacts differs from the current commit')
    absent(f'{root}/releases/tags/{tag}')
    absent(f'{root}/tags/{tag}')

    sums = {}
    for line in (dist / 'SHA256SUMS').read_text().splitlines():
        checksum, name = line.split('  ', 1)
        sums[name] = checksum
    expected = {f'{COMPONENT}-{t}.tar.gz' for t in EXPECTED_TARGETS if not t.startswith('windows')}
    expected.add(f'{COMPONENT}-windows-x64.zip')
    if set(sums) != expected:
        raise RuntimeError(f'a release must contain exactly {sorted(expected)}')

    for name in sorted(expected):
        if not (dist / name).is_file():
            raise RuntimeError(f'missing artifact file: {name}')

    files = [dist / name for name in sorted(expected)] + [dist / 'SHA256SUMS']
    for path in files:
        if path.name in sums and hashlib.sha256(path.read_bytes()).hexdigest() != sums[path.name]:
            raise RuntimeError(f'local artifact digest mismatch: {path.name}')

    body = {'tag_name': tag, 'target_commitish': commit, 'name': f'{COMPONENT} {tag}',
            'body': (dist / 'release-notes.md').read_text(), 'draft': True, 'prerelease': False}
    release = json.loads(request(root + '/releases', 'POST', json.dumps(body, ensure_ascii=True).encode()))
    release_url = f"{root}/releases/{release['id']}"
    for path in files:
        boundary = f'{COMPONENT}-release-attachment'
        data = (f'--{boundary}\r\nContent-Disposition: form-data; name="attachment"; filename="{path.name}"\r\n'
                'Content-Type: application/octet-stream\r\n\r\n').encode() + path.read_bytes() + f'\r\n--{boundary}--\r\n'.encode()
        request(release_url + '/assets', 'POST', data, f'multipart/form-data; boundary={boundary}')

    assets = json.loads(request(release_url + '/assets'))
    if {asset['name'] for asset in assets} != {path.name for path in files}:
        raise RuntimeError('the remote attachments are incomplete; the draft is kept for inspection')
    for asset in assets:
        # Use the current instance, so the server address stays reachable from the runner.
        path = urllib.parse.urlsplit(asset['browser_download_url']).path
        actual = request(base + path)
        expected_hash = hashlib.sha256((dist / asset['name']).read_bytes()).hexdigest()
        if hashlib.sha256(actual).hexdigest() != expected_hash:
            raise RuntimeError('remote attachment digest mismatch; the draft is kept for inspection')

    result = json.loads(request(release_url, 'PATCH', b'{"draft":false}'))
    print(f"Published: {result['html_url']}, source commit {commit}")


if __name__ == '__main__':
    main()
