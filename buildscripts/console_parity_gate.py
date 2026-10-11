#!/usr/bin/env python3
"""Fail-closed retirement evidence gate; never executes or retries acceptance."""
import argparse
import json
import re
from pathlib import Path

HEX40 = re.compile(r'[0-9a-f]{40}')
HEX64 = re.compile(r'[0-9a-f]{64}')


def normalize(report, source, binary):
    """Supported: test-real-protocol-ci.py schemaVersion=1 only.

    Legacy integration reports with natural-language checks remain historical
    evidence, not capability certification. Producers may add stable named checks
    using the same structural schema; gate callers bind exact source and binary.
    """
    errors = []
    if report.get('schemaVersion') != 1:
        return {}, ['unsupported-evidence-schema']
    server = report.get('server', {})
    if not isinstance(server, dict):
        return {}, ['invalid-server']
    if server.get('source') != source or not HEX40.fullmatch(source):
        errors.append('source-mismatch')
    if server.get('binarySHA256') != binary or not HEX64.fullmatch(binary):
        errors.append('binary-mismatch')
    if server.get('sourceState') != 'clean':
        errors.append('source-not-clean')
    if server.get('kind') != 'release-asset':
        errors.append('not-release-asset')
    if not report.get('platform'):
        errors.append('missing-platform')
    harnesses = report.get('harnesses', {})
    if not isinstance(harnesses, dict) or not harnesses or any(not isinstance(v, str) or not HEX64.fullmatch(v) for v in harnesses.values()):
        errors.append('missing-harness-digests')
    if report.get('passed') is not True:
        errors.append('report-not-passed')
    checks = report.get('checks', [])
    if not isinstance(checks, list):
        return {}, errors + ['invalid-checks']
    indexed = {}
    for check in checks:
        if not isinstance(check, dict) or not isinstance(check.get('name'), str):
            errors.append('invalid-check')
            continue
        name = check['name']
        if name in indexed:
            errors.append('duplicate-check:' + name)
        indexed[name] = check
        if check.get('status') != 'passed' or check.get('exitCode') != 0 or check.get('failedTests') or check.get('skippedTests') or check.get('skip'):
            errors.append('check-not-passed:' + name)
        for profile in check.get('profiles', []):
            if not isinstance(profile, dict) or profile.get('status', 'passed') != 'passed' or profile.get('skip'):
                errors.append('profile-not-passed:' + name)
    return indexed, errors


def evaluate(matrix, report, source, binary):
    checks, errors = normalize(report, source, binary)
    if matrix.get('schemaVersion') != 1 or not isinstance(matrix.get('capabilities'), list) or not matrix['capabilities']:
        return {'ready': False, 'errors': errors + ['invalid-matrix']}
    seen = set()
    gaps = []
    for row in matrix['capabilities']:
        name = row.get('id')
        if not isinstance(name, str) or name in seen:
            errors.append('invalid-or-duplicate-capability')
            continue
        seen.add(name)
        missing = []
        if row.get('status') != 'verified-replacement':
            missing.append('unverified-scope')
        required = row.get('requiredChecks')
        if not isinstance(required, list) or not required:
            missing.append('missing-check-contract')
        else:
            missing.extend('missing-check:' + check for check in required if check not in checks)
        for check in required or []:
            item = checks.get(check, {})
            actual = {p.get('profile') for p in item.get('profiles', []) if isinstance(p, dict)}
            missing.extend('missing-profile:' + p for p in row.get('requiredProfiles', []) if p not in actual)
        if missing:
            gaps.append({'id': name, 'gaps': missing})
    return {'ready': not errors and not gaps, 'errors': errors, 'capabilities': gaps}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    for arg in ('matrix', 'report', 'server-source', 'server-sha256'):
        parser.add_argument('--' + arg, required=True)
    args = parser.parse_args()
    try:
        result = evaluate(json.loads(Path(args.matrix).read_text()), json.loads(Path(args.report).read_text()), args.server_source, args.server_sha256)
    except (OSError, ValueError, TypeError, AttributeError) as exc:
        result = {'ready': False, 'errors': ['invalid-input:' + type(exc).__name__]}
    print(json.dumps(result, indent=2))
    return 0 if result['ready'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
