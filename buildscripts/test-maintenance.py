#!/usr/bin/env python3
import importlib.util
import json
import os
from pathlib import Path
import tempfile
import sys
import unittest
from unittest.mock import patch
from check_budgets import budgets, throughput_gate
import stability_checks as stability
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
                                   {'Path': 'github.com/soulteary/otterio-sdk/v7', 'Version': 'v7.0.0'}], True)
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

    def test_transfer_medians_keep_majority_slowdowns_and_absolute_floor(self):
        limits = budgets()
        for actual, reference in (([40, 40, 100], [100, 100, 100]),
                                  ([40, 4, 100], [100, 10, 10]),
                                  ([4, 4, 20], [4, 4, 4])):
            metric = {'samplePairs': [
                {'measured': {'aggregateMiBPerSecond': speed},
                 'reference': {'aggregateMiBPerSecond': baseline}}
                for speed, baseline in zip(actual, reference)]}
            with self.subTest(actual=actual), self.assertRaises(AssertionError):
                stability.check_transfer_samples(metric, limits)
            self.assertEqual(metric['aggregateMiBPerSecond'], actual[0])
            self.assertEqual(len(metric['samplePairs']), 3)

    def test_transfer_medians_keep_fixed_outlier_sample(self):
        metric = {'samplePairs': [
            {'measured': {'aggregateMiBPerSecond': speed},
             'reference': {'aggregateMiBPerSecond': 100}}
            for speed in (10, 80, 90)]}
        stability.check_transfer_samples(metric, budgets())
        self.assertEqual(metric['aggregateMiBPerSecond'], 80)
        self.assertEqual(metric['requiredMiBPerSecond'], 5)
        self.assertEqual(metric['pairedMedianRatio'], 0.8)
        self.assertEqual(metric['requiredPairedRatio'], 0.5)
        self.assertEqual(metric['samplePairs'][0]['measured']['aggregateMiBPerSecond'], 10)

    def transfer_fixture(self, corrupt=None, fail=None):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            source = root / 'source'
            source.write_bytes(b'payload' * 40000)
            expected = stability.file_hash(source)
            objects = {}
            destinations = set()
            lock = threading.Lock()
            def transfer(args):
                _, src, dest = args
                if fail and fail in ' '.join(args):
                    raise RuntimeError('fixture transfer failure')
                with lock:
                    if src == str(source):
                        if dest in objects:
                            raise AssertionError('reference overwrote a measured upload')
                        objects[dest] = source.read_bytes()
                    else:
                        path = Path(dest)
                        if path.exists() or dest in destinations:
                            raise AssertionError('reference reused a measured download')
                        destinations.add(dest)
                        path.write_bytes(b'corrupt' if corrupt and corrupt in dest else objects[src])
                return {'startedAt': 10, 'finishedAt': 10.001, 'elapsedSeconds': 0.001,
                        'sampledPeakRSSMiB': 1, 'samplerCleanupSeconds': 9}
            def run(*args):
                self.assertEqual(args[0], 'rm')
                for target in args[1:]:
                    del objects[target]
            evidence = {'checks': 0, 'transferMetrics': []}
            with patch.object(stability, 'file_hash', wraps=stability.file_hash) as hashes:
                checks = stability.transfer_performance_checks(
                    transfer, run, source, root, expected, budgets(), evidence)
                hash_count = hashes.call_count
            self.assertEqual(objects, {})
            self.assertFalse(list(root.glob('*.download')))
            return evidence, checks, hash_count

    def test_transfer_workloads_use_fresh_targets_and_hash_every_download(self):
        evidence, checks, hash_count = self.transfer_fixture()
        self.assertEqual(checks, 84)
        self.assertEqual(hash_count, 28)
        self.assertEqual(len(evidence['transferMetrics']), 4)
        for metric in evidence['transferMetrics']:
            self.assertEqual(len(metric['samplePairs']), 3)
            for pair in [metric['warmup'], *metric['samplePairs']]:
                self.assertEqual(len(pair['reference']['processSamples']), 1)
                self.assertEqual(len(pair['measured']['processSamples']), metric['concurrency'])
                self.assertAlmostEqual(pair['measured']['wallSeconds'], 0.001)
            self.assertEqual(metric['samplePairs'][0]['order'], ['measured', 'reference'])
            self.assertEqual(metric['samplePairs'][1]['order'], ['reference', 'measured'])

    def test_transfer_warmup_reference_and_measured_corruption_fail(self):
        for target in ('warmup', 'sample-1-reference', 'sample-2-measured'):
            with self.subTest(target=target), self.assertRaisesRegex(AssertionError, 'checksum mismatch'):
                self.transfer_fixture(corrupt=target)

    def test_transfer_warmup_failure_is_not_retried(self):
        with self.assertRaisesRegex(RuntimeError, 'fixture transfer failure'):
            self.transfer_fixture(fail='warmup-reference')

    def invoke_sampled_transfer(self, rss_kib=1024, exit_rss_kib=1024, timeout=False):
        class FixtureDone(Exception):
            pass
        class SampleEvent:
            calls = 0
            def is_set(self):
                self.calls += 1
                return self.calls > 1
            def set(self):
                self.calls = 2
            def wait(self, _):
                pass
        class Sampler:
            def __init__(self, target):
                self.target = target
            def start(self):
                self.target()
            def join(self, timeout):
                pass
            def is_alive(self):
                return False
        child = unittest.mock.Mock()
        child.exit_peak_rss_kib = exit_rss_kib
        child.returncode = None if timeout else 0
        child.poll.side_effect = lambda: child.returncode
        child.kill.side_effect = lambda: setattr(child, 'returncode', -9)
        child.communicate.side_effect = ([subprocess.TimeoutExpired('fixture', 120), (b'', b'')]
                                         if timeout else [(b'', b'')])
        outcome = {}
        def one_transfer(transfer, *args):
            outcome.update(transfer(['cp', 'source', 'destination']))
            raise FixtureDone()
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            (root / 'large').write_bytes(b'payload')
            self.transfer_evidence = {}
            with patch.object(stability, 'transfer_performance_checks', side_effect=one_transfer), \
                    patch.object(stability, 'TransferProcess', return_value=child), \
                    patch.object(stability.subprocess, 'run', return_value=subprocess.CompletedProcess([], 0, stdout=b'' if rss_kib is None else str(rss_kib).encode())), \
                    patch.object(stability.threading, 'Thread', Sampler), \
                    patch.object(stability.threading, 'Event', SampleEvent), \
                    patch.object(stability.time, 'monotonic', side_effect=[10, 10.25, 17.25]):
                try:
                    stability.stability_checks('fixture', root, {}, root, None, root, evidence=self.transfer_evidence)
                except FixtureDone:
                    pass
        child.communicate.assert_any_call(timeout=budgets()['transferSeconds'])
        return outcome

    def test_transfer_timer_excludes_sampler_cleanup(self):
        outcome = self.invoke_sampled_transfer()
        self.assertEqual(outcome['elapsedSeconds'], 0.25)
        self.assertEqual(outcome['samplerCleanupSeconds'], 7)
        self.assertEqual(outcome['finishedAt'] - outcome['startedAt'], 0.25)

    def test_transfer_rss_and_timeout_remain_fatal(self):
        with self.assertRaisesRegex(AssertionError, 'RSS budget'):
            self.invoke_sampled_transfer(rss_kib=600*1024)
        with self.assertRaises(subprocess.TimeoutExpired):
            self.invoke_sampled_transfer(timeout=True)
        self.assertEqual(self.transfer_evidence['transferAttempts'][0]['status'], 'failed')

    def test_transfer_exit_peak_closes_short_process_sampling_race(self):
        outcome = self.invoke_sampled_transfer(rss_kib=None, exit_rss_kib=2048)
        self.assertEqual(outcome['sampledPeakRSSMiB'], 0)
        self.assertEqual(outcome['exitPeakRSSMiB'], 2)
        self.assertEqual(outcome['processPeakRSSMiB'], 2)
        attempt = self.transfer_evidence['transferAttempts'][0]
        self.assertEqual(attempt['sampledPeakRSSMiB'], 0)
        self.assertEqual(attempt['processPeakRSSMiB'], 2)
        with self.assertRaisesRegex(AssertionError, 'no process memory'):
            self.invoke_sampled_transfer(rss_kib=None, exit_rss_kib=0)
        with self.assertRaisesRegex(AssertionError, 'RSS budget'):
            self.invoke_sampled_transfer(rss_kib=None, exit_rss_kib=600*1024)

    @unittest.skipUnless(hasattr(os, 'wait4'), 'wait4 is only available on POSIX')
    def test_transfer_wait4_peak_units_and_running_child(self):
        process = stability.TransferProcess.__new__(stability.TransferProcess)
        process.pid = 321
        process.exit_peak_rss_kib = 0
        for platform, maxrss in (('linux', 2048), ('darwin', 2048*1024)):
            with self.subTest(platform=platform), \
                    patch.object(stability.sys, 'platform', platform), \
                    patch.object(stability.os, 'wait4', return_value=(321, 0, unittest.mock.Mock(ru_maxrss=maxrss))) as wait:
                self.assertEqual(process._try_wait(os.WNOHANG), (321, 0))
                self.assertEqual(process.exit_peak_rss_kib, 2048)
                wait.assert_called_once_with(321, os.WNOHANG)
        with patch.object(stability.os, 'wait4', return_value=(0, 0, unittest.mock.Mock(ru_maxrss=0))):
            self.assertEqual(process._try_wait(os.WNOHANG), (0, 0))
            self.assertEqual(process.exit_peak_rss_kib, 2048)

    @unittest.skipUnless(hasattr(os, 'wait4'), 'wait4 is only available on POSIX')
    def test_transfer_wait4_peaks_are_independent_for_concurrent_children(self):
        # Reap the larger child first: process-global RUSAGE_CHILDREN would
        # incorrectly attribute its retained peak to the smaller child too.
        children = []
        def launch(size):
            child = stability.TransferProcess(
                [sys.executable, '-c', f'payload = bytearray({size})'],
                stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            children.append(child)
            return child
        try:
            startup = launch(1024*1024)
            startup.communicate(timeout=10)
            self.assertEqual(startup.returncode, 0)
            self.assertGreater(startup.exit_peak_rss_kib, 0)
            # Linux can retain the parent's inherited RSS before exec. Size
            # the larger allocation above that measured startup high-water.
            small = launch(1024*1024)
            large = launch(int(startup.exit_peak_rss_kib*1024) + 64*1024*1024)
            for child in (large, small):
                child.communicate(timeout=10)
                self.assertEqual(child.returncode, 0)
                self.assertGreater(child.exit_peak_rss_kib, 0)
            self.assertGreater(large.exit_peak_rss_kib,
                               small.exit_peak_rss_kib + 24*1024)
        finally:
            for child in children:
                if child.returncode is None:
                    child.kill()
                    child.communicate()

if __name__ == '__main__': unittest.main()
