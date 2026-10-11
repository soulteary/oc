#!/usr/bin/env python3
"""Run disposable protocol acceptance and retain only structural CI evidence."""
import argparse
import hashlib
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


def sha256(path):
    return hashlib.sha256(Path(path).read_bytes()).hexdigest()


def go_result(output, name):
    """Require the named test to run and pass; a skipped or absent test is failure."""
    events = []
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not isinstance(event, dict):
            continue
        if event.get('Test') == name and event.get('Action') in ('run', 'pass', 'fail', 'skip'):
            events.append(event['Action'])
    return 'run' in events and 'pass' in events and not any(x in events for x in ('skip', 'fail'))


def failed_tests(output):
    names = set()
    for line in output.splitlines():
        try:
            event = json.loads(line)
        except ValueError:
            continue
        if not isinstance(event, dict):
            continue
        name = event.get('Test', '')
        if event.get('Action') == 'fail' and isinstance(name, str) and re.fullmatch(r'[A-Za-z0-9_/]+', name):
            names.add(name)
    return sorted(names)


def failure_locations(output):
    allowed = {'test-console-range-real.py', 'test-console-range.py', 'test-console-diagnostics.py', 'test-console-integration.py'}
    result = []
    for filename, line in re.findall(r'File "([^"\n]+)", line ([0-9]+)', output):
        name = Path(filename).name
        if name in allowed:
            result.append({'file': name, 'line': int(line)})
    return result


def run(command, env, timeout):
    start = time.monotonic()
    # A separate process group contains wrapper-owned Go tests and fixture servers.
    try:
        process = subprocess.Popen(command, cwd=ROOT, env=env, stdout=subprocess.PIPE,
                                   stderr=subprocess.STDOUT, text=True, start_new_session=True)
    except OSError:
        return 127, '', round(time.monotonic() - start, 3)
    try:
        output, _ = process.communicate(timeout=timeout)
        return process.returncode, output, round(time.monotonic() - start, 3)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.communicate(timeout=10)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()
        return 124, '', round(time.monotonic() - start, 3)
    finally:
        # Also reap fixture descendants after a child exits unexpectedly.
        try:
            os.killpg(process.pid, signal.SIGKILL)
        except ProcessLookupError:
            pass
        if process.poll() is None:
            process.wait(timeout=10)


