#!/usr/bin/env python3
"""Test packaged candidates; only trusted draft/registry CI evidence can publish."""
import argparse
from contextlib import contextmanager
import datetime
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import shutil
import shlex
import signal
import subprocess
import sys
import tarfile
import tempfile
import uuid

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('release_stage', ROOT / 'buildscripts/release-stage.py')
STAGE = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(STAGE)
PREVIOUS_SERVER_TAG = 'RELEASE.2026-10-10T16-38-05Z'
PREVIOUS_OC_TAG = 'RELEASE.2026-10-10T11-42-18Z'
PREVIOUS_SERVER_SOURCE = '8cc518b31ac8b69c8f855e19b4bd027f44275c47'
PREVIOUS_MANIFEST = '2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3'
# Allow the pair wrapper's 30-second cleanup and its nested 10-second cleanup.
PROCESS_CLEANUP_TIMEOUT = 45
IMAGE_PROBE_HARNESS_PATHS = ('buildscripts/console_release_acceptance.py',
                             'buildscripts/verify-s3-startup.go', 'go.mod', 'go.sum')


def host_target():
    name = {'Darwin': 'darwin', 'Linux': 'linux'}.get(platform.system())
    arch = {'arm64': 'arm64', 'aarch64': 'arm64', 'x86_64': 'amd64', 'AMD64': 'amd64'}.get(platform.machine())
    STAGE.require(name and arch, 'acceptance requires Linux or Darwin amd64/arm64')
    return name + '-' + arch


def unpack_oc(directory, tag, target, destination):
    """Read only the two expected regular files, never extract archive paths."""
    stem = f'oc-{tag}-{target}'
    archive = Path(directory) / (stem + '.tar.gz')
    with tarfile.open(archive, 'r:gz') as data:
        rows = data.getmembers()
        STAGE.require(len(rows) <= 256 and len({r.name for r in rows}) == len(rows), 'duplicate or excessive archive members')
        selected = {}
        for name in ('oc', 'oc-console'):
            matches = [r for r in rows if r.name == stem + '/' + name]
            STAGE.require(len(matches) == 1 and matches[0].isfile() and 0 < matches[0].size < 256_000_000, 'packaged executable missing or unsafe')
            selected[name] = matches[0]
        for name, member in selected.items():
            path = Path(destination) / name
            with data.extractfile(member) as stream:
                contents = stream.read()
            STAGE.require(len(contents) == member.size, 'truncated packaged executable')
            path.write_bytes(contents)
            path.chmod(0o755)
    return {'oc': Path(destination) / 'oc', 'console': Path(destination) / 'oc-console'}


@contextmanager
def protect_cleanup():
    """A second cancellation must not interrupt owned process/container cleanup."""
    previous = {sig: signal.signal(sig, signal.SIG_IGN) for sig in (signal.SIGTERM, signal.SIGINT)}
    try:
        yield
    finally:
        for sig, handler in previous.items():
            signal.signal(sig, handler)


def run_isolated(command, env, timeout, cwd=ROOT):
    """Reap the entire owned group before returning or propagating cancellation."""
    # Functional fixtures and already-authenticated Docker commands need no
    # repository publication/read credentials, including their descendants.
    child_env = {key: value for key, value in env.items()
                 if key not in ('GH_TOKEN', 'GITHUB_TOKEN', 'CONSOLE_RELEASE_TOKEN')}
    process = subprocess.Popen(command, cwd=cwd, env=child_env, stdout=subprocess.PIPE,
                               stderr=subprocess.STDOUT, text=True, start_new_session=True)
    try:
        output, _ = process.communicate(timeout=timeout)
        return process.returncode, output
    except subprocess.TimeoutExpired:
        return 124, ''
    finally:
        with protect_cleanup():
            try:
                os.killpg(process.pid, signal.SIGTERM)
            except ProcessLookupError:
                pass
            try:
                process.communicate(timeout=PROCESS_CLEANUP_TIMEOUT)
            except subprocess.TimeoutExpired:
                pass
            # A finished wrapper may leave descendants with closed output pipes.
            try:
                os.killpg(process.pid, signal.SIGKILL)
            except ProcessLookupError:
                pass
            process.communicate(timeout=PROCESS_CLEANUP_TIMEOUT)


