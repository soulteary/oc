"""Exercise release target/identity/license packaging without publishing."""
import hashlib
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import patch
import zipfile

sys.dont_write_bytecode = True
SPEC = importlib.util.spec_from_file_location('build_release', Path(__file__).with_name('build-release.py'))
release = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(release)
ROOT = Path(__file__).resolve().parents[1]
TAG = 'RELEASE.2026-10-07T12-00-00Z'


class ReleaseBuildTests(unittest.TestCase):
    def test_all_targets_identity_checksums_and_licenses(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary) / 'source'
            root.mkdir()
            for relative in ('LICENSE', 'NOTICE', 'CREDITS', 'README.md', 'README_zh_CN.md', 'internal/notify/LICENSE', 'docs/compatibility.json'):
                path = root / relative
                path.parent.mkdir(parents=True, exist_ok=True)
                path.write_bytes((ROOT / relative).read_bytes())
            subprocess.run(['git', 'init', '-b', 'main', str(root)], check=True, capture_output=True)
            for key, value in [('user.name', 'Release Test'), ('user.email', 'release-test@example.invalid'), ('commit.gpgsign', 'false')]:
                subprocess.run(['git', '-C', str(root), 'config', key, value], check=True)
            subprocess.run(['git', '-C', str(root), 'add', '.'], check=True)
            subprocess.run(['git', '-C', str(root), 'commit', '-m', 'fixture'], check=True, capture_output=True)
            calls = []
            real_run = subprocess.run

            def builder(args, **kwargs):
                if args[0] != 'go':
                    return real_run(args, **kwargs)
                calls.append((args, kwargs))
                Path(args[args.index('-o') + 1]).write_bytes(b'release executable')
                return subprocess.CompletedProcess(args, 0)

            output = Path(temporary) / 'artifacts'
            with patch.object(release.subprocess, 'run', side_effect=builder):
                release.build(root, output, TAG)
            manifest = json.loads((output / 'release-manifest.json').read_text())
            self.assertEqual(manifest['release_tag'], TAG)
            sha = release.preflight.git(root, 'rev-parse', 'HEAD')
            self.assertEqual(manifest['source_commit'], sha)
            targets = json.loads((ROOT / 'docs/compatibility.json').read_text())['crossCompileTargets']
            self.assertEqual([asset['target'] for asset in manifest['assets']], targets)
            self.assertEqual(len(list(output.iterdir())), 13)
            for args, kwargs in calls:
                flags = args[args.index('-ldflags') + 1]
                self.assertIn('cmd.ReleaseTag=' + TAG, flags)
                self.assertIn('cmd.Version=2026-10-07T12:00:00Z', flags)
                self.assertIn('cmd.CommitID=' + sha, flags)
                self.assertEqual(kwargs['env']['CGO_ENABLED'], '0')
            for asset in manifest['assets']:
                archive = output / asset['name']
                self.assertEqual(hashlib.sha256(archive.read_bytes()).hexdigest(), asset['sha256'])
                if archive.suffix == '.zip':
                    with zipfile.ZipFile(archive) as data:
                        entries = {name: data.read(name) for name in data.namelist()}
                else:
                    with tarfile.open(archive) as data:
                        entries = {member.name: data.extractfile(member).read() for member in data.getmembers()}
                for suffix, expected in [('licenses/notify-LICENSE', ROOT / 'internal/notify/LICENSE'), ('/LICENSE', ROOT / 'LICENSE')]:
                    found = [value for name, value in entries.items() if name.endswith(suffix)]
                    self.assertEqual(found, [expected.read_bytes()])
                executable = 'oc.exe' if asset['target'].startswith('windows/') else 'oc'
                self.assertIn(b'release executable', [value for name, value in entries.items() if name.endswith('/' + executable)])
            for line in (output / 'checksums.txt').read_text().splitlines():
                digest, name = line.split('  ', 1)
                self.assertEqual(hashlib.sha256((output / name).read_bytes()).hexdigest(), digest)
            with self.assertRaisesRegex(ValueError, 'empty'):
                release.build(root, output, TAG)
            with self.assertRaisesRegex(ValueError, 'outside'):
                release.build(root, root / 'dist', TAG)
            (root / 'dirty').write_text('untracked')
            with self.assertRaisesRegex(ValueError, 'clean'):
                release.build(root, Path(temporary) / 'other', TAG)


if __name__ == '__main__':
    unittest.main()
