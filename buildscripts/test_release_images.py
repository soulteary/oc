"""Verify image contexts preserve release bytes and reject untrusted archive inputs."""
import importlib.util
import io
import json
import os
from pathlib import Path
import shlex
import shutil
import subprocess
import tarfile
import tempfile
import unittest
import sys

sys.dont_write_bytecode = True
ROOT = Path(__file__).resolve().parents[1]
TAG = 'RELEASE.2026-10-07T12-00-00Z'
SHA = '1234567890abcdef1234567890abcdef12345678'


def load(name, filename):
    spec = importlib.util.spec_from_file_location(name, Path(__file__).with_name(filename))
    module = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(module)
    return module


images = load('release_images', 'release-images.py')
build = load('build_release', 'build-release.py')


class Fixture:
    def __init__(self, temporary):
        self.root = Path(temporary) / 'source'
        self.artifacts = Path(temporary) / 'artifacts'
        self.output = Path(temporary) / 'image-context'
        self.root.mkdir()
        self.artifacts.mkdir()
        for name in ('Dockerfile.release', *images.LICENSES.values(), 'README.md', 'README_zh_CN.md', 'docs/compatibility.json'):
            source = self.root / name
            source.parent.mkdir(parents=True, exist_ok=True)
            source.write_bytes((ROOT / name).read_bytes())
        self.manifest = {'schema_version': 1, 'release_tag': TAG, 'source_commit': SHA, 'assets': []}
        self.binaries = {}
        for target in ('linux/amd64', 'linux/arm64', 'darwin/amd64', 'windows/amd64'):
            binary = Path(temporary) / ('oc.exe' if target.startswith('windows') else 'oc')
            # These stamped bytes distinguish both platform and source identity;
            # the context must copy the release executable without rebuilding it.
            payload = f'OC {TAG} commit={SHA} target={target}\n'.encode()
            binary.write_bytes(payload)
            self.binaries[target] = payload
            console = Path(temporary) / ('oc-console.exe' if target.startswith('windows') else 'oc-console')
            console.write_bytes(payload + b'console')
            archive = build.package(self.root, self.artifacts, TAG, target, binary, console)
            self.manifest['assets'].append({'name': archive.name, 'target': target, 'sha256': images.sha256(archive)})
        self.write_manifest()

    def write_manifest(self):
        (self.artifacts / 'release-manifest.json').write_text(json.dumps(self.manifest), encoding='utf-8')

    def archive(self, target='linux/amd64'):
        asset = next(asset for asset in self.manifest['assets'] if asset['target'] == target)
        return self.artifacts / asset['name']

    def rewrite(self, transform):
        archive = self.archive()
        with tarfile.open(archive, 'r:gz') as data:
            entries = [(member, data.extractfile(member).read()) for member in data]
        with tarfile.open(archive, 'w:gz') as data:
            for member, payload in transform(entries):
                data.addfile(member, io.BytesIO(payload) if member.isfile() else None)
        self.manifest['assets'][0]['sha256'] = images.sha256(archive)
        self.write_manifest()

    def prepare(self, **kwargs):
        args = {'root': self.root, 'artifacts': self.artifacts, 'destination': self.output, 'tag': TAG, 'source_sha': SHA}
        args.update(kwargs)
        return images.prepare(**args)


