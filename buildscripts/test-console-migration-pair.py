#!/usr/bin/env python3
"""Verify one immutable, unpatched candidate pair with independent fixtures.

Accepts existing clean Go binaries. Docker, OpenSSL, Go and a configured real
Playwright Chromium are required; missing dependencies or profiles fail closed.
This report is candidate evidence, never published release-asset certification.
Only structural diagnostics are retained; subprocess output stays in memory.
"""
import argparse
import datetime
import hashlib
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import signal
import subprocess
import sys
import time

ROOT = Path(__file__).resolve().parents[1]
SERVER_SOURCE = 'c3b3665154264993fa06a28e95ab25fdd03d7c0c'
SCENARIOS = ['single-http', 'dual-http', 'single-tls', 'dual-tls']
TLS_SCENARIOS = ['single-tls', 'dual-tls']
PROFILES = ['storage', 'settings', 'features-version-copy', 'native',
            'relationship-iam', 'real-protocol', 'oidc-browser-single', 'oidc-browser-dual']
HARNESS_PATHS = [
    'buildscripts/test-console-migration-pair.py', 'buildscripts/test-console-integration.py',
    'buildscripts/console_acceptance_protocol.py', 'buildscripts/local_http.py',
    'buildscripts/console_write_acceptance.py', 'buildscripts/console_settings_acceptance.py',
    'buildscripts/console_features_acceptance.py', 'buildscripts/version_copy_acceptance.py',
    'buildscripts/console_native_acceptance.py', 'buildscripts/console_relationship_iam_acceptance.py',
    'buildscripts/test-real-protocol-ci.py', 'buildscripts/test-console-range-real.py',
    'buildscripts/test-console-range.py', 'buildscripts/test-console-diagnostics.py',
    'buildscripts/console_oidc_keycloak_acceptance.py',
    'buildscripts/console_oidc_browser_acceptance.py', 'buildscripts/console_oidc_browser_acceptance.cjs',
    'internal/oidc/sts_integration_test.go', 'internal/storageclient/diagnostics_test.go',
]
BROWSER_CHECKS = {'different_sites', 'alice_binding_cookie_flags', 'bob_binding_cookie_flags',
                  'alice_cross_site_top_level_callback_binding', 'bob_cross_site_top_level_callback_binding',
                  'alice_ui_session', 'bob_ui_session', 'alice_ui_logout', 'bob_logout_isolation',
                  'bob_session_after_initial_token_expiry', 'fixed_deadline_401'}
RELATIONSHIP_COVERAGE = {'user.lifecycle-parent-service-sts-cascade', 'user.lifecycle-self',
                         'user.rotate-other', 'user.rotate-self', 'disabled-user.rotate',
                         'user.policies', 'group.policies', 'group.status-members-delete',
                         'service-account.status-secret-policy-delete'}


def sha256(path):
    with Path(path).open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def run(command, env, timeout):
    spec = importlib.util.spec_from_file_location('migration_protocol_runner', ROOT / 'buildscripts/test-real-protocol-ci.py')
    protocol = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(protocol)
    # The real-protocol child may itself need ten seconds to stop a detached group.
    return protocol.run(command, env, timeout, cleanup_timeout=30)


def require(condition, reason):
    if not condition:
        raise ValueError(reason)


def binary_identity(info, source, digest, client=False):
    """Binary-derived revision is mandatory; caller labels cannot certify it."""
    require(re.fullmatch(r'[0-9a-f]{40}', source) is not None, 'invalid-source')
    settings = {row['Key']: row.get('Value', '') for row in info.get('Settings', [])}
    require(settings.get('vcs.revision') == source, 'binary-source-mismatch')
    require(settings.get('vcs.modified') == 'false', 'binary-source-not-clean')
    dependencies = info.get('Deps', [])
    require(not any('Replace' in row for row in dependencies), 'binary-dependency-replacement')
    require(settings.get('GOOS') and settings.get('GOARCH'), 'binary-platform-missing')
    identity = {'source': source, 'binarySHA256': digest, 'binaryVCSRevision': source,
                'binaryVCSModified': False, 'sourceKind': 'unpatched-git-build',
                'platform': settings['GOOS'] + '/' + settings['GOARCH']}
    if client:
        sdk = [row for row in dependencies if row.get('Path') == 'github.com/soulteary/otterio']
        require(len(sdk) == 1 and sdk[0].get('Version'), 'client-sdk-missing')
        identity['sdkPin'] = sdk[0]['Version']
    return identity


