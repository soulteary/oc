"""Reject drift between the independent SDK, server and release baselines."""
import json
from pathlib import Path
import shutil
import subprocess
import sys
import tempfile
import unittest

ROOT = Path(__file__).resolve().parents[1]
FILES = (
    'cmd/update-main.go', 'cmd/main.go', 'cmd/admin-subnet-health.go',
    '.goreleaser.yml', 'Makefile', 'Dockerfile', 'Dockerfile.dev',
    'Dockerfile.release', 'docker-buildx.sh', 'go.mod',
    'docs/compatibility.json', 'buildscripts/cross-compile.sh',
    'buildscripts/check_budgets.py', 'buildscripts/verify-release-boundaries.py',
)


class ReleaseBoundaryTests(unittest.TestCase):
    def check(self, modify=None):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            for name in FILES:
                target = root / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(ROOT / name, target)
            if modify:
                modify(root)
            return subprocess.run(
                [sys.executable, str(root / 'buildscripts/verify-release-boundaries.py')],
                capture_output=True, text=True, check=False,
            )

    def manifest_change(self, field, value):
        def modify(root):
            path = root / 'docs/compatibility.json'
            support = json.loads(path.read_text())
            support[field] = value
            path.write_text(json.dumps(support))
        return modify

    def test_reviewed_independent_sdk_and_server_baselines_pass(self):
        result = self.check()
        self.assertEqual(result.returncode, 0, result.stderr)

    def test_server_version_cannot_label_the_independent_sdk(self):
        support = json.loads((ROOT / 'docs/compatibility.json').read_text())
        sdk = dict(support['storageSDK'], version=support['otterioSDK'])
        result = self.check(self.manifest_change('storageSDK', sdk))
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('storage SDK differs', result.stderr)

    def test_missing_sdk_source_and_drifted_kit_are_rejected(self):
        support = json.loads((ROOT / 'docs/compatibility.json').read_text())
        cases = (
            ('storageSDK', dict(support['storageSDK'], source='c11549d'), 'full storage SDK source SHA'),
            ('otterioKits', dict(support['otterioKits'], **{'github.com/soulteary/otterio-kits/md5-simd': 'v1.1.2'}), 'OtterIO kits differ'),
        )
        for field, value, message in cases:
            with self.subTest(field=field):
                result = self.check(self.manifest_change(field, value))
                self.assertNotEqual(result.returncode, 0)
                self.assertIn(message, result.stderr)

    def test_retired_storage_import_is_rejected(self):
        def modify(root):
            (root / 'legacy.go').write_text('package fixture\nimport _ "github.com/minio/minio-go/v7/pkg/credentials"\n')
        result = self.check(modify)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn('imports a retired storage SDK or kit', result.stderr)


if __name__ == '__main__':
    unittest.main()
