#!/usr/bin/env python3
"""Prepare an isolated image context from verified, already stamped release archives."""
import argparse
import hashlib
import importlib.util
import json
from pathlib import Path, PurePosixPath
import re
import sys
import tarfile
import tempfile

SPEC = importlib.util.spec_from_file_location('preflight', Path(__file__).with_name('release-preflight.py'))
preflight = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(preflight)

PLATFORMS = ('linux/amd64', 'linux/arm64')
LICENSES = {
    'LICENSE': 'LICENSE',
    'NOTICE': 'NOTICE',
    'CREDITS': 'CREDITS',
    'licenses/notify-LICENSE': 'internal/notify/LICENSE',
}
ARCHIVE_FILES = {'oc', 'oc-console', *LICENSES, 'README.md', 'README_zh_CN.md', 'docs/compatibility.json'}


def sha256(path):
    digest = hashlib.sha256()
    with path.open('rb') as source:
        for chunk in iter(lambda: source.read(1024 * 1024), b''):
            digest.update(chunk)
    return digest.hexdigest()


def image_assets(manifest, tag, source_sha):
    if (not isinstance(manifest, dict) or type(manifest.get('schema_version')) is not int
            or manifest['schema_version'] != 1):
        raise ValueError('release manifest must use schema_version 1')
    if manifest.get('release_tag') != tag:
        raise ValueError('release manifest tag does not match the requested release')
    if manifest.get('source_commit') != source_sha:
        raise ValueError('release manifest source commit does not match the requested source')
    assets = manifest.get('assets')
    if not isinstance(assets, list):
        raise ValueError('release manifest assets must be a list')
    names, targets, selected = set(), set(), {}
    for asset in assets:
        if not isinstance(asset, dict):
            raise ValueError('invalid release manifest asset')
        name, target, digest = (asset.get(key) for key in ('name', 'target', 'sha256'))
        if not isinstance(name, str) or not name or PurePosixPath(name).name != name or '\\' in name:
            raise ValueError('release manifest asset names must be plain filenames')
        if not isinstance(target, str) or not re.fullmatch(r'[a-z0-9]+/[a-z0-9]+', target):
            raise ValueError('invalid release manifest target')
        if not isinstance(digest, str) or not re.fullmatch(r'[a-f0-9]{64}', digest):
            raise ValueError('invalid release manifest SHA-256')
        if name in names or target in targets:
            raise ValueError('duplicate release manifest asset name or target')
        names.add(name)
        targets.add(target)
        if target in PLATFORMS:
            expected = f'oc-{tag}-{target.replace("/", "-")}.tar.gz'
            if name != expected:
                raise ValueError(f'{target} archive name does not match the requested release')
            selected[target] = asset
    if set(selected) != set(PLATFORMS):
        raise ValueError('release manifest must contain linux/amd64 and linux/arm64 archives')
    return selected


def read_archive(archive, tag, target, expected_licenses):
    stem = f'oc-{tag}-{target.replace("/", "-")}'
    extracted, seen = {}, set()
    with tarfile.open(archive, 'r:gz') as data:
        for index, member in enumerate(data):
            if index >= 256:
                raise ValueError(f'{archive.name}: too many archive entries')
            path = PurePosixPath(member.name)
            if (path.is_absolute() or '\\' in member.name or '..' in path.parts
                    or path.as_posix() != member.name or not member.name.startswith(stem + '/')):
                raise ValueError(f'{archive.name}: unsafe archive path {member.name!r}')
            relative = member.name[len(stem) + 1:]
            if not member.isfile() or relative not in ARCHIVE_FILES:
                raise ValueError(f'{archive.name}: unexpected archive entry {member.name!r}')
            if relative in seen:
                raise ValueError(f'{archive.name}: duplicate archive entry {member.name!r}')
            seen.add(relative)
            if relative not in ('oc', 'oc-console') and relative not in LICENSES:
                continue
            maximum = 256 * 1024 * 1024 if relative in ('oc', 'oc-console') else 4 * 1024 * 1024
            if member.size <= 0 or member.size > maximum:
                raise ValueError(f'{archive.name}: invalid size for {relative}')
            source = data.extractfile(member)
            if source is None:
                raise ValueError(f'{archive.name}: unreadable archive entry {relative}')
            with source:
                payload = source.read(maximum + 1)
            if len(payload) != member.size:
                raise ValueError(f'{archive.name}: incomplete archive entry {relative}')
            if relative in LICENSES and payload != expected_licenses[relative]:
                raise ValueError(f'{archive.name}: {relative} differs from the source license')
            extracted[relative] = payload
    missing = {'oc', 'oc-console', *LICENSES} - set(extracted)
    if missing:
        raise ValueError(f'{archive.name}: missing required entries: {", ".join(sorted(missing))}')
    return extracted