def main():
    parser = argparse.ArgumentParser()
    for name in ('cli', 'console', 'server', 'server-source', 'output'):
        parser.add_argument('--' + name, required=True)
    parser.add_argument('--source-kind', choices=('module-build', 'release-asset', 'patched-fixture'), required=True)
    parser.add_argument('--source-state', choices=('clean', 'modified', 'unknown'), required=True)
    args = parser.parse_args()
    report_path = Path(args.output).resolve()
    report_path.parent.mkdir(parents=True, exist_ok=True)
    report = {'schemaVersion': 1, 'platform': platform.system() + '/' + platform.machine(),
              'server': {'source': args.server_source, 'kind': args.source_kind,
                         'sourceState': args.source_state, 'sourceBinding': 'caller-declared; binary hash recorded separately'},
              'cgo': os.environ.get('CGO_ENABLED', 'unspecified'),
              'checkoutSHA': os.environ.get('GITHUB_SHA', 'local-unpublished'),
              'harnesses': {}, 'checks': [], 'passed': False}
    # Persist before preparation so a missing binary or invalid argument still leaves evidence.
    def save():
        report_path.write_text(json.dumps(report, indent=2) + '\n')
    save()
    try:
        if not re.fullmatch(r'[0-9a-f]{40}', args.server_source):
            raise ValueError('invalid-source')
        paths = {name: str(Path(getattr(args, name)).resolve()) for name in ('cli', 'console', 'server')}
        report['server']['binarySHA256'] = sha256(paths['server'])
        report['clientBinarySHA256'] = {name: sha256(paths[name]) for name in ('cli', 'console')}
        for name in ('test-real-protocol-ci.py', 'test-console-range-real.py', 'test-console-range.py',
                     'test-console-integration.py', 'test-console-diagnostics.py'):
            report['harnesses'][name] = sha256(ROOT / 'buildscripts' / name)
        report['harnesses']['sts_integration_test.go'] = sha256(ROOT / 'internal/oidc/sts_integration_test.go')
        report['harnesses']['diagnostics_test.go'] = sha256(ROOT / 'internal/storageclient/diagnostics_test.go')
        env = {k: v for k, v in os.environ.items() if not k.startswith(('OC_', 'OTTERIO_'))}
        env.update(NO_PROXY='*', no_proxy='*', PYTHONDONTWRITEBYTECODE='1')
        range_path = report_path.with_name(report_path.stem + '-range.json')
        range_path.unlink(missing_ok=True)
        commands = [
            ('range', [sys.executable, 'buildscripts/test-console-range-real.py', '--cli', paths['cli'],
                       '--console', paths['console'], '--server', paths['server'], '--server-source',
                       args.server_source, '--output', str(range_path)], 240),
            ('diagnostics', [sys.executable, 'buildscripts/test-console-diagnostics.py', '--server', paths['server']], 120),
            ('oidc-sts', ['go', 'test', '-json', './internal/oidc', '-run', '^TestOtterIOSTSIntegration$',
                          '-count=1', '-timeout=90s'], 120),
        ]
        env.update(OC_OIDC_TEST_SERVER=paths['server'], OC_OIDC_TEST_SERVER_SOURCE=args.server_source)
        for name, command, timeout in commands:
            code, output, elapsed = run(command, env, timeout)
            detail = {'name': name, 'exitCode': code, 'elapsedSeconds': elapsed,
                      'status': 'failed', 'reason': 'timeout' if code == 124 else 'command-start-failed' if code == 127 else 'command-failed'}
            if code == 0:
                if name == 'range':
                    try:
                        result = json.loads(range_path.read_text())
                        profiles = result['profiles']
                        valid = {p['profile'] for p in profiles} == {'single-http', 'dual-http', 'dual-tls'} and len(profiles) == 3
                        valid = valid and all(p['historicalVersionStatus'] == 206 and p['restrictedHistoricalStatus'] == 403 for p in profiles)
                        if valid:
                            detail.update(status='passed', reason='all-profiles-and-version-denial', profiles=[{key: p[key] for key in ('profile', 'checks', 'fixtureBytes', 'sha256', 'emptyObjectChecks', 'nullVersionStatus', 'historicalVersionStatus', 'restrictedCurrentStatus', 'restrictedHistoricalStatus', 'restrictedConditionalFullStatus', 'restrictedIfRangeFallbackStatus')} for p in profiles])
                    except (OSError, ValueError, KeyError, TypeError):
                        pass
                else:
                    test = 'TestDiagnosticsRealServer' if name == 'diagnostics' else 'TestOtterIOSTSIntegration'
                    if go_result(output, test):
                        detail.update(status='passed', reason='named-test-ran-and-passed', test=test)
                    else:
                        detail['reason'] = 'required-test-absent-or-skipped'
            # Never persist stdout, stderr, request bodies or fixture logs.
            if detail['status'] != 'passed':
                if name == 'range':
                    try:
                        failure = json.loads(range_path.read_text()).get('failure', {})
                        if failure.get('profile') in ('single-http', 'dual-http', 'dual-tls') and failure.get('phase') in (
                                'server-startup', 'fixture-setup', 'console-startup', 'range-conditions', 'empty-object',
                                'historical-version', 'restricted-user-setup', 'restricted-console-startup', 'restricted-downloads'):
                            detail['fixtureFailure'] = {key: failure[key] for key in ('profile', 'phase')}
                            if failure.get('exceptionClass') in ('AssertionError', 'RuntimeError', 'TimeoutError', 'FileNotFoundError', 'CalledProcessError', 'TimeoutExpired'):
                                detail['fixtureFailure']['exceptionClass'] = failure['exceptionClass']
                    except (OSError, ValueError, AttributeError):
                        pass
                detail['failedTests'] = failed_tests(output)
                detail['failureLocations'] = failure_locations(output)
                detail['exceptionClasses'] = sorted(set(re.findall(r'^(AssertionError|RuntimeError|TimeoutError|FileNotFoundError|subprocess[.]CalledProcessError|subprocess[.]TimeoutExpired):', output, re.MULTILINE)))
            report['checks'].append(detail)
            save()
            print(f"{name}: {detail['status']} ({detail['reason']}, {elapsed}s)", flush=True)
        report['passed'] = len(report['checks']) == 3 and all(c['status'] == 'passed' for c in report['checks'])
    except KeyboardInterrupt:
        report['interrupted'] = True
    except (OSError, ValueError) as error:
        report['preparationError'] = type(error).__name__
    finally:
        save()
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    def interrupted(*_):
        raise KeyboardInterrupt
    signal.signal(signal.SIGTERM, interrupted)
    sys.exit(main())
