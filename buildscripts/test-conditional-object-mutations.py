#!/usr/bin/env python3
# Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
"""Test conditional console mutations against disposable local OtterIO processes."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import re
import shutil
import subprocess
import sys
import tempfile
import time


SCENARIOS = (
    'large-create', 'large-create-target-race', 'rename-metadata-tags',
    'rename-multipart-source', 'rename-target-race',
    'rename-source-before-copy', 'rename-source-before-delete',
)
TEST_NAME = 'TestLiveConditionalUploadAndRename'


def source_identity(directory):
    def git(*args):
        return subprocess.check_output(['git', *args], cwd=directory, text=True).strip()
    return {'commit': git('rev-parse', 'HEAD'), 'tree': git('rev-parse', 'HEAD^{tree}'),
            'dirty': bool(git('status', '--porcelain'))}


def sha256(path):
    with path.open('rb') as handle:
        return hashlib.file_digest(handle, 'sha256').hexdigest()


def main():
    scripts = Path(__file__).resolve().parent
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--oc', type=Path, default=scripts.parent,
                        help='OC worktree containing both upload and rename fixes (default: this repository)')
    parser.add_argument('--server', type=Path, required=True,
                        help='Already-built local server binary with conditional copy and delete support')
    parser.add_argument('--server-source', type=Path, required=True,
                        help='Server worktree used to build that binary, recorded in the report')
    parser.add_argument('--server-overlay', type=Path,
                        help='Optional build overlay, recorded as a verification limitation')
    parser.add_argument('--go', help='Go compiler executable (default: go from PATH)')
    parser.add_argument('--race', action='store_true', help='Enable the race detector in the OC test process')
    parser.add_argument('--output-dir', type=Path,
                        help='Directory for logs and report (default: a new temporary directory)')
    args = parser.parse_args()
    executable = shutil.which(args.go or 'go')
    if executable is None:
        parser.error('Go compiler not found; put the required toolchain on PATH or pass --go.')
    go = Path(executable).resolve()
    oc, binary, server_source = args.oc.resolve(), args.server.resolve(), args.server_source.resolve()
    if not binary.is_file() or not server_source.is_dir() or not (oc / 'go.mod').is_file():
        parser.error('Provide an existing OC worktree, local server binary and its source worktree.')
    module = (oc / 'go.mod').read_text()
    pin = re.search(r'^\s*github\.com/soulteary/otterio-sdk/v7\s+(\S+)', module, re.M)
    if not pin or re.search(r'^replace\b', module, re.M):
        parser.error('The OC worktree must use its declared storage SDK dependency without module replacements.')
    if args.server_overlay and not args.server_overlay.is_file():
        parser.error('The supplied server build overlay does not exist.')
    fixture = scripts / 'testdata' / 'conditional_object_mutations_live_test.go'
    if not fixture.is_file():
        parser.error('The conditional mutations Go fixture is missing.')
    output = args.output_dir.resolve() if args.output_dir else Path(tempfile.mkdtemp(prefix='oc-conditional-mutations-'))
    output.mkdir(parents=True, exist_ok=True)
    overlay = output / 'overlay.json'
    overlay.write_text(json.dumps({'Replace': {str(oc / 'internal/storageclient/conditional_object_mutations_live_test.go'): str(fixture)}}, indent=2)+'\n')
    env = dict(os.environ, GOTOOLCHAIN='local', GOMAXPROCS='2', OC_CONDITIONAL_SERVER_BINARY=str(binary))
    command = [str(go), 'test', '-mod=readonly', '-p', '2', '-count=1', '-timeout=8m', '-json', '-overlay='+str(overlay)]
    if args.race:
        command.append('-race')
    command.extend(['-run', '^'+TEST_NAME+'$', './internal/storageclient'])
    report = {
        'scope': 'Real OC Client/SDK HTTP through an observing proxy to disposable FS and local four-disk erasure servers; not packaged release acceptance',
        'storageSDK': pin.group(1), 'ocSource': source_identity(oc),
        'serverSource': source_identity(server_source), 'serverBinarySHA256': sha256(binary),
        'testFixtureSHA256': sha256(fixture), 'command': command, 'clientRace': args.race,
        'status': 'running',
    }
    if args.server_overlay:
        report['serverBuildOverlay'] = str(args.server_overlay.resolve())
        report['serverBuildOverlaySHA256'] = sha256(args.server_overlay)
    report_path, log_path = output / 'report.json', output / 'tests.jsonl'
    report_path.write_text(json.dumps(report, indent=2)+'\n')
    print('Running disposable local integration; logs:', log_path, flush=True)
    start = time.monotonic()
    with log_path.open('w') as log:
        result = subprocess.run(command, cwd=oc, env=env, stdout=log, stderr=subprocess.STDOUT)
    report['elapsedSeconds'] = round(time.monotonic()-start, 3)
    report['exitCode'] = result.returncode
    report['tests'] = []
    for line in log_path.read_text().splitlines():
        try:
            event = json.loads(line)
        except json.JSONDecodeError:
            print(line)
            continue
        if event.get('Action') in ('pass', 'fail', 'skip') and event.get('Test'):
            report['tests'].append({'name': event['Test'], 'status': event['Action'], 'seconds': event.get('Elapsed')})
            print(event['Action'].upper(), event['Test'], event.get('Elapsed', ''))
        if result.returncode != 0 and event.get('Action') == 'output':
            print(event.get('Output', ''), end='')
    expected = {f'{TEST_NAME}/{backend}/{scenario}' for backend in ('FS', 'Erasure') for scenario in SCENARIOS}
    passed = {test['name'] for test in report['tests'] if test['status'] == 'pass'}
    success = result.returncode == 0 and expected.issubset(passed)
    report['status'] = 'passed' if success else 'failed'
    report['passedScenarios'] = len(expected & passed)
    report['expectedScenarios'] = len(expected)
    if not success and result.returncode == 0:
        report['error'] = 'The test command did not execute and pass every required scenario.'
        print(report['error'], file=sys.stderr)
    report_path.write_text(json.dumps(report, indent=2)+'\n')
    print(json.dumps({k: report[k] for k in ('status', 'elapsedSeconds', 'passedScenarios', 'expectedScenarios')}, ensure_ascii=False))
    print('Report:', report_path)
    return 0 if success else 1


if __name__ == '__main__':
    sys.exit(main())