def check_hashes(report_hashes, programs):
    require(report_hashes == {key: value['binarySHA256'] for key, value in programs.items()},
            'child-report-binary-mismatch')


def cleanup_ok(value):
    return (isinstance(value, dict) and value.get('status') == 'passed'
            and value.get('errors') == [] and isinstance(value.get('processes'), list)
            and bool(value['processes']) and all(row.get('stopped') is True for row in value['processes']))


def validate_integration(report, profile, programs):
    scopes = {'storage': 'read-only-and-writes', 'settings': 'read-only-and-protected-settings',
              'features-version-copy': 'console-five-features-and-version-copy',
              'native': 'native-iam-https-read-only', 'relationship-iam': 'relationship-iam-native'}
    require(report.get('status') == 'passed', 'fixture-not-passed')
    require(report.get('scope') == scopes[profile], 'wrong-or-unsupported-profile')
    require(not report.get('serverPatches'), 'patched-fixture-not-allowed')
    require(report.get('serverSource') == programs['server']['source'], 'child-report-source-mismatch')
    require(report.get('sdkPin') == programs['oc']['sdkPin'], 'child-report-sdk-mismatch')
    check_hashes({{'oc': 'oc', 'oc-console': 'console', 'otterio': 'server'}[key]: row['sha256']
                  for key, row in report['binaries'].items()}, programs)
    require(report.get('identityEvidence') == {'sdkPin': 'verified-client-build-info',
                                             'serverSource': 'verified-vcs-revision'}, 'child-identity-not-clean')
    require(report.get('storage') == 'single-node-four-disk-erasure', 'wrong-storage-fixture')
    scenarios = report.get('scenarios', [])
    expected = TLS_SCENARIOS if profile in ('native', 'relationship-iam') else SCENARIOS
    names = [row['scenario'] for row in scenarios]
    require(len(names) == len(expected) and set(names) == set(expected), 'missing-or-duplicate-scenarios')
    for row in scenarios:
        require(row.get('status') == 'passed' and cleanup_ok(row.get('processCleanup')), 'scenario-or-cleanup-failed')
        checks = row.get('checks', [])
        require(isinstance(checks, list) and len(checks) >= 10
                and all(isinstance(item, str) and item for item in checks), 'scenario-checks-missing')
        if profile == 'storage':
            require(isinstance(row.get('writeMetrics'), dict) and bool(row['writeMetrics']), 'write-evidence-missing')
        elif profile == 'settings':
            require(report.get('settingsProfile') == 'v1-protected-settings'
                    and row.get('settingsMetrics', {}).get('profile') == 'v1-protected-settings', 'settings-profile-unsupported')
        elif profile == 'features-version-copy':
            require(isinstance(row.get('featureMetrics'), dict) and bool(row['featureMetrics']), 'feature-evidence-missing')
            metric = row.get('versionCopyMetrics', {})
            require(metric.get('profile') == 'copy-source-version-authorization'
                    and metric.get('confirmedCopies', 0) > 0 and metric.get('accessDeniedWithoutDestination', 0) > 0,
                    'version-copy-evidence-missing')
        elif profile == 'native':
            metric = row.get('nativeMetrics', {})
            require(metric.get('iamCreation') == 'native user/group/service-account; permission and duplicate-target checks verified'
                    and metric.get('browserTLS') == 'verified' and metric.get('storageTLS') == 'verified'
                    and metric.get('rootLogin') == 'denied', 'native-profile-unsupported')
        else:
            metric = row.get('relationshipIAMMetrics', {})
            coverage = metric.get('positiveMutationCoverage', [])
            require(metric.get('capabilityMode') == 'required-supported'
                    and set(coverage) == RELATIONSHIP_COVERAGE and len(coverage) == len(RELATIONSHIP_COVERAGE)
                    and cleanup_ok(metric.get('processCleanup')), 'relationship-positive-evidence-missing')
    return {'scenarios': names, 'acceptanceGroups': sum(len(row['checks']) for row in scenarios)}