class ReleaseImageTests(unittest.TestCase):
    def assert_rejected(self, fixture, pattern, **kwargs):
        with self.assertRaisesRegex((ValueError, OSError, tarfile.TarError, images.preflight.PreflightError), pattern):
            fixture.prepare(**kwargs)
        self.assertFalse(fixture.output.exists(), 'invalid inputs must not leave an image context')

    def test_context_preserves_stamped_binary_and_exact_source_licenses(self):
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Fixture(temporary)
            fixture.output.mkdir()  # An explicitly supplied empty context is supported.
            self.assertEqual(fixture.prepare(), fixture.output.resolve())
            expected = {'Dockerfile.release', 'dist/oc-linux-amd64', 'dist/oc-linux-arm64', 'dist/oc-console-linux-amd64', 'dist/oc-console-linux-arm64',
                        'licenses/LICENSE', 'licenses/NOTICE', 'licenses/CREDITS', 'licenses/notify-LICENSE'}
            self.assertEqual({path.relative_to(fixture.output).as_posix() for path in fixture.output.rglob('*') if path.is_file()}, expected)
            for arch in ('amd64', 'arm64'):
                binary = fixture.output / 'dist' / f'oc-linux-{arch}'
                self.assertEqual(binary.read_bytes(), fixture.binaries[f'linux/{arch}'])
                if os.name != 'nt':
                    self.assertEqual(binary.stat().st_mode & 0o777, 0o755)
                console = fixture.output / 'dist' / f'oc-console-linux-{arch}'
                self.assertEqual(console.read_bytes(), fixture.binaries[f'linux/{arch}'] + b'console')
                if os.name != 'nt':
                    self.assertEqual(console.stat().st_mode & 0o777, 0o755)
            for name, source in images.LICENSES.items():
                self.assertEqual((fixture.output / 'licenses' / Path(name).name).read_bytes(), (fixture.root / source).read_bytes())
            self.assertIn(b'Apache License', (fixture.output / 'licenses/LICENSE').read_bytes())
            self.assertIn(b'The MIT License', (fixture.output / 'licenses/notify-LICENSE').read_bytes())
            self.assertEqual((fixture.output / 'Dockerfile.release').read_bytes(), (fixture.root / 'Dockerfile.release').read_bytes())

    def test_manifest_identity_and_target_validation(self):
        mutations = [
            ('schema', lambda m: m.update(schema_version=True), 'schema_version'),
            ('tag', lambda m: m.update(release_tag='RELEASE.2026-10-08T12-00-00Z'), 'tag'),
            ('sha', lambda m: m.update(source_commit='f' * 40), 'source commit'),
            ('assets', lambda m: m.update(assets={}), 'assets'),
            ('missing target', lambda m: m['assets'].pop(1), 'linux/arm64'),
            ('duplicate target', lambda m: m['assets'].append(dict(m['assets'][0])), 'duplicate'),
            ('archive name', lambda m: m['assets'][0].update(name='wrong.tar.gz'), 'archive name'),
            ('unsafe name', lambda m: m['assets'][0].update(name='../archive.tar.gz'), 'filenames'),
            ('digest', lambda m: m['assets'][0].update(sha256='bad'), 'SHA-256'),
        ]
        for description, mutate, pattern in mutations:
            with self.subTest(description=description), tempfile.TemporaryDirectory() as temporary:
                fixture = Fixture(temporary)
                mutate(fixture.manifest)
                fixture.write_manifest()
                self.assert_rejected(fixture, pattern)

    def test_hash_missing_archive_and_invalid_archive(self):
        for problem in ('hash', 'missing', 'invalid'):
            with self.subTest(problem=problem), tempfile.TemporaryDirectory() as temporary:
                fixture = Fixture(temporary)
                archive = fixture.archive()
                if problem == 'missing':
                    archive.unlink()
                else:
                    archive.write_bytes(b'not the verified archive')
                    if problem == 'invalid':
                        fixture.manifest['assets'][0]['sha256'] = images.sha256(archive)
                        fixture.write_manifest()
                self.assert_rejected(fixture, {'hash': 'SHA-256', 'missing': 'No such file|cannot find|system cannot', 'invalid': 'gzip'}[problem])

    def test_required_binary_and_license_contents(self):
        for relative in ('oc', 'oc-console', *images.LICENSES):
            with self.subTest(missing=relative), tempfile.TemporaryDirectory() as temporary:
                fixture = Fixture(temporary)
                suffix = '/' + relative
                fixture.rewrite(lambda entries: [(member, data) for member, data in entries if not member.name.endswith(suffix)])
                self.assert_rejected(fixture, 'missing required')
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Fixture(temporary)

            def changed_license(entries):
                for member, data in entries:
                    if member.name.endswith('/licenses/notify-LICENSE'):
                        data = b'incorrect license'
                        member.size = len(data)
                    yield member, data

            fixture.rewrite(changed_license)
            self.assert_rejected(fixture, 'differs from the source license')

    def test_unsafe_or_unexpected_members(self):
        stem = f'oc-{TAG}-linux-amd64'
        bad_names = ['/tmp/outside', f'{stem}/../outside', f'{stem}/licenses/../../outside',
                     f'{stem}\\oc', f'{stem}//oc', f'{stem}/./oc', f'{stem}/unexpected']
        for name in bad_names:
            with self.subTest(name=name), tempfile.TemporaryDirectory() as temporary:
                fixture = Fixture(temporary)

                def append_bad(entries):
                    member = tarfile.TarInfo(name)
                    member.size = 4
                    return entries + [(member, b'evil')]

                fixture.rewrite(append_bad)
                self.assert_rejected(fixture, 'unsafe|unexpected')
        for kind in (tarfile.SYMTYPE, tarfile.LNKTYPE, tarfile.DIRTYPE, tarfile.CHRTYPE):
            with self.subTest(kind=kind), tempfile.TemporaryDirectory() as temporary:
                fixture = Fixture(temporary)

                def append_link(entries):
                    member = tarfile.TarInfo(stem + '/oc')
                    member.type = kind
                    member.linkname = '/tmp/outside'
                    return entries + [(member, b'')]

                fixture.rewrite(append_link)
                self.assert_rejected(fixture, 'unexpected')
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Fixture(temporary)
            fixture.rewrite(lambda entries: entries + [entries[0]])
            self.assert_rejected(fixture, 'duplicate archive')

    def test_output_boundaries_and_existing_contents(self):
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Fixture(temporary)
            self.assert_rejected(fixture, 'outside', destination=fixture.root / 'dist')
            self.assert_rejected(fixture, 'separate', destination=fixture.artifacts / 'context')
            self.assert_rejected(fixture, 'full lowercase', source_sha=SHA[:12])
            self.assert_rejected(fixture, 'not SemVer', tag='v1.0.0')
            fixture.output.mkdir()
            existing = fixture.output / 'keep'
            existing.write_bytes(b'existing context')
            with self.assertRaisesRegex(ValueError, 'empty'):
                fixture.prepare()
            self.assertEqual(existing.read_bytes(), b'existing context')


