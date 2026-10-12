"""Candidate success needs every positive profile, clean identities and cleanup."""
import argparse
import copy
import importlib.util
import json
from pathlib import Path
import tempfile
import unittest
from unittest.mock import patch

spec = importlib.util.spec_from_file_location('console_migration_pair', Path(__file__).with_name('test-console-migration-pair.py'))
h = importlib.util.module_from_spec(spec)
spec.loader.exec_module(h)
OC_SOURCE = 'a' * 40
SDK = 'v0.0.0-fixture'


def info(source, client=True):
    return {'Settings': [{'Key': key, 'Value': value} for key, value in
                         [('vcs.revision', source), ('vcs.modified', 'false'), ('GOOS', 'linux'), ('GOARCH', 'amd64')]],
            'Deps': [{'Path': 'github.com/soulteary/otterio', 'Version': SDK}] if client else []}


def programs():
    return {name: h.binary_identity(info(h.SERVER_SOURCE if name == 'server' else OC_SOURCE, name != 'server'),
                                   h.SERVER_SOURCE if name == 'server' else OC_SOURCE, 'b' * 64, client=name != 'server')
            for name in ('oc', 'console', 'server')}


def cleanup():
    return {'status': 'passed', 'errors': [], 'processes': [{'component': 'server', 'stopped': True}]}


def integration(profile):
    scopes = {'storage': 'read-only-and-writes', 'settings': 'read-only-and-protected-settings',
              'features-version-copy': 'console-five-features-and-version-copy',
              'native': 'native-iam-https-read-only', 'relationship-iam': 'relationship-iam-native'}
    report = {'status': 'passed', 'scope': scopes[profile], 'serverSource': h.SERVER_SOURCE,
              'sdkPin': SDK, 'binaries': {name: {'sha256': 'b' * 64} for name in ('oc', 'oc-console', 'otterio')},
              'identityEvidence': {'sdkPin': 'verified-client-build-info', 'serverSource': 'verified-vcs-revision'},
              'storage': 'single-node-four-disk-erasure', 'scenarios': []}
    report['settingsProfile'] = 'v1-protected-settings'
    for scenario in h.TLS_SCENARIOS if profile in ('native', 'relationship-iam') else h.SCENARIOS:
        row = {'scenario': scenario, 'status': 'passed', 'checks': ['required check'] * 10, 'processCleanup': cleanup()}
        if profile == 'storage':
            row['writeMetrics'] = {'fixtureBytes': 1}
        elif profile == 'settings':
            row['settingsMetrics'] = {'profile': 'v1-protected-settings'}
        elif profile == 'features-version-copy':
            row['featureMetrics'] = {'fixtureBytes': 1}
            row['versionCopyMetrics'] = {'profile': 'copy-source-version-authorization',
                                         'confirmedCopies': 1, 'accessDeniedWithoutDestination': 1}
        elif profile == 'native':
            row['nativeMetrics'] = {'iamCreation': 'native user/group/service-account; permission and duplicate-target checks verified',
                                    'browserTLS': 'verified', 'storageTLS': 'verified', 'rootLogin': 'denied'}
        else:
            row['relationshipIAMMetrics'] = {'capabilityMode': 'required-supported',
                                             'positiveMutationCoverage': sorted(h.RELATIONSHIP_COVERAGE), 'processCleanup': cleanup()}
        report['scenarios'].append(row)
    return report


def protocol():
    return {'schemaVersion': 1, 'passed': True, 'server': {'source': h.SERVER_SOURCE, 'kind': 'module-build',
             'sourceState': 'clean', 'binarySHA256': 'b' * 64}, 'clientBinarySHA256': {'cli': 'b' * 64, 'console': 'b' * 64},
             'checks': [
                 {'name': 'range', 'status': 'passed', 'exitCode': 0, 'profiles': [
                     {'profile': name, 'historicalVersionStatus': 206, 'restrictedHistoricalStatus': 403}
                     for name in ('single-http', 'dual-http', 'dual-tls')]},
                 {'name': 'diagnostics', 'status': 'passed', 'exitCode': 0, 'reason': 'named-test-ran-and-passed',
                  'test': 'TestDiagnosticsRealServer'},
                 {'name': 'oidc-sts', 'status': 'passed', 'exitCode': 0, 'reason': 'named-test-ran-and-passed',
                  'test': 'TestOtterIOSTSIntegration'}]}


