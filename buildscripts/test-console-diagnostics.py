#!/usr/bin/env python3
# Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
"""Isolated HTTP diagnostics authorization check; no real account or data."""
import argparse
import os
from pathlib import Path
import socket
import subprocess
import tempfile
import time
import urllib.error
import urllib.request

parser = argparse.ArgumentParser()
parser.add_argument('--server', required=True)
options = parser.parse_args()
repo = Path(__file__).resolve().parents[1]
opener = urllib.request.build_opener(urllib.request.ProxyHandler({}))
with tempfile.TemporaryDirectory(prefix='oc-diagnostics-') as data:
    with socket.socket() as listener:
        listener.bind(('127.0.0.1', 0))
        port = listener.getsockname()[1]
    env = {k: v for k, v in os.environ.items() if not k.startswith(('OC_', 'OTTERIO_'))}
    env.update(NO_PROXY='*', no_proxy='*', OTTERIO_ROOT_USER='diagnostic-test-root',
               OTTERIO_ROOT_PASSWORD='diagnostic-test-root-secret', OTTERIO_BROWSER='off')
    certs = Path(data) / 'certs'
    certs.mkdir()
    with open(data + '/server.log', 'wb') as log:
        process = subprocess.Popen([
            str(Path(options.server).resolve()), 'server', '--certs-dir', str(certs),
            '--address', f'127.0.0.1:{port}', data + '/storage',
        ], env=env, stdout=log, stderr=log)
        try:
            url = f'http://127.0.0.1:{port}'
            for _ in range(100):
                if process.poll() is not None:
                    raise RuntimeError('isolated server exited before readiness')
                try:
                    with opener.open(url + '/otterio/health/ready', timeout=.3):
                        break
                except (urllib.error.URLError, TimeoutError):
                    time.sleep(.1)
            else:
                raise RuntimeError('isolated server did not become healthy')
            env.update(OC_DIAGNOSTICS_TEST_URL=url,
                       OC_DIAGNOSTICS_TEST_ACCESS='diagnostic-test-root',
                       OC_DIAGNOSTICS_TEST_SECRET='diagnostic-test-root-secret')
            subprocess.run(['go', 'test', './internal/storageclient', '-run',
                            'TestDiagnosticsRealServer', '-count=1', '-v'],
                           env=env, cwd=repo, check=True)
        finally:
            process.terminate()
            process.wait(timeout=10)