def prepare(root, artifacts, destination, tag, source_sha):
    preflight.release_version(tag)
    if not re.fullmatch(r'[a-f0-9]{40}', source_sha):
        raise ValueError('source SHA must be a full lowercase Git commit SHA')
    root, artifacts = root.resolve(), artifacts.resolve()
    if destination.is_symlink():
        raise ValueError('output directory must not be a symlink')
    destination = destination.resolve()
    if destination == root or root in destination.parents or destination in root.parents:
        raise ValueError('output directory must be outside the source checkout')
    if (destination == artifacts or artifacts in destination.parents
            or destination in artifacts.parents):
        raise ValueError('output directory must be separate from the release artifacts')
    if destination.exists() and (not destination.is_dir() or any(destination.iterdir())):
        raise ValueError('output directory must be empty')
    manifest = json.loads((artifacts / 'release-manifest.json').read_text(encoding='utf-8'))
    selected = image_assets(manifest, tag, source_sha)
    licenses = {name: (root / source).read_bytes() for name, source in LICENSES.items()}
    dockerfile = (root / 'Dockerfile.release').read_bytes()
    binaries = {}
    for target in PLATFORMS:
        asset = selected[target]
        archive = artifacts / asset['name']
        if sha256(archive) != asset['sha256']:
            raise ValueError(f'{archive.name}: SHA-256 does not match the release manifest')
        binaries[target.split('/')[1]] = read_archive(archive, tag, target, licenses)
    # Validate every input before creating output. A staged directory also avoids
    # leaving a partially prepared context when a filesystem operation fails.
    destination.parent.mkdir(parents=True, exist_ok=True)
    with tempfile.TemporaryDirectory(prefix='.oc-release-image-', dir=destination.parent) as temporary:
        stage = Path(temporary) / 'context'
        (stage / 'dist').mkdir(parents=True)
        (stage / 'licenses').mkdir()
        for directory in (stage, stage / 'dist', stage / 'licenses'):
            directory.chmod(0o755)
        recipe = stage / 'Dockerfile.release'
        recipe.write_bytes(dockerfile)
        recipe.chmod(0o644)
        for arch, payloads in binaries.items():
            for name in ('oc', 'oc-console'):
                executable = stage / 'dist' / f'{name}-linux-{arch}'
                executable.write_bytes(payloads[name])
                executable.chmod(0o755)
        for name, payload in licenses.items():
            license_file = stage / 'licenses' / PurePosixPath(name).name
            license_file.write_bytes(payload)
            license_file.chmod(0o644)
        if destination.exists():
            destination.rmdir()
        stage.rename(destination)
    return destination


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('tag', help='RELEASE.YYYY-MM-DDTHH-MM-SSZ')
    parser.add_argument('--artifacts', required=True, type=Path)
    parser.add_argument('--output', required=True, type=Path)
    parser.add_argument('--source-sha', required=True)
    args = parser.parse_args()
    try:
        prepare(Path(__file__).resolve().parents[1], args.artifacts, args.output, args.tag, args.source_sha)
    except (ValueError, OSError, tarfile.TarError, preflight.PreflightError) as exc:
        print(f'release image preparation failed: {exc}', file=sys.stderr)
        return 1
    print('Verified image context prepared for linux/amd64 and linux/arm64')
    return 0


if __name__ == '__main__':
    sys.exit(main())