def oidc(dual):
    identities = programs()
    return {'status': 'passed', 'cleanup': 'passed', 'programs': {
                name: {'declaredSource': row['source'], 'binaryVCSRevision': row['source'],
                       'binaryVCSModified': False, 'sha256': row['binarySHA256']} for name, row in identities.items()},
            'storageTopology': 'dual HTTPS S3/Admin listeners' if dual else 'single HTTPS S3/Admin listener',
            'browserSiteTopology': 'localhost IdP and 127.0.0.1 Console (different sites)',
            'certificateIdentities': {'primaryCAsha256': 'c' * 64, 'adminCAsha256': ('d' if dual else 'c') * 64},
            'checks': ['protocol check'] * 12 + ['Bob saved pre-expiry cookie is rejected after fixed TTL despite successful refresh']
                      + (['independent Admin HTTPS CA accepted; S3 primary CA is rejected for Admin listener'] if dual else []),
            'consoleRefreshEvidence': {'waitSeconds': 95, 'idTokenSeconds': 90, 'keycloakEventsBefore': 1, 'keycloakEventsAfter': 2},
            'browserAcceptance': {'status': 'passed', 'persistentProfile': False, 'successfulRefreshEventsDelta': 1,
                                  'browser': 'Chromium fixture', 'checks': [{'name': name, 'status': 'passed'} for name in sorted(h.BROWSER_CHECKS)]},
            'consoleAcceptance': 'passed: required assertions', 'consoleFixedExpiryReference': 'callback plus configured TTL',
            'keycloak': {'image': 'quay.io/keycloak/keycloak@sha256:' + 'e' * 64}}