def run_profile(command, report):
    code, _ = run_isolated(command, dict(os.environ), 3600)
    # Keep credentials and raw subprocess output out of the persisted evidence.
    return code == 0 and Path(report).is_file()


def docker_command(arguments, timeout=300):
    code, output = run_isolated(['docker', *arguments], dict(os.environ), timeout)
    STAGE.require(code == 0, 'container verification command failed')
    return output


def cleanup_containers(names):
    """Confirm absence through a successful query; daemon errors fail closed."""
    errors = []
    with protect_cleanup():
        for name in reversed(names):
            try:
                query = ['container', 'ls', '--all', '--filter', 'name=^/' + name + '$', '--format', '{{.Names}}']
                present = docker_command(query, timeout=30).strip()
                STAGE.require(present in ('', name), 'unexpected owned container identity')
                if present:
                    docker_command(['rm', '-f', name], timeout=30)
                    STAGE.require(not docker_command(query, timeout=30).strip(), 'owned container still exists')
            except (Exception, KeyboardInterrupt) as error:
                errors.append(type(error).__name__)
    return {'status': 'failed' if errors else 'passed', 'containers': len(names), 'errors': errors}


def platform_image(release, target):
    """Select a child pinned by the accepted index, without local index aliases."""
    primary = release['images'][0]
    index_ref = primary['repository'] + '@' + primary['digest']
    index = json.loads(STAGE.command(['docker', 'buildx', 'imagetools', 'inspect',
                                     index_ref, '--format', '{{json .Manifest}}']))
    STAGE.require(index.get('digest') == primary['digest'] and index.get('schemaVersion') == 2 and
                  index.get('mediaType') in ('application/vnd.oci.image.index.v1+json',
                                            'application/vnd.docker.distribution.manifest.list.v2+json'),
                  'accepted image index identity mismatch')
    os_name, arch = target.split('/')
    rows = index.get('manifests')
    STAGE.require(isinstance(rows, list) and all(isinstance(row, dict) for row in rows),
                  'invalid accepted image index')
    selected = [row for row in rows if isinstance(row.get('platform'), dict) and
                row['platform'].get('os') == os_name and row['platform'].get('architecture') == arch]
    STAGE.require(len(selected) == 1, 'accepted image index must identify exactly one target platform')
    descriptor = selected[0]
    digest = descriptor.get('digest')
    STAGE.require(isinstance(digest, str) and re.fullmatch(r'sha256:[0-9a-f]{64}', digest) and
                  descriptor.get('mediaType') in ('application/vnd.oci.image.manifest.v1+json',
                                                 'application/vnd.docker.distribution.manifest.v2+json'),
                  'invalid accepted platform manifest')
    return primary['repository'] + '@' + digest