def validate_protocol(report, programs):
    require(report.get('schemaVersion') == 1 and report.get('passed') is True, 'protocol-not-passed')
    server = report['server']
    require(server.get('source') == programs['server']['source'] and server.get('kind') == 'module-build'
            and server.get('sourceState') == 'clean', 'protocol-source-mismatch')
    check_hashes({'oc': report['clientBinarySHA256']['cli'], 'console': report['clientBinarySHA256']['console'],
                  'server': server['binarySHA256']}, programs)
    checks = report.get('checks', [])
    require(len(checks) == 3 and {row['name'] for row in checks} == {'range', 'diagnostics', 'oidc-sts'}
            and all(row.get('status') == 'passed' and row.get('exitCode') == 0 for row in checks), 'protocol-skipped-or-failed')
    for row in checks:
        if row['name'] != 'range':
            expected = 'TestDiagnosticsRealServer' if row['name'] == 'diagnostics' else 'TestOtterIOSTSIntegration'
            require(row.get('reason') == 'named-test-ran-and-passed' and row.get('test') == expected,
                    'protocol-test-absent-or-skipped')
        else:
            profiles = row.get('profiles', [])
            require(len(profiles) == 3 and {p['profile'] for p in profiles} == {'single-http', 'dual-http', 'dual-tls'}
                    and all(p.get('historicalVersionStatus') == 206 and p.get('restrictedHistoricalStatus') == 403
                            for p in profiles), 'range-profile-missing')
    return {'protocolChecks': [row['name'] for row in checks]}


def validate_oidc(report, dual, programs):
    require(report.get('status') == 'passed' and report.get('cleanup') == 'passed', 'oidc-or-cleanup-failed')
    for name, identity in programs.items():
        child = report['programs'][name]
        require(child.get('declaredSource') == identity['source'] and child.get('binaryVCSRevision') == identity['source']
                and child.get('binaryVCSModified') is False and child.get('sha256') == identity['binarySHA256'],
                'oidc-program-mismatch')
    require(report.get('storageTopology') == ('dual HTTPS S3/Admin listeners' if dual else 'single HTTPS S3/Admin listener'),
            'oidc-topology-missing')
    require(report.get('browserSiteTopology') == 'localhost IdP and 127.0.0.1 Console (different sites)', 'oidc-cross-site-missing')
    ca = report['certificateIdentities']
    require(bool(re.fullmatch(r'[0-9a-f]{64}', ca['primaryCAsha256']))
            and bool(re.fullmatch(r'[0-9a-f]{64}', ca['adminCAsha256']))
            and (ca['primaryCAsha256'] != ca['adminCAsha256']) == dual, 'oidc-ca-topology-mismatch')
    checks = report.get('checks', [])
    require(isinstance(checks, list) and len(checks) >= (14 if dual else 13)
            and 'Bob saved pre-expiry cookie is rejected after fixed TTL despite successful refresh' in checks,
            'oidc-fixed-expiry-not-run')
    if dual:
        require('independent Admin HTTPS CA accepted; S3 primary CA is rejected for Admin listener' in checks, 'oidc-wrong-ca-not-tested')
    refresh = report['consoleRefreshEvidence']
    require(refresh['waitSeconds'] > refresh['idTokenSeconds']
            and refresh['keycloakEventsAfter'] > refresh['keycloakEventsBefore'], 'oidc-provider-refresh-not-observed')
    browser = report.get('browserAcceptance', {})
    require(browser.get('status') == 'passed' and browser.get('persistentProfile') is False
            and browser.get('successfulRefreshEventsDelta', 0) > 0 and bool(browser.get('browser')), 'real-browser-missing')
    rows = browser.get('checks', [])
    names = [row['name'] for row in rows]
    require(BROWSER_CHECKS.issubset(names) and len(names) == len(set(names))
            and all(row.get('status') == 'passed' for row in rows), 'browser-check-absent-or-skipped')
    require(str(report.get('consoleAcceptance', '')).startswith('passed:')
            and report.get('consoleFixedExpiryReference') and 'consoleFixedExpiry' not in report, 'console-profile-not-run')
    return {'browserChecks': len(rows), 'protocolChecks': len(checks), 'browserVersion': browser['browser'],
            'keycloakImage': report['keycloak']['image']}


