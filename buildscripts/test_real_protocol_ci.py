"""CI evidence must never turn skipped acceptance into a passing result."""
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import patch
import os
import sys
import tempfile

spec = importlib.util.spec_from_file_location('protocol_ci', Path(__file__).with_name('test-real-protocol-ci.py'))
h = importlib.util.module_from_spec(spec)
spec.loader.exec_module(h)


class EvidenceTests(unittest.TestCase):
    def test_named_test_must_run_and_pass(self):
        name = 'TestOtterIOSTSIntegration'
        events = lambda actions: '\n'.join(json.dumps({'Action': action, 'Test': name}) for action in actions)
        self.assertTrue(h.go_result(events(['run', 'pass']), name))
        for actions in ([], ['pass'], ['run', 'skip'], ['run', 'fail'], ['run', 'skip', 'pass']):
            self.assertFalse(h.go_result(events(actions), name))
        self.assertFalse(h.go_result('arbitrary upstream secret text', name))
        self.assertFalse(h.go_result(events(['run', 'pass']), 'TestDiagnosticsRealServer'))

    def test_failure_report_only_contains_structural_test_names(self):
        output = '\n'.join([json.dumps({'Action': 'fail', 'Test': 'TestAcceptance/denied', 'Output': 'secret'}),
                            json.dumps({'Action': 'fail', 'Test': 'unsafe secret name'}), 'token=secret'])
        self.assertEqual(h.failed_tests(output), ['TestAcceptance/denied'])
        traceback = 'File "/private/secret/test-console-range-real.py", line 123\nFile "/private/token.py", line 456'
        self.assertEqual(h.failure_locations(traceback), [{'file': 'test-console-range-real.py', 'line': 123}])

    def test_timeout_and_command_start_failures_are_structural(self):
        code, output, _ = h.run(['/nonexistent-protocol-command'], {}, 1)
        self.assertEqual((code, output), (127, ''))
        code, output, elapsed = h.run([sys.executable, '-c', 'import time; time.sleep(60)'], dict(os.environ), .1)
        self.assertEqual((code, output), (124, ''))
        self.assertLess(elapsed, 15)

    def test_command_start_failure_does_not_skip_other_protocols(self):
        with tempfile.TemporaryDirectory() as tmp:
            binary = Path(tmp) / 'binary'
            binary.write_bytes(b'fixture')
            output = Path(tmp) / 'report.json'
            args = ['test', '--cli', str(binary), '--console', str(binary), '--server', str(binary),
                    '--server-source', 'a' * 40, '--source-kind', 'patched-fixture',
                    '--source-state', 'modified', '--output', str(output)]
            with patch.object(sys, 'argv', args), patch.object(h, 'run', return_value=(127, 'raw-secret', .1)) as run:
                self.assertEqual(h.main(), 1)
            self.assertEqual(run.call_count, 3)
            report = json.loads(output.read_text())
            self.assertEqual([check['name'] for check in report['checks']], ['range', 'diagnostics', 'oidc-sts'])
            self.assertTrue(all(check['reason'] == 'command-start-failed' for check in report['checks']))
            self.assertNotIn('raw-secret', output.read_text())

    def test_partial_range_failure_cannot_pass_or_export_private_fields(self):
        with tempfile.TemporaryDirectory() as tmp:
            binary = Path(tmp) / 'binary'
            binary.write_bytes(b'fixture')
            output = Path(tmp) / 'report.json'
            args = ['test', '--cli', str(binary), '--console', str(binary), '--server', str(binary),
                    '--server-source', 'a' * 40, '--source-kind', 'patched-fixture',
                    '--source-state', 'modified', '--output', str(output)]
            def fake_run(command, env, timeout):
                if 'buildscripts/test-console-range-real.py' in command:
                    Path(command[-1]).write_text(json.dumps({'profiles': [], 'failure': {
                        'profile': 'single-http', 'phase': 'fixture-setup', 'exceptionClass': 'RuntimeError',
                        'message': 'private-secret', 'credentials': 'private-secret'}}))
                    return 0, '', .1
                return 127, '', .1
            with patch.object(sys, 'argv', args), patch.object(h, 'run', side_effect=fake_run):
                self.assertEqual(h.main(), 1)
            report = json.loads(output.read_text())
            self.assertEqual(report['checks'][0]['status'], 'failed')
            self.assertEqual(report['checks'][0]['fixtureFailure'], {
                'profile': 'single-http', 'phase': 'fixture-setup', 'exceptionClass': 'RuntimeError'})
            self.assertNotIn('private-secret', output.read_text())


if __name__ == '__main__':
    unittest.main()
