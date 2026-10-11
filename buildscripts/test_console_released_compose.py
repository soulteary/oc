"""Reject false browser-off success and derive actual container architecture."""
import subprocess
import json
import tempfile
from pathlib import Path
import unittest
from unittest.mock import patch

from console_acceptance_protocol import legacy_route_disabled
from console_released_compose_acceptance import container_platform, probe_legacy_route, images_from_recipe, finalize_report


class ReleasedComposeEvidenceTests(unittest.TestCase):
    def test_authentication_rejection_does_not_prove_route_removal(self):
        for status, body in [(403, '{"error":"InvalidToken"}'), (403, 'Access Denied'),
                             (405, ''), (400, '<html>bad request</html>'),
                             (403, '<Error><Code/></Error>'), (403, '<Reply><Code>AccessDenied</Code></Reply>')]:
            with self.subTest(status=status, body=body):
                self.assertFalse(legacy_route_disabled(status, body))
        self.assertTrue(legacy_route_disabled(404, 'not found'))
        self.assertTrue(legacy_route_disabled(403, '<Error><Code>AccessDenied</Code></Error>'))
        self.assertTrue(legacy_route_disabled(400, '<Error xmlns="urn:s3"><Code>InvalidRequest</Code></Error>'))

    def test_image_evidence_uses_recipe_and_requires_matching_immutable_refs(self):
        recipe=(Path(__file__).resolve().parents[1]/'deploy/compose.console.yaml').read_text()
        refs=images_from_recipe(recipe)
        self.assertEqual(refs['oc'], refs['oc-init'])
        with self.assertRaises(ValueError):
            images_from_recipe(recipe.replace('@sha256:', '@sha512:'))
        with self.assertRaises(ValueError):
            images_from_recipe(recipe.replace(refs['oc'], 'other@sha256:'+'a'*64, 1))

    def test_cleanup_failure_saves_report_and_preserves_primary_failure(self):
        def fail():
            raise RuntimeError('sensitive subprocess output')
        with tempfile.TemporaryDirectory() as root:
            path=Path(root)/'nested/report.json'
            report={'status':'failed','error':'original failure'}
            finalize_report(report, path, fail)
            saved=json.loads(path.read_text())
            self.assertEqual(saved['error'], 'original failure')
            self.assertEqual(saved['cleanup'], 'failed')
            self.assertNotIn('sensitive', path.read_text())
            report={'status':'passed'}
            with self.assertRaises(RuntimeError):
                finalize_report(report, path, fail)
            self.assertEqual(json.loads(path.read_text())['status'], 'failed')

    @patch('console_released_compose_acceptance.subprocess.run')
    def test_probe_requires_response_body_and_successful_curl(self, run):
        for reply in ['{"error":"InvalidToken"}\n403', '\n405', 'garbled',
                      '<Error><Code>AccessDenied</Code></Error>\n403', 'not found\n404']:
            run.return_value = subprocess.CompletedProcess([], 0, stdout=reply)
            if reply.endswith('\n404') or reply.startswith('<Error>'):
                probe_legacy_route('container', 'GET', '/otterio/')
            else:
                with self.assertRaises(RuntimeError):
                    probe_legacy_route('container', 'GET', '/otterio/')
        run.side_effect = subprocess.CalledProcessError(7, ['curl'])
        with self.assertRaises(subprocess.CalledProcessError):
            probe_legacy_route('container', 'GET', '/otterio/')

    @patch('console_released_compose_acceptance.subprocess.run')
    def test_platform_comes_from_running_image_not_host_or_tag(self, run):
        for actual in ('linux/amd64', 'linux/arm64', 'linux/arm/v7'):
            run.side_effect = [subprocess.CompletedProcess([], 0, stdout='sha256:actual-image\n'),
                               subprocess.CompletedProcess([], 0, stdout=actual + '\n')]
            self.assertEqual(container_platform('actual-container'), actual)
            self.assertEqual(run.call_args_list[-2].args[0][-1], 'actual-container')
            self.assertEqual(run.call_args_list[-1].args[0][-1], 'sha256:actual-image')
        run.side_effect = [subprocess.CompletedProcess([], 0, stdout='sha256:actual-image\n'),
                           subprocess.CompletedProcess([], 0, stdout='/\n')]
        with self.assertRaises(RuntimeError):
            container_platform('actual-container')


if __name__ == '__main__':
    unittest.main()