def image_acceptance(releases, assets, output):
    """Execute both architectures by digest and compare actual container bytes."""
    report = {'schemaVersion': 1, 'mode': 'registry-digest', 'passed': False, 'checks': [],
              'scope': 'OCI program byte equality and CLI/filesystem/S3 fixtures; QEMU/binfmt execution does not qualify native hardware.'}
    output = Path(output)
    try:
        STAGE.require(platform.system() == 'Linux', 'registry runtime acceptance requires a Linux hosted runner')
        host = host_target().replace('-', '/')
        report['runnerPlatform'] = host
        report['harnesses'] = {name: STAGE.sha256(ROOT / name) for name in IMAGE_PROBE_HARNESS_PATHS}
        for repository, release in releases.items():
            STAGE.verify_images(release)
            for arch in ('amd64', 'arm64'):
                target = 'linux/' + arch
                row = {'repository': repository, 'platform': target, 'digest': release['images'][0]['digest'],
                       'executionMode': 'native' if host == target else 'qemu-binfmt-emulated',
                       'nativeHardwareQualification': False,
                       'passed': False, 'binaryBytesMatched': False, 'runtimePassed': False}
                report['checks'].append(row)
                owned = []
                prefix = 'console-release-' + uuid.uuid4().hex + '-' + arch
                with tempfile.TemporaryDirectory(prefix='packaged-image-check-') as temporary:
                    directory = Path(temporary)
                    try:
                        # Classic Docker stores cannot bind one index digest to two platforms.
                        # The accepted index selects this immutable child; all runtime checks remain.
                        image = platform_image(release, target)
                        row['platformManifestDigest'] = image.rsplit('@', 1)[1]
                        docker_command(['pull', '--platform', target, image])
                        container = prefix + '-extract'
                        owned.append(container)
                        docker_command(['create', '--name', container, '--platform', target, image])

                        def run_container(arguments, purpose):
                            name = prefix + '-' + purpose
                            owned.append(name)
                            return docker_command(['run', '--rm', '--name', name, '--platform', target, *arguments])
                        if repository.endswith('/oc'):
                            binaries = unpack_oc(assets[repository], release['tag'], 'linux-' + arch, directory)
                            for key, name in (('oc', 'oc'), ('console', 'oc-console')):
                                extracted = directory / ('image-' + name)
                                docker_command(['cp', container + ':/usr/bin/' + name, str(extracted)])
                                STAGE.require(STAGE.sha256(extracted) == STAGE.sha256(binaries[key]), 'image program differs from release archive')
                                version = run_container(['--entrypoint', name, image, '--version'], name + '-version')
                                STAGE.require(release['tag'] in version, 'image version mismatch')
                                run_container(['--entrypoint', name, image, '--help'], name + '-help')
                            # A real filesystem operation in the container, not only --version.
                            (directory / 'input').write_bytes(b'packaged image acceptance\n')
                            run_container(['--mount',
                                           f'type=bind,src={directory},dst=/acceptance', image, '--config-dir', '/tmp/oc-acceptance',
                                           'cp', '/acceptance/input', '/acceptance/output'], 'copy')
                            STAGE.require((directory / 'output').read_bytes() == (directory / 'input').read_bytes(), 'image copy failed')
                        else:
                            binary = Path(assets[repository]) / ('otterio-linux-' + arch)
                            extracted = directory / 'otterio'
                            docker_command(['cp', container + ':/usr/bin/otterio', str(extracted)])
                            STAGE.require(STAGE.sha256(extracted) == STAGE.sha256(binary), 'image server differs from release program')
                            STAGE.require(release['tag'] in run_container([image, '--version'], 'version'), 'image server version mismatch')
                            # Linux hosted runners support host networking. Execute the reviewed
                            # OC probe and dependencies; the candidate contributes only image bytes.
                            launcher = directory / 'server'
                            server_name = prefix + '-server'
                            owned.append(server_name)
                            launcher.write_text('#!/bin/sh\nexec docker run --rm --name ' + shlex.quote(server_name) + ' --platform ' + target + ' --network host '
                                                '--user "$(id -u):$(id -g)" -e HOME=/tmp -e OTTERIO_ACCESS_KEY '
                                                '-e OTTERIO_SECRET_KEY -e OTTERIO_BROWSER '
                                                '--mount ' + shlex.quote('type=bind,src=' + temporary + ',dst=' + temporary) + ' '
                                                + shlex.quote(image) + ' "$@"\n')
                            launcher.chmod(0o755)
                            env = dict(os.environ, TMPDIR=temporary, GOWORK='off', GOENV='off',
                                       GOTOOLCHAIN='local', CGO_ENABLED='0')
                            for name in ('GOFLAGS', 'GOOS', 'GOARCH'):
                                env.pop(name, None)
                            code, _ = run_isolated(['go', 'run', '-mod=readonly',
                                                   str(ROOT / 'buildscripts/verify-s3-startup.go'), str(launcher)],
                                                   env, 300, cwd=ROOT)
                            STAGE.require(code == 0, 'container S3 round trip failed')
                        STAGE.require(all(STAGE.sha256(ROOT / name) == digest for name, digest in report['harnesses'].items()),
                                      'trusted image probe or dependencies changed during acceptance')
                        row.update(passed=True, binaryBytesMatched=True, runtimePassed=True)
                    finally:
                        row['cleanup'] = cleanup_containers(owned)
                        if row['cleanup']['status'] != 'passed':
                            row['passed'] = False
                            raise ValueError('container cleanup failed')
        report['passed'] = len(report['checks']) == 4 and all(r['passed'] for r in report['checks'])
    except (Exception, KeyboardInterrupt) as error:
        report['errorClass'] = type(error).__name__
        if isinstance(error, ValueError):
            report['reason'] = str(error)
    finally:
        output.write_text(json.dumps(report, indent=2) + '\n')
    return report['passed']


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--oc-tag', required=True)
    parser.add_argument('--otterio-tag', required=True)
    parser.add_argument('--oc-assets', type=Path)
    parser.add_argument('--otterio-assets', type=Path)
    parser.add_argument('--download-drafts', action='store_true')
    parser.add_argument('--previous-oc', type=Path, required=True)
    parser.add_argument('--previous-otterio', type=Path, required=True)
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    args.output.mkdir(parents=True, exist_ok=False)
    document = {'schemaVersion': 1, 'evidenceKind': 'draft-release-pair' if args.download_drafts else 'local-packaged-candidate-pair',
                'passed': False, 'publishable': False, 'dateUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(),
                'scope': 'packaged programs; single-node fixtures; native/OIDC direct HTTPS; Chromium; two TLS upgrade/rollback paths',
                'releases': {}, 'packagedPrograms': {}, 'previousPrograms': {}, 'reportFiles': {}}
    destination = args.output / 'acceptance.json'
    try:
        STAGE.require((args.download_drafts and not args.oc_assets and not args.otterio_assets) or
                      (not args.download_drafts and args.oc_assets and args.otterio_assets), 'select draft download or both local asset directories')
        with tempfile.TemporaryDirectory(prefix='packaged-release-acceptance-') as temporary:
            temp = Path(temporary)
            assets = {'soulteary/oc': args.oc_assets, 'soulteary/otterio': args.otterio_assets}
            for repository, tag in [('soulteary/oc', args.oc_tag), ('soulteary/otterio', args.otterio_tag)]:
                if args.download_drafts:
                    assets[repository] = temp / repository.split('/')[1]
                    record = STAGE.snapshot(repository, tag, assets[repository])
                else:
                    record = STAGE.inventory(assets[repository], repository, tag, require_images=False)
                document['releases'][repository] = record
            target = host_target()
            programs = temp / 'programs'
            programs.mkdir()
            binaries = unpack_oc(assets['soulteary/oc'], args.oc_tag, target, programs)
            binaries['server'] = assets['soulteary/otterio'] / ('otterio-' + target)
            # chmod a private copy only; candidate bytes remain immutable.
            copied_server = programs / 'otterio'
            shutil.copyfile(binaries['server'], copied_server)
            copied_server.chmod(0o755)
            binaries['server'] = copied_server
            document['packagedPrograms'] = {key: STAGE.sha256(path) for key, path in binaries.items()}
            pair = args.output / 'pair.json'
            pair_ok = run_profile([sys.executable, 'buildscripts/test-console-migration-pair.py',
                                   '--cli', str(binaries['oc']), '--console', str(binaries['console']), '--server', str(binaries['server']),
                                   '--oc-source', document['releases']['soulteary/oc']['sourceSHA'],
                                   '--server-source', document['releases']['soulteary/otterio']['sourceSHA'], '--output', str(pair)], pair)
            old = temp / 'previous-oc-programs'
            # Extract the previous archive independently, then bind it in upgrade runner.
            old.mkdir(parents=True, exist_ok=True)
            previous = unpack_oc(args.previous_oc, PREVIOUS_OC_TAG, target, old)
            upgrade = args.output / 'upgrade.json'
            upgrade_ok = run_profile([sys.executable, 'buildscripts/console_upgrade_acceptance.py',
                                      '--previous-server', str(args.previous_otterio / ('otterio-' + target)),
                                      '--previous-cli', str(previous['oc']), '--previous-console', str(previous['console']),
                                      '--server', str(binaries['server']), '--cli', str(binaries['oc']), '--console', str(binaries['console']),
                                      '--previous-source', PREVIOUS_SERVER_SOURCE,
                                      '--server-source', document['releases']['soulteary/otterio']['sourceSHA'],
                                      '--cli-source', document['releases']['soulteary/oc']['sourceSHA'],
                                      '--previous-release', PREVIOUS_SERVER_TAG, '--previous-oc-release', PREVIOUS_OC_TAG,
                                      '--previous-manifest', str(args.previous_otterio / 'release-manifest.json'),
                                      '--previous-manifest-sha256', PREVIOUS_MANIFEST,
                                      '--previous-checksums', str(args.previous_otterio / ('otterio-' + PREVIOUS_SERVER_TAG + '-checksums.txt')),
                                      '--previous-oc-manifest', str(args.previous_oc / 'release-manifest.json'),
                                      '--previous-oc-checksums', str(args.previous_oc / 'checksums.txt'),
                                      '--previous-oc-archive', str(args.previous_oc / ('oc-' + PREVIOUS_OC_TAG + '-' + target + '.tar.gz')),
                                      '--output', str(upgrade)], upgrade)
            image_ok = False
            if args.download_drafts:
                STAGE.require(platform.system() == 'Linux', 'registry runtime acceptance requires a Linux hosted runner')
                image_ok = image_acceptance(document['releases'], assets, args.output / 'images.json')
            else:
                (args.output / 'images.json').write_text(json.dumps({'schemaVersion': 1, 'mode': 'not-certified', 'passed': False,
                    'checks': [], 'reason': 'Local packaged-program evidence does not certify registry digests or container runtime.'}, indent=2) + '\n')
            document['previousPrograms'] = {k: json.loads(upgrade.read_text()).get('programs', {}).get(k)
                                            for k in ('previousServer', 'previousCLI', 'previousConsole')} if upgrade.is_file() else {}
            document['passed'] = pair_ok and upgrade_ok and (image_ok or not args.download_drafts)
            document['publishable'] = document['passed'] and image_ok and args.download_drafts
            for repository, candidate in document['releases'].items():
                current = (STAGE.snapshot(repository, candidate['tag'], temp / (repository.split('/')[1] + '-recheck'))
                           if args.download_drafts else STAGE.inventory(assets[repository], repository, candidate['tag'], require_images=False))
                STAGE.require(current == candidate, 'candidate changed during acceptance')
            if args.download_drafts:
                document['provenance'] = {'repository': os.environ.get('GITHUB_REPOSITORY'), 'workflowPath': STAGE.ACCEPTANCE_WORKFLOW,
                                          'runID': int(os.environ['GITHUB_RUN_ID']), 'runAttempt': int(os.environ['GITHUB_RUN_ATTEMPT']),
                                          'headSHA': os.environ['GITHUB_SHA'], 'event': os.environ['GITHUB_EVENT_NAME'], 'ref': os.environ['GITHUB_REF']}
            document['reportFiles'] = {p.name: STAGE.sha256(p) for p in sorted(args.output.glob('*.json')) if p != destination}
            destination.write_text(json.dumps(document, indent=2) + '\n')
            if document['publishable']:
                STAGE.validate_evidence(document, args.output)
    except (Exception, KeyboardInterrupt) as error:
        document.update(passed=False, publishable=False, errorClass=type(error).__name__)
        if isinstance(error, ValueError):
            document['reason'] = str(error)
    finally:
        document['reportFiles'] = {p.name: STAGE.sha256(p) for p in sorted(args.output.glob('*.json')) if p != destination}
        destination.write_text(json.dumps(document, indent=2) + '\n')
        print(json.dumps({'passed': document['passed'], 'publishable': document['publishable'],
                          'attestationSHA256': STAGE.sha256(destination), 'report': str(destination)}))
    return 0 if document['passed'] else 1


def handle_termination(signum, frame):
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    raise KeyboardInterrupt


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, handle_termination)
    signal.signal(signal.SIGINT, handle_termination)
    raise SystemExit(main())
