#!/usr/bin/env python3
import importlib.util
import json
from pathlib import Path
import tempfile
import sys
import unittest
from unittest.mock import patch
from check_budgets import budgets, throughput_gate
from local_http import local_urlopen
from http.server import BaseHTTPRequestHandler, HTTPServer
import threading
import subprocess
import traceback

spec = importlib.util.spec_from_file_location('sbom', Path(__file__).with_name('generate-sbom.py'))
sbom = importlib.util.module_from_spec(spec)
spec.loader.exec_module(sbom)

class MaintenanceTests(unittest.TestCase):
    def test_console_cli_failure_does_not_disclose_credentials(self):
        spec = importlib.util.spec_from_file_location('console_integration', Path(__file__).with_name('test-console-integration.py'))
        integration = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(integration)
        secret = 'synthetic-secret-for-redaction-regression'
        command = ['oc', 'admin', 'user', 'add', 'store', secret]
        timeout = subprocess.TimeoutExpired(command, 30, output=secret.encode())
        with patch.object(integration.subprocess, 'run', side_effect=timeout):
            try:
                integration.run_fixture_cli(command, {}, [secret])
            except RuntimeError as error:
                self.assertNotIn(secret, str(error))
                self.assertNotIn(secret, traceback.format_exc())
                self.assertTrue(error.__suppress_context__)
            else:
                self.fail('CLI timeout did not fail the fixture')
        failed = subprocess.CompletedProcess(command, 1, stdout=secret.encode(), stderr=secret.encode())
        with patch.object(integration.subprocess, 'run', return_value=failed):
            with self.assertRaises(RuntimeError) as raised:
                integration.run_fixture_cli(command, {}, [secret])
            self.assertNotIn(secret, str(raised.exception))
            self.assertIn('[redacted]', str(raised.exception))

    def test_console_failure_retains_completed_checks_and_metrics(self):
        spec = importlib.util.spec_from_file_location('console_integration', Path(__file__).with_name('test-console-integration.py'))
        integration = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(integration)
        def fail(*args, record, **kwargs):
            record.update(checks=['completed fixture group'], writeMetrics={'consoleMiBPerSecond': 243})
            raise RuntimeError('intentional fixture failure')
        with tempfile.TemporaryDirectory() as temp:
            report = Path(temp) / 'results.json'
            argv = ['integration', '--cli', __file__, '--console', __file__, '--server', __file__,
                    '--server-source', 'fixture', '--output', str(report)]
            sdk = json.loads((Path(__file__).resolve().parents[1] / 'docs' / 'compatibility.json').read_text())['otterioSDK']
            build_info = {'Deps': [{'Path': 'github.com/soulteary/otterio', 'Version': sdk}],
                          'Settings': [{'Key': 'vcs.revision', 'Value': 'fixture'},
                                       {'Key': 'vcs.modified', 'Value': 'false'}]}
            with patch.object(sys, 'argv', argv), patch.object(integration, 'scenario', side_effect=fail), \
                    patch.object(integration.platform, 'platform', return_value='fixture-platform'), \
                    patch.object(integration.subprocess, 'check_output', return_value=json.dumps(build_info)):
                with self.assertRaises(RuntimeError):
                    integration.main()
            failed = json.loads(report.read_text())
            self.assertEqual(failed['status'], 'failed')
            self.assertEqual(failed['scenarios'][0]['status'], 'failed')
            self.assertEqual(failed['scenarios'][0]['checks'], ['completed fixture group'])
            self.assertEqual(failed['scenarios'][0]['writeMetrics']['consoleMiBPerSecond'], 243)

    def test_local_requests_ignore_inherited_proxies(self):
        class Handler(BaseHTTPRequestHandler):
            def do_GET(self):
                self.send_response(200)
                self.end_headers()
                self.wfile.write(b'local test evidence')

            def log_message(self, *args):
                pass

        with HTTPServer(('127.0.0.1', 0), Handler) as server:
            worker = threading.Thread(target=server.serve_forever)
            worker.start()
            try:
                with patch('urllib.request.getproxies', return_value={'http': 'http://127.0.0.1:1'}), patch('urllib.request.proxy_bypass', return_value=False):
                    with local_urlopen('http://127.0.0.1:' + str(server.server_port), timeout=2) as response:
                        self.assertEqual(response.read(), b'local test evidence')
            finally:
                server.shutdown()
                worker.join(timeout=3)
                self.assertFalse(worker.is_alive())

    def test_release_provenance(self):
        with tempfile.TemporaryDirectory() as temp:
            binary = Path(temp)/'oc'
            binary.write_bytes(b'fixture')
            clean = {'vcs':'git', 'vcs.modified':'false','vcs.revision':'abc123','vcs.time':'2026-10-07T00:00:00Z'}
            variants = [{k:v for k,v in clean.items() if k != 'vcs'}, {}, {'vcs.modified':'false'}, dict(clean, **{'vcs.modified':'true'}), clean]
            for settings in variants:
                info = {'GoVersion':'go1.27.1','Main':{'Version':'v1.0.0'},'Deps':[{'Path':'github.com/urfave/cli/v3','Version':'v3.14.0'}], 'Settings':[{'Key':k,'Value':v} for k,v in settings.items()] + [{'Key':'GO_EXTLINK_ENABLED'}]}
                with patch.object(sbom.subprocess,'check_output',return_value=json.dumps(info).encode()):
                    if settings == clean:
                        sbom.inventory(binary, True)
                    else:
                        with self.assertRaises(ValueError): sbom.inventory(binary, True)
            info['Deps']=[{'Path':'module','Replace':{'Path':'other','Version':'v1.0.0'}}]
            with patch.object(sbom.subprocess,'check_output',return_value=json.dumps(info).encode()):
                with self.assertRaises(ValueError): sbom.inventory(binary, True)

    def test_compiled_cli_boundary(self):
        sbom.validate_cli_modules([{'Path': 'github.com/urfave/cli/v3', 'Version': 'v3.14.0'},
                                   {'Path': 'github.com/minio/minio-go/v7', 'Version': 'v7.0.0'}], True)
        for modules in ([], [{'Path': 'github.com/urfave/cli/v3', 'Version': 'v3.13.0'}],
                        [{'Path': 'github.com/minio/cli', 'Version': 'v1.24.2'}],
                        [{'Path': 'github.com/minio/cli/v2', 'Version': 'v2.0.0'}]):
            with self.subTest(modules=modules), self.assertRaises(ValueError):
                sbom.validate_cli_modules(modules, True)

    def test_failed_scenario_retains_partial_metrics(self):
        spec = importlib.util.spec_from_file_location('integration', Path(__file__).with_name('test-core-integration.py'))
        integration = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(integration)
        def fail(*args, record, **kwargs):
            record.update(scenario='test', checks=7, stability={'transferMetrics':[{'wallSeconds':1.5}]})
            raise ValueError('intentional failure')
        with tempfile.TemporaryDirectory() as temp:
            report = Path(temp)/'results.json'
            with patch.object(sys,'argv',['integration','--oc',__file__,'--otterio',__file__,'--stability-only','--report',str(report)]), patch.object(integration,'migration_checks',return_value={'scenario':'migration'}), patch.object(integration,'scenario',side_effect=fail):
                with self.assertRaises(ValueError): integration.main()
            failed = json.loads(report.read_text())['results'][1]
            self.assertEqual(failed['checks'],7)
            self.assertEqual(failed['stability']['transferMetrics'][0]['wallSeconds'],1.5)
            self.assertEqual(failed['status'],'failed')
            self.assertTrue(Path(str(report)+'.artifacts','failure-summary.json').is_file())

    def test_performance_gate(self):
        limits = budgets()
        floor = max(limits['minimumMiBPerSecond'],100*(1-limits['maximumThroughputDropFraction']))
        self.assertEqual(throughput_gate(100,100,limits),floor)
        for speed in (floor-1, 1):
            with self.assertRaises(AssertionError): throughput_gate(speed,100,limits)

if __name__ == '__main__': unittest.main()
