#!/usr/bin/env python3
"""Prevent an OC release from inheriting MinIO's update/upload/publish channels."""
from pathlib import Path
import re
import json
import sys
from check_budgets import budgets

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
support = json.loads((root / 'docs/compatibility.json').read_text())
def required_version(module):
    matches = re.findall(r'^\s*' + re.escape(module) + r'\s+(\S+)\s*(?://.*)?$', mod, re.M)
    return matches[0] if len(matches) == 1 else None

toolchain = re.search(r'^go\s+(\S+)\s*$', mod, re.M)
if required_version('github.com/soulteary/otterio') != support['otterioSDK'] or not toolchain or toolchain.group(1) != support['goToolchain']:
    failures.append('compatibility manifest differs from the pinned dependencies')
framework = support['cliFramework']
if framework['module'] != 'github.com/urfave/cli/v3' or required_version(framework['module']) != framework['version']:
    failures.append('CLI framework differs from the reviewed compatibility manifest')
if re.search(r'github\.com/minio/cli(?:/v\d+)?\s', mod):
    failures.append('go.mod retains the retired CLI framework')
for path in root.rglob('*.go'):
    if re.search(r'"github\.com/minio/cli(?:/v\d+)?"', path.read_text(encoding='utf-8')):
        failures.append(f'{path.relative_to(root)} imports the retired CLI framework')
source = support.get('otterioSource', '')
if not re.fullmatch(r'[0-9a-f]{40}', source):
    failures.append('compatibility manifest requires the full OtterIO source SHA')
elif re.search(r'-[0-9a-f]{12}$', support['otterioSDK']) and not support['otterioSDK'].endswith('-' + source[:12]):
    failures.append('OtterIO module version does not match the recorded source SHA')
try:
    budgets()
except (ValueError, TypeError, KeyError) as error:
    failures.append('invalid compatibility budgets: ' + str(error))
script = (root / 'buildscripts/cross-compile.sh').read_text()
targets = re.search(r'SUPPORTED_OSARCH="([^"]+)"', script).group(1).split()
if sorted(targets) != sorted(support['crossCompileTargets']):
    failures.append('cross-compilation targets differ from the compatibility manifest')
for patch in support['requiredServerPatches']:
    if not (root / patch).is_file():
        failures.append('compatibility manifest references a missing server patch')
if failures:
    print('\n'.join(failures), file=sys.stderr)
    sys.exit(1)
print('OC release boundaries verified')
