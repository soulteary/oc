#!/usr/bin/env python3
"""Generate a CycloneDX 1.6 compiled-module inventory from an OC binary."""
import argparse
import hashlib
import json
from pathlib import Path
import subprocess
import urllib.parse


def validate_cli_modules(modules, require_framework=False):
    versions = {item['Path']: item.get('Version') for item in modules}
    if any(path == 'github.com/minio/cli' or path.startswith('github.com/minio/cli/') for path in versions):
        raise ValueError('compiled binary contains the retired MinIO CLI framework')
    framework = json.loads((Path(__file__).resolve().parents[1] / 'docs' / 'compatibility.json').read_text())['cliFramework']
    actual = versions.get(framework['module'])
    if (require_framework or actual is not None) and actual != framework['version']:
        raise ValueError('compiled CLI framework differs from the compatibility manifest')


def inventory(binary, require_clean=False):
    info = json.loads(subprocess.check_output(['go', 'version', '-m', '-json', str(binary)]))
    settings = {item['Key']: item.get('Value', '') for item in info.get('Settings', [])}
    modules = info.get('Deps', [])
    validate_cli_modules(modules, require_framework=require_clean)
    if require_clean and (not settings.get('vcs') or settings.get('vcs.modified') != 'false' or
                          not settings.get('vcs.revision') or not settings.get('vcs.time') or
                          any('Replace' in item for item in modules)):
        raise ValueError('release inventory requires complete VCS revision/time, vcs.modified=false, and no dependency replacements')
    components = []
    for item in sorted(modules, key=lambda module: module['Path']):
        actual = item.get('Replace', item)
        if not actual.get('Version'):
            raise ValueError('cannot inventory an unversioned local module replacement')
        purl = 'pkg:golang/' + urllib.parse.quote(actual['Path'], safe='/') + '@' + urllib.parse.quote(actual['Version'], safe='')
        component = {'type': 'library', 'name': actual['Path'], 'version': actual['Version'],
                     'bom-ref': purl, 'purl': purl}
        if actual.get('Sum'):
            component['properties'] = [{'name': 'go:module:sum', 'value': actual['Sum']}]
        components.append(component)
    with binary.open('rb') as source:
        checksum = hashlib.file_digest(source, 'sha256').hexdigest()
    return {'bomFormat': 'CycloneDX', 'specVersion': '1.6', 'version': 1,
            'metadata': {'component': {'type': 'application', 'name': 'oc',
                                      'version': info['Main']['Version'],
                                      'hashes': [{'alg': 'SHA-256', 'content': checksum}]},
                         'properties': [{'name': 'oc:inventory:scope',
                                         'value': 'Compiled Go modules; not a vulnerability report or package-level dependency graph'},
                                        {'name': 'go:version', 'value': info['GoVersion']},
                                        {'name': 'go:vcs:revision', 'value': settings.get('vcs.revision', 'unknown')},
                                        {'name': 'go:vcs:modified', 'value': settings.get('vcs.modified', 'unknown')}]},
            'components': components}


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--binary', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    parser.add_argument('--require-clean', action='store_true')
    args = parser.parse_args()
    args.output.write_text(json.dumps(inventory(args.binary, args.require_clean), indent=2) + '\n')


if __name__ == '__main__':
    main()