class CandidateEvidenceTests(unittest.TestCase):
    def test_identity_requires_embedded_clean_revision_and_unreplaced_sdk(self):
        self.assertEqual(h.binary_identity(info(OC_SOURCE), OC_SOURCE, 'b' * 64, True)['sdkPin'], SDK)
        for change in ('revision', 'modified', 'missing-vcs', 'replace', 'missing-sdk', 'platform'):
            with self.subTest(change=change):
                build = info(OC_SOURCE)
                if change == 'revision':
                    build['Settings'][0]['Value'] = 'f' * 40
                elif change == 'modified':
                    build['Settings'][1]['Value'] = 'true'
                elif change == 'missing-vcs':
                    build['Settings'] = build['Settings'][2:]
                elif change == 'replace':
                    build['Deps'][0]['Replace'] = {'Path': '/tmp/local-source'}
                elif change == 'missing-sdk':
                    build['Deps'] = []
                else:
                    build['Settings'] = build['Settings'][:2]
                with self.assertRaises(ValueError):
                    h.binary_identity(build, OC_SOURCE, 'b' * 64, True)

    def test_commands_require_all_positive_profiles_and_independent_fixtures(self):
        args = argparse.Namespace(server_source=h.SERVER_SOURCE, oc_source=OC_SOURCE)
        plan = h.commands(args, {'oc': '/bin/oc', 'console': '/bin/ui', 'server': '/bin/server'}, programs(), Path('/tmp/pair.json'))
        self.assertEqual([row[0] for row in plan], h.PROFILES)
        self.assertEqual(len({row[2] for row in plan}), len(h.PROFILES))
        for name, command, _, timeout in plan:
            self.assertGreater(timeout, 0)
            self.assertNotIn('--server-patch', command)
            self.assertFalse(any('legacy' in item or 'unsupported' in item for item in command))
            self.assertIn(h.SERVER_SOURCE, command)
        for index in range(3):
            self.assertIn(','.join(h.SCENARIOS), plan[index][1])
        self.assertIn('--version-copy', plan[2][1])
        self.assertNotIn('--settings', plan[2][1])
        for row in plan[-2:]:
            self.assertIn('--browser-hook', row[1])
            self.assertIn('--cross-site-idp', row[1])
        self.assertNotIn('--dual-admin', plan[-2][1])
        self.assertIn('--dual-admin', plan[-1][1])

    def test_integration_rejects_missing_duplicate_legacy_patched_and_wrong_pair(self):
        for profile in h.PROFILES[:5]:
            self.assertGreater(h.validate_integration(integration(profile), profile, programs())['acceptanceGroups'], 0)
            for change in ('missing', 'duplicate', 'unsupported', 'patch', 'source', 'sdk', 'hash', 'cleanup', 'checks'):
                with self.subTest(profile=profile, change=change):
                    report = integration(profile)
                    if change == 'missing':
                        report['scenarios'].pop()
                    elif change == 'duplicate':
                        report['scenarios'][-1] = copy.deepcopy(report['scenarios'][0])
                    elif change == 'unsupported':
                        report['scope'] = 'relationship-iam-unsupported'
                    elif change == 'patch':
                        report['serverPatches'] = [{'name': 'patch'}]
                    elif change == 'source':
                        report['serverSource'] = 'f' * 40
                    elif change == 'sdk':
                        report['sdkPin'] = 'old'
                    elif change == 'hash':
                        report['binaries']['otterio']['sha256'] = 'f' * 64
                    elif change == 'cleanup':
                        report['scenarios'][0]['processCleanup']['processes'][0]['stopped'] = False
                    else:
                        report['scenarios'][0]['checks'] = []
                    with self.assertRaises(ValueError):
                        h.validate_integration(report, profile, programs())

    def test_positive_metrics_cannot_be_missing_or_legacy(self):
        for profile, key in [('storage', 'writeMetrics'), ('settings', 'settingsMetrics'),
                             ('features-version-copy', 'versionCopyMetrics'), ('native', 'nativeMetrics'),
                             ('relationship-iam', 'relationshipIAMMetrics')]:
            report = integration(profile)
            report['scenarios'][0][key] = {}
            with self.assertRaises(ValueError):
                h.validate_integration(report, profile, programs())
        report = integration('relationship-iam')
        report['scenarios'][0]['relationshipIAMMetrics']['positiveMutationCoverage'].pop()
        with self.assertRaises(ValueError):
            h.validate_integration(report, 'relationship-iam', programs())

    def test_protocol_rejects_skipped_named_tests_and_partial_range(self):
        self.assertEqual(len(h.validate_protocol(protocol(), programs())['protocolChecks']), 3)
        for change in ('missing', 'skip', 'range', 'kind', 'modified', 'source'):
            report = protocol()
            if change == 'missing':
                report['checks'].pop()
            elif change == 'skip':
                report['checks'][2]['reason'] = 'required-test-absent-or-skipped'
            elif change == 'range':
                report['checks'][0]['profiles'].pop()
            elif change == 'kind':
                report['server']['kind'] = 'release-asset'
            elif change == 'modified':
                report['server']['sourceState'] = 'modified'
            else:
                report['server']['source'] = 'f' * 40
            with self.subTest(change=change), self.assertRaises(ValueError):
                h.validate_protocol(report, programs())

    def test_oidc_requires_actual_cross_site_browser_refresh_fixed_expiry_and_independent_ca(self):
        for dual in (False, True):
            self.assertEqual(h.validate_oidc(oidc(dual), dual, programs())['browserChecks'], 11)
            for change in ('browser', 'skip', 'missing-check', 'duplicate-check', 'refresh', 'expiry', 'ca', 'cleanup', 'identity'):
                report = oidc(dual)
                if change == 'browser':
                    report.pop('browserAcceptance')
                elif change == 'skip':
                    report['browserAcceptance']['checks'][0]['status'] = 'skipped'
                elif change == 'missing-check':
                    report['browserAcceptance']['checks'].pop()
                elif change == 'duplicate-check':
                    report['browserAcceptance']['checks'].append(report['browserAcceptance']['checks'][0])
                elif change == 'refresh':
                    report['consoleRefreshEvidence']['keycloakEventsAfter'] = 1
                elif change == 'expiry':
                    report['consoleFixedExpiry'] = 'not run'
                elif change == 'ca':
                    report['certificateIdentities']['adminCAsha256'] = ('c' if dual else 'd') * 64
                elif change == 'cleanup':
                    report['cleanup'] = 'failed'
                else:
                    report['programs']['console']['binaryVCSModified'] = True
                with self.subTest(dual=dual, change=change), self.assertRaises(ValueError):
                    h.validate_oidc(report, dual, programs())

    def test_command_failures_run_remaining_profiles_and_never_export_output(self):
        with tempfile.TemporaryDirectory() as tmp:
            binary = Path(tmp) / 'binary'
            binary.write_bytes(b'fixture binary')
            output = Path(tmp) / 'pair.json'
            def build_info(command, **_):
                if command[0] == 'git':
                    return OC_SOURCE + '\n'
                # All paths are shared by the test; sources differ by iteration.
                build_info.calls += 1
                return json.dumps(info(h.SERVER_SOURCE if build_info.calls == 3 else OC_SOURCE, build_info.calls != 3))
            build_info.calls = 0
            with patch.object(h.subprocess, 'check_output', side_effect=build_info), patch.object(h, 'run', return_value=(127, 'private-password', .1)) as run:
                self.assertEqual(h.main(['--cli', str(binary), '--console', str(binary), '--server', str(binary),
                                         '--oc-source', OC_SOURCE, '--output', str(output)]), 1)
            self.assertEqual(run.call_count, 8)
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertFalse(report['releaseAssetCertification'])
            self.assertEqual(report['evidenceKind'], 'candidate-source-pair')
            self.assertTrue(all(row['status'] == 'failed' for row in report['checks']))
            self.assertNotIn('private-password', output.read_text())

    def test_structural_failure_omits_exception_messages_and_unknown_files(self):
        result = h.structural_failure('File "/private/password/test-console-integration.py", line 25\n'
                                      'File "/private/password/user.py", line 14\nAssertionError: fixture-secret')
        self.assertEqual(result, {'failureLocations': [{'file': 'test-console-integration.py', 'line': 25}],
                                  'exceptionClasses': ['AssertionError']})

    def test_complete_suite_binds_all_original_reports_and_only_then_passes(self):
        with tempfile.TemporaryDirectory() as tmp:
            binary = Path(tmp) / 'binary'
            binary.write_bytes(b'fixture binary')
            digest = h.sha256(binary)
            output = Path(tmp) / 'pair.json'
            def build_info(command, **_):
                if command[0] == 'git':
                    return OC_SOURCE + '\n'
                build_info.calls += 1
                return json.dumps(info(h.SERVER_SOURCE if build_info.calls == 3 else OC_SOURCE, build_info.calls != 3))
            build_info.calls = 0
            def fake_run(command, env, timeout):
                path = Path(command[command.index('--output') + 1])
                profile = path.stem.removeprefix('pair-')
                if profile == 'real-protocol':
                    child = protocol()
                    child['server']['binarySHA256'] = digest
                    child['clientBinarySHA256'] = {'cli': digest, 'console': digest}
                    path.with_name(path.stem + '-range.json').write_text(json.dumps({'profiles': child['checks'][0]['profiles']}))
                elif profile.startswith('oidc-browser-'):
                    child = oidc(profile.endswith('-dual'))
                    for row in child['programs'].values():
                        row['sha256'] = digest
                else:
                    child = integration(profile)
                    for row in child['binaries'].values():
                        row['sha256'] = digest
                path.write_text(json.dumps(child))
                return 0, 'private fixture output', .1
            with patch.object(h.subprocess, 'check_output', side_effect=build_info), patch.object(h, 'run', side_effect=fake_run):
                self.assertEqual(h.main(['--cli', str(binary), '--console', str(binary), '--server', str(binary),
                                         '--oc-source', OC_SOURCE, '--output', str(output)]), 0)
            report = json.loads(output.read_text())
            self.assertTrue(report['passed'])
            self.assertIs(h.validate_integration, h.EVIDENCE.validate_integration)
            self.assertIs(h.validate_protocol, h.EVIDENCE.validate_protocol)
            self.assertIs(h.validate_oidc, h.EVIDENCE.validate_oidc)
            self.assertEqual(report['harnesses']['buildscripts/console_pair_evidence.py'],
                             h.sha256(h.ROOT / 'buildscripts/console_pair_evidence.py'))
            self.assertEqual([row['name'] for row in report['checks']], h.PROFILES)
            for row in report['checks']:
                self.assertEqual(row['reportSHA256'], h.sha256(output.with_name(row['report'])))
            self.assertNotIn('private fixture output', output.read_text())

    def test_missing_preparation_binary_still_leaves_failed_candidate_summary(self):
        with tempfile.TemporaryDirectory() as tmp:
            output = Path(tmp) / 'pair.json'
            with patch.object(h.subprocess, 'check_output', side_effect=FileNotFoundError('private path')):
                self.assertEqual(h.main(['--cli', '/missing', '--console', '/missing', '--server', '/missing',
                                         '--oc-source', OC_SOURCE, '--output', str(output)]), 1)
            report = json.loads(output.read_text())
            self.assertFalse(report['passed'])
            self.assertEqual(report['preparationError'], 'FileNotFoundError')
            self.assertNotIn('private path', output.read_text())


if __name__ == '__main__':
    unittest.main()
