#!/usr/bin/env python3
"""Build OC timestamp releases; never create tags or publish artifacts."""
import argparse
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import subprocess
import tarfile
import tempfile
import zipfile

SPEC = importlib.util.spec_from_file_location('preflight', Path(__file__).with_name('release-preflight.py'))
preflight = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(preflight)


def package(root, destination, tag, target, binary):
    os_name, arch = target.split('/')
    stem = f'oc-{tag}-{os_name}-{arch}'
    assets = {
        binary.name: binary,
        'LICENSE': root / 'LICENSE',
        'NOTICE': root / 'NOTICE',
        'CREDITS': root / 'CREDITS',
        'licenses/notify-LICENSE': root / 'internal/notify/LICENSE',
        'README.md': root / 'README.md',
        'README_zh_CN.md': root / 'README_zh_CN.md',
        'docs/compatibility.json': root / 'docs/compatibility.json',
    }
    if os_name == 'windows':
        archive = destination / (stem + '.zip')
        with zipfile.ZipFile(archive, 'w', zipfile.ZIP_DEFLATED) as out:
            for name, source in assets.items():
                out.write(source, arcname=f'{stem}/{name}')
    else:
        archive = destination / (stem + '.tar.gz')
        with tarfile.open(archive, 'w:gz') as out:
            for name, source in assets.items():
                out.add(source, arcname=f'{stem}/{name}', recursive=False)
    return archive


def build(root, destination, tag):
    version = preflight.release_version(tag)
    sha = preflight.git(root, 'rev-parse', '--verify', 'HEAD^{commit}')
    if preflight.git(root, 'status', '--porcelain', '--untracked-files=all'):
        raise ValueError('release builds require a clean checkout')
    destination = destination.resolve()
    if destination == root.resolve() or root.resolve() in destination.parents:
        raise ValueError('output directory must be outside the checkout')
    destination.mkdir(parents=True, exist_ok=True)
    if any(destination.iterdir()):
        raise ValueError('output directory must be empty')
    support = json.loads((root / 'docs/compatibility.json').read_text())
    flags = ' '.join(['-s -w'] + [f'-X github.com/soulteary/mc/cmd.{key}={value}' for key, value in {
        'Version': version, 'ReleaseTag': tag, 'CommitID': sha, 'ShortCommitID': sha[:12],
    }.items()])
    assets = []
    with tempfile.TemporaryDirectory() as temporary:
        for target in support['crossCompileTargets']:
            os_name, arch = target.split('/')
            binary = Path(temporary) / ('oc.exe' if os_name == 'windows' else 'oc')
            env = dict(os.environ, GOOS=os_name, GOARCH=arch, GOARM='7', CGO_ENABLED='0', GOTOOLCHAIN='local')
            subprocess.run(['go', 'build', '-tags', 'kqueue', '-trimpath', '-ldflags', flags, '-o', str(binary), '.'], cwd=root, env=env, check=True)
            archive = package(root, destination, tag, target, binary)
            assets.append({'name': archive.name, 'target': target, 'sha256': hashlib.sha256(archive.read_bytes()).hexdigest()})
    manifest = {'schema_version': 1, 'release_tag': tag, 'source_commit': sha, 'go_toolchain': support['goToolchain'], 'otterio_sdk': support['otterioSDK'], 'assets': assets}
    (destination / 'release-manifest.json').write_text(json.dumps(manifest, indent=2) + '\n')
    files = sorted(destination.iterdir())
    (destination / 'checksums.txt').write_text(''.join(f'{hashlib.sha256(path.read_bytes()).hexdigest()}  {path.name}\n' for path in files))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('tag')
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    build(Path(__file__).resolve().parents[1], args.output, args.tag)