@unittest.skipUnless(os.name != 'nt' and shutil.which('bash'), 'Linux publication smoke helper requires Bash')
class SmokeHelperTests(unittest.TestCase):
    def run_smoke(self, failure=''):
        with tempfile.TemporaryDirectory() as temporary:
            fixture = Fixture(temporary)
            fixture.prepare()
            mock = Path(temporary) / 'mock-bin'
            mock.mkdir()
            program = mock / 'docker.py'
            program.write_text('''import json
import os
from pathlib import Path
import shutil
import sys
args = sys.argv[1:]
failure = os.environ['MOCK_FAILURE']
context = Path(os.environ['MOCK_CONTEXT'])
with open(os.environ['MOCK_LOG'], 'a', encoding='utf-8') as out:
    out.write(json.dumps(args) + '\\n')
if args[:3] == ['buildx', 'imagetools', 'inspect']:
    assert args[-1] == '--raw'
    platforms = ['amd64'] if failure == 'platforms' else ['amd64', 'arm64']
    manifests = [{'platform': {'os': 'linux', 'architecture': arch}} for arch in platforms]
    manifests.append({'platform': {'os': 'unknown', 'architecture': 'unknown'},
                      'annotations': {'vnd.docker.reference.type': 'attestation-manifest'}})
    print(json.dumps({'manifests': manifests}))
elif args[:1] == ['pull']:
    assert args[1:3] == ['--platform', 'linux/amd64']
elif args[:2] == ['image', 'inspect']:
    template = args[-1]
    if '.Config.Entrypoint' in template:
        print('["oc"]')
    else:
        labels = {'title': 'OC', 'source': os.environ['MOCK_SOURCE'],
                  'version': os.environ['MOCK_TAG'], 'revision': os.environ['MOCK_SHA'],
                  'licenses': 'Apache-2.0 AND MIT'}
        label = next(key for key in labels if 'org.opencontainers.image.' + key + '"' in template)
        print('wrong source' if failure == 'source' and label == 'source' else labels[label])
elif args[:1] == ['run']:
    assert args[1:4] == ['--rm', '--platform', 'linux/amd64']
    if args[-1] == '--version':
        if 'oc-console' in args:
            print('oc-console ' + os.environ['MOCK_TAG'])
        else:
            print('oc version ' + ('WRONG' if failure == 'version' else os.environ['MOCK_TAG']))
    elif args[-1] == '--help':
        print('OC help')
    else:
        assert args[-5:] == ['--config-dir', '/oc-config', 'cp', '/smoke/input', '/smoke/output']
        mount = args[args.index('--mount') + 1]
        source = next(part[4:] for part in mount.split(',') if part.startswith('src='))
        shutil.copyfile(Path(source) / 'input', Path(source) / 'output')
elif args[:1] == ['create']:
    assert args[1:3] == ['--platform', 'linux/amd64']
    print('mock-container')
elif args[:1] == ['cp']:
    source, destination = args[-2:]
    destination = Path(destination)
    Path(os.environ['MOCK_TEMP_CAPTURE']).write_text(str(destination.parent), encoding='utf-8')
    if source.endswith('/ca-bundle.crt'):
        assert args[1] == '-L', 'CA bundle must follow the UBI absolute symlink'
        destination.write_bytes(b'CA bundle contents')
    elif source.endswith('/usr/bin/oc-console'):
        destination.write_bytes((context / 'dist/oc-console-linux-amd64').read_bytes())
    elif source.endswith('/usr/bin/oc'):
        destination.write_bytes(b'wrong binary' if failure == 'binary' else (context / 'dist/oc-linux-amd64').read_bytes())
    else:
        license = source.rsplit('/', 1)[-1]
        destination.write_bytes(b'wrong license' if failure == 'license' else (context / 'licenses' / license).read_bytes())
elif args[:1] == ['rm']:
    assert args == ['rm', '-f', 'mock-container']
else:
    raise AssertionError('Unexpected Docker command: ' + repr(args))
''', encoding='utf-8')
            docker = mock / 'docker'
            docker.write_text('#!/bin/sh\nexec ' + shlex.quote(sys.executable) + ' "$MOCK_DOCKER_PROGRAM" "$@"\n', encoding='utf-8')
            docker.chmod(0o755)
            log = mock / 'docker.log'
            capture = mock / 'temporary-path'
            env = dict(os.environ, MOCK_DOCKER_PROGRAM=str(program), MOCK_LOG=str(log), MOCK_CONTEXT=str(fixture.output),
                       MOCK_TAG=TAG, MOCK_SHA=SHA, MOCK_SOURCE='https://github.com/example/oc',
                       MOCK_FAILURE=failure, MOCK_TEMP_CAPTURE=str(capture))
            env['PATH'] = str(mock) + os.pathsep + env.get('PATH', '')
            result = subprocess.run(['bash', str(Path(__file__).with_name('verify-release-image.sh')),
                                     'ghcr.io/example/oc@sha256:' + 'b' * 64, TAG, SHA,
                                     env['MOCK_SOURCE'], str(fixture.output)], env=env, text=True,
                                    capture_output=True, timeout=20)
            commands = [json.loads(line) for line in log.read_text(encoding='utf-8').splitlines()]
            if capture.exists():
                self.assertFalse(Path(capture.read_text(encoding='utf-8')).exists(), 'smoke helper must remove temporary files')
            if any(command[0] == 'create' for command in commands):
                self.assertIn(['rm', '-f', 'mock-container'], commands, 'smoke helper must remove the inspection container')
            return result, commands

    def test_smoke_verifies_index_source_identity_bytes_and_ca_symlink(self):
        result, commands = self.run_smoke()
        self.assertEqual(result.returncode, 0, result.stdout + result.stderr)
        self.assertTrue(any(command[:3] == ['cp', '-L', 'mock-container:/etc/pki/tls/certs/ca-bundle.crt'] for command in commands))
        self.assertTrue(any('/smoke/output' in command for command in commands))

    def test_smoke_rejects_wrong_platforms_labels_version_binary_or_licenses(self):
        for failure in ('platforms', 'source', 'version', 'binary', 'license'):
            with self.subTest(failure=failure):
                result, _ = self.run_smoke(failure)
                self.assertNotEqual(result.returncode, 0, result.stdout + result.stderr)


if __name__ == '__main__':
    unittest.main()