def commands(args, paths, programs, report_path):
    result = []
    for profile, flags in [('storage', ['--writes']), ('settings', ['--settings']),
                           ('features-version-copy', ['--features', '--version-copy']),
                           ('native', ['--native']), ('relationship-iam', ['--relationship-iam'])]:
        output = report_path.with_name(report_path.stem + '-' + profile + '.json')
        scenarios = TLS_SCENARIOS if profile in ('native', 'relationship-iam') else SCENARIOS
        command = [sys.executable, 'buildscripts/test-console-integration.py', '--cli', paths['oc'],
                   '--console', paths['console'], '--server', paths['server'], '--server-source', args.server_source,
                   '--sdk-pin', programs['oc']['sdkPin'], '--scenarios', ','.join(scenarios), '--output', str(output), *flags]
        result.append((profile, command, output, 900))
    output = report_path.with_name(report_path.stem + '-real-protocol.json')
    result.append(('real-protocol', [sys.executable, 'buildscripts/test-real-protocol-ci.py',
                   '--cli', paths['oc'], '--console', paths['console'], '--server', paths['server'],
                   '--server-source', args.server_source, '--source-kind', 'module-build', '--source-state', 'clean',
                   '--output', str(output)], output, 600))
    for dual in (False, True):
        profile = 'oidc-browser-dual' if dual else 'oidc-browser-single'
        output = report_path.with_name(report_path.stem + '-' + profile + '.json')
        command = [sys.executable, 'buildscripts/console_oidc_keycloak_acceptance.py', '--server', paths['server'],
                   '--server-source', args.server_source, '--oc', paths['oc'], '--oc-source', args.oc_source,
                   '--console', paths['console'], '--console-source', args.oc_source,
                   '--browser-hook', str(ROOT / 'buildscripts/console_oidc_browser_acceptance.py'), '--cross-site-idp',
                   '--console-session-ttl', '150s', '--console-refresh-wait', '95', '--output', str(output)]
        if dual:
            command.append('--dual-admin')
        result.append((profile, command, output, 900))
    return result


def structural_failure(output):
    allowed = {Path(name).name for name in HARNESS_PATHS if name.endswith('.py')}
    return {'failureLocations': [{'file': Path(name).name, 'line': int(line)}
                                for name, line in re.findall(r'File "([^"\n]+)", line ([0-9]+)', output)
                                if Path(name).name in allowed],
            'exceptionClasses': sorted(set(re.findall(
                r'^(AssertionError|RuntimeError|ValueError|TimeoutError|FileNotFoundError|subprocess[.]CalledProcessError):',
                output, re.MULTILINE)))}


