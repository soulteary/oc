#!/usr/bin/env python3
"""Prevent an OC release from inheriting MinIO's update/upload/publish channels."""
from pathlib import Path
import re
import sys

root = Path(__file__).resolve().parents[1]
files = [root / p for p in (
    'cmd/update-main.go', 'cmd/main.go', 'cmd/admin-subnet-health.go',
    '.goreleaser.yml', 'Makefile', 'Dockerfile', 'Dockerfile.dev',
    'Dockerfile.release', 'docker-buildx.sh',
)]
failures = []
for path in files:
    text = path.read_text()
    if re.search(r'dl\.min(?:io)?\.io|subnet\.min\.io|minio/mc|inconshreveable/go-update', text):
        failures.append(f'{path.relative_to(root)} references a retired external channel')
for name in ('Dockerfile', 'Dockerfile.dev', 'Dockerfile.release'):
    if 'ENTRYPOINT ["oc"]' not in (root / name).read_text():
        failures.append(f'{name} does not run OC')
mod = (root / 'go.mod').read_text()
if re.search(r'^replace\b', mod, re.M):
    failures.append('go.mod must not depend on development replacements')
if 'go 1.27.1' not in mod or 'v0.0.0-20261004215341-be8596f0d69d' not in mod:
    failures.append('build baseline differs from the reviewed OtterIO commit')
if failures:
    print('\n'.join(failures), file=sys.stderr)
    sys.exit(1)
print('OC release boundaries verified')
