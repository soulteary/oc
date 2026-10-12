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

# Load only the reviewed sibling module, even when this runner is imported by path.
EVIDENCE_SPEC = importlib.util.spec_from_file_location('console_pair_evidence', ROOT / 'buildscripts/console_pair_evidence.py')
EVIDENCE = importlib.util.module_from_spec(EVIDENCE_SPEC)
EVIDENCE_SPEC.loader.exec_module(EVIDENCE)
SCENARIOS, TLS_SCENARIOS, PROFILES = EVIDENCE.SCENARIOS, EVIDENCE.TLS_SCENARIOS, EVIDENCE.PROFILES
BROWSER_CHECKS, RELATIONSHIP_COVERAGE = EVIDENCE.BROWSER_CHECKS, EVIDENCE.RELATIONSHIP_COVERAGE
require, check_hashes, cleanup_ok = EVIDENCE.require, EVIDENCE.check_hashes, EVIDENCE.cleanup_ok
validate_integration = EVIDENCE.validate_integration
validate_protocol = EVIDENCE.validate_protocol
validate_oidc = EVIDENCE.validate_oidc
SERVER_SOURCE = 'c3b3665154264993fa06a28e95ab25fdd03d7c0c'
HARNESS_PATHS = [
    'buildscripts/test-console-migration-pair.py', 'buildscripts/console_pair_evidence.py',
    'buildscripts/test-console-integration.py',
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


def sha256(path):
    with Path(path).open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def run(command, env, timeout):
    spec = importlib.util.spec_from_file_location('migration_protocol_runner', ROOT / 'buildscripts/test-real-protocol-ci.py')
    protocol = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(protocol)
    # The real-protocol child may itself need ten seconds to stop a detached group.
    return protocol.run(command, env, timeout, cleanup_timeout=30)


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