def main(argv=None):
    parser = argparse.ArgumentParser(description=__doc__)
    for name in ('cli', 'console', 'server', 'oc-source', 'output'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--server-source', default=SERVER_SOURCE)
    parser.add_argument('--timeout', type=int, default=2700, help='whole-suite deadline in seconds; never skips required profiles')
    args = parser.parse_args(argv)
    report_path = Path(args.output).resolve()
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report = {'schemaVersion': 1, 'evidenceKind': 'candidate-source-pair', 'releaseAssetCertification': False,
              'dateUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'platform': platform.system() + '/' + platform.machine(), 'programs': {}, 'harnesses': {},
              'requiredProfiles': PROFILES, 'checks': [], 'passed': False,
              'scope': 'single-node storage; direct HTTPS native/OIDC; Chromium; no production or release asset claim'}
    def save():
        report_path.write_text(json.dumps(report, indent=2) + '\n')
    save()
    start = time.monotonic()
    try:
        require(args.timeout > 0, 'invalid-deadline')
        paths = {'oc': str(Path(args.cli).resolve()), 'console': str(Path(args.console).resolve()),
                 'server': str(Path(args.server).resolve())}
        for name, path in paths.items():
            info = json.loads(subprocess.check_output(['go', 'version', '-m', '-json', path], text=True,
                                                     stderr=subprocess.DEVNULL, timeout=30))
            report['programs'][name] = binary_identity(info, args.server_source if name == 'server' else args.oc_source,
                                                     sha256(path), client=name != 'server')
        programs = report['programs']
        require(programs['oc']['sdkPin'] == programs['console']['sdkPin'], 'client-sdk-mismatch')
        require(len({row['platform'] for row in programs.values()}) == 1, 'program-platform-mismatch')
        report['harnesses'] = {name: sha256(ROOT / name) for name in HARNESS_PATHS}
        report['sourceCheckout'] = {'revision': subprocess.check_output(['git', 'rev-parse', 'HEAD'], cwd=ROOT, text=True).strip(),
                                   'binding': 'program revisions verified separately; exact harness content hashed'}
        env = {key: value for key, value in os.environ.items() if not key.startswith(('OC_', 'MC_', 'OTTERIO_'))}
        env.update(NO_PROXY='*', no_proxy='*', PYTHONDONTWRITEBYTECODE='1')
        # Browser overrides are required on Linux and portable elsewhere; no user profile is read.
        for name in ('OC_BROWSER_NODE', 'OC_BROWSER_PLAYWRIGHT_MODULE', 'OC_BROWSER_EXECUTABLE'):
            if os.environ.get(name):
                env[name] = os.environ[name]
        for profile, command, output_path, timeout in commands(args, paths, programs, report_path):
            output_path.unlink(missing_ok=True)
            if profile == 'real-protocol':
                output_path.with_name(output_path.stem + '-range.json').unlink(missing_ok=True)
            detail = {'name': profile, 'status': 'failed', 'report': output_path.name}
            report['checks'].append(detail)
            save()
            try:
                require(all(sha256(paths[name]) == row['binarySHA256'] for name, row in programs.items()), 'program-changed-during-suite')
                require(all(sha256(ROOT / name) == digest for name, digest in report['harnesses'].items()), 'harness-changed-during-suite')
                remaining = args.timeout - (time.monotonic() - start)
                require(remaining > 0, 'whole-suite-deadline-exceeded')
                code, output, elapsed = run(command, env, min(timeout, remaining))
                detail.update(exitCode=code, elapsedSeconds=elapsed)
                if output_path.is_file():
                    detail['reportSHA256'] = sha256(output_path)
                if code != 0:
                    detail.update(structural_failure(output))
                    raise ValueError('fixture-timeout' if code == 124 else 'fixture-start-failed' if code == 127 else 'fixture-command-failed')
                child = json.loads(output_path.read_text())
                if profile == 'real-protocol':
                    detail.update(validate_protocol(child, programs))
                    extra = output_path.with_name(output_path.stem + '-range.json')
                    require(extra.is_file(), 'original-range-report-missing')
                    detail['additionalReports'] = [{'report': extra.name, 'reportSHA256': sha256(extra)}]
                elif profile.startswith('oidc-browser-'):
                    detail.update(validate_oidc(child, profile.endswith('-dual'), programs))
                else:
                    detail.update(validate_integration(child, profile, programs))
                require(all(sha256(paths[name]) == row['binarySHA256'] for name, row in programs.items()), 'program-changed-during-suite')
                require(all(sha256(ROOT / name) == digest for name, digest in report['harnesses'].items()), 'harness-changed-during-suite')
                detail.update(status='passed', reason='required-positive-profile-and-identity-verified')
            except (OSError, ValueError, KeyError, TypeError) as error:
                detail['reason'] = str(error) if isinstance(error, ValueError) and re.fullmatch('[a-z-]+', str(error)) else 'invalid-or-missing-fixture-evidence'
                detail['exceptionClass'] = type(error).__name__
            save()
            print(profile + ': ' + detail['status'] + ' (' + detail['reason'] + ')', flush=True)
        report['passed'] = [row['name'] for row in report['checks']] == PROFILES and all(row['status'] == 'passed' for row in report['checks'])
    except KeyboardInterrupt:
        report['interrupted'] = True
    except (OSError, ValueError, KeyError, TypeError, subprocess.SubprocessError) as error:
        report['preparationError'] = type(error).__name__
        if isinstance(error, ValueError) and re.fullmatch('[a-z-]+', str(error)):
            report['preparationReason'] = str(error)
    finally:
        report['elapsedSeconds'] = round(time.monotonic() - start, 3)
        save()
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(KeyboardInterrupt()))
    sys.exit(main())
