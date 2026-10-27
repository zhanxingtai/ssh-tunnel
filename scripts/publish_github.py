#!/usr/bin/env python3
"""Publish a fixed version on GitHub: create a draft, read every attachment back and
verify it, then make the release public. An existing version must never be overwritten.

Usage:
    GITHUB_TOKEN=... GITHUB_REPOSITORY=owner/repo python3 scripts/publish_github.py
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
# A release must cover every platform.
EXPECTED_TARGETS = ('linux-x64', 'linux-arm64', 'darwin-x64', 'darwin-arm64', 'windows-x64')
API_VERSION = '2022-11-28'


class CrossHostRedirect(urllib.request.HTTPRedirectHandler):
    """Drop Authorization on a cross-host redirect, so the token never reaches the object store."""

    def redirect_request(self, req, fp, code, msg, headers, newurl):
        redirected = super().redirect_request(req, fp, code, msg, headers, newurl)
        if redirected is None:
            return None
        if urllib.parse.urlsplit(newurl).netloc != urllib.parse.urlsplit(req.full_url).netloc:
            if redirected.has_header('Authorization'):
                redirected.remove_header('Authorization')
        return redirected


def main():
    token = os.environ['GITHUB_TOKEN']
    repository = os.environ['GITHUB_REPOSITORY']
    api = os.environ.get('GITHUB_API_URL', 'https://api.github.com').rstrip('/')
    # github.com uploads assets to a dedicated host; GHES uses the API host.
    uploads = 'https://uploads.github.com' if api == 'https://api.github.com' else api
    opener = urllib.request.build_opener(CrossHostRedirect())

    def request(url, method='GET', data=None, content_type=None, accept='application/vnd.github+json'):
        headers = {'Authorization': f'Bearer {token}', 'Accept': accept,
                   'X-GitHub-Api-Version': API_VERSION}
        if content_type:
            headers['Content-Type'] = content_type
        req = urllib.request.Request(url, data=data, headers=headers, method=method)
        with opener.open(req, timeout=120) as response:
            return response.read()

    def get_json(url, accept='application/vnd.github+json'):
        return json.loads(request(url, accept=accept))

    def absent(url, what):
        try:
            request(url)
        except urllib.error.HTTPError as error:
            if error.code == 404:
                return
            raise
        raise RuntimeError(f'{what} already exists; overwriting is forbidden, '
                           'read it back first to confirm it is not an unfinished release')

    dist = Path('dist/release')
    # The version and the source commit come from the archive manifest, never from
    # an intermediate state elsewhere.
    with tarfile.open(dist / f'{COMPONENT}-linux-x64.tar.gz') as archive:
        manifest = json.load(archive.extractfile(f'{COMPONENT}/manifest.json'))
    tag, commit = manifest['tag'], manifest['sourceCommit']
    if subprocess.check_output(['git', 'rev-parse', 'HEAD'], text=True).strip() != commit:
        raise RuntimeError('the source commit of the artifacts differs from the current commit')

    quoted_tag = urllib.parse.quote(tag)
    release_by_tag = f'{api}/repos/{repository}/releases/tags/{quoted_tag}'
    commit_by_tag = f'{api}/repos/{repository}/commits/{quoted_tag}'

    # A version already published from the same commit is skipped; a draft, or one
    # pointing at another commit, is refused.
    try:
        existing = get_json(release_by_tag)
    except urllib.error.HTTPError as error:
        if error.code != 404:
            raise
    else:
        if not existing.get('draft') and get_json(commit_by_tag)['sha'] == commit:
            print(f'Version {tag} is already published from the same commit, skipping.')
            return
        raise RuntimeError('the version already exists; overwriting is forbidden, '
                           'read it back first to confirm it is not an unfinished release')

    if os.environ.get('GITHUB_REF_TYPE') == 'tag':
        pushed = os.environ.get('GITHUB_REF_NAME', '')
        if pushed != tag:
            raise RuntimeError(f'the pushed tag {pushed} does not match the artifact tag {tag}')
        if get_json(commit_by_tag)['sha'] != commit:
            raise RuntimeError(f'tag {tag} points at a commit other than the artifact commit')
    else:
        absent(f'{api}/repos/{repository}/git/ref/tags/{quoted_tag}', 'the tag')

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
    release = json.loads(request(f'{api}/repos/{repository}/releases', 'POST',
                                 json.dumps(body, ensure_ascii=True).encode(), 'application/json'))
    release_id = release['id']
    for path in files:
        url = (f'{uploads}/repos/{repository}/releases/{release_id}/assets'
               f'?name={urllib.parse.quote(path.name)}')
        request(url, 'POST', path.read_bytes(), 'application/octet-stream')

    assets = json.loads(request(f'{api}/repos/{repository}/releases/{release_id}/assets?per_page=100'))
    if {asset['name'] for asset in assets} != {path.name for path in files}:
        raise RuntimeError('the remote attachments are incomplete; the draft is kept for inspection')
    for asset in assets:
        actual = request(f'{api}/repos/{repository}/releases/assets/{asset["id"]}',
                         accept='application/octet-stream')
        expected_hash = hashlib.sha256((dist / asset['name']).read_bytes()).hexdigest()
        if hashlib.sha256(actual).hexdigest() != expected_hash:
            raise RuntimeError(f'remote attachment digest mismatch: {asset["name"]}; '
                               'the draft is kept for inspection')

    result = json.loads(request(f'{api}/repos/{repository}/releases/{release_id}', 'PATCH',
                                b'{"draft":false}', 'application/json'))
    if result.get('draft'):
        raise RuntimeError('the release is still a draft after publishing, please check it manually')
    print(f"Published: {result['html_url']}, source commit {commit}")


if __name__ == '__main__':
    main()
