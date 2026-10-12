#!/usr/bin/env python3
"""Actual previous-release -> source candidate -> rollback on disposable storage.

The old assets are independently checksum/manifest/archive bound. The new pair
is candidate evidence, never release certification. No operator configuration,
credentials, ports, volumes or existing containers are used.
"""
import argparse
import contextlib
import datetime
import hashlib
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import platform
import re
import secrets
import shutil
import signal
import ssl
import subprocess
import tarfile
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
SPEC = importlib.util.spec_from_file_location('upgrade_protocol', ROOT / 'buildscripts/test-console-integration.py')
PROTOCOL = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(PROTOCOL)


def require(condition, message):
    if not condition:
        raise RuntimeError(message)


def checksum_bound(path, checksums):
    matches = []
    for line in Path(checksums).read_text().splitlines():
        fields = line.split(maxsplit=1)
        if len(fields) == 2 and fields[1].lstrip('*') == Path(path).name:
            matches.append(fields[0])
    require(len(matches) == 1 and re.fullmatch('[0-9a-f]{64}', matches[0]) is not None,
            'missing or ambiguous previous asset checksum')
    require(PROTOCOL.digest(path) == matches[0], 'previous asset checksum mismatch')
    return matches[0]


def manifest_bound(path, tag, source=None, kind='server'):
    doc = json.loads(Path(path).read_text())
    tag_key, source_key = ('tag', 'source_sha') if kind == 'server' else ('release_tag', 'source_commit')
    require(doc.get('schema_version') == 1 and doc.get(tag_key) == tag and re.fullmatch('[0-9a-f]{40}', doc.get(source_key, '')),
            'previous release manifest identity mismatch')
    require(source is None or doc[source_key] == source, 'previous source differs from release manifest')
    return {'tag': tag, 'source': doc[source_key], 'manifestSHA256': PROTOCOL.digest(path)}


def archive_bound(archive, programs):
    with tarfile.open(archive, 'r:gz') as package:
        for name, path in programs.items():
            members = [m for m in package.getmembers() if m.isfile() and Path(m.name).name == name]
            require(len(members) == 1, 'previous archive has missing or duplicate program')
            with package.extractfile(members[0]) as stream:
                require(hashlib.file_digest(stream, 'sha256').hexdigest() == PROTOCOL.digest(path),
                        'previous extracted program differs from verified archive')


def program_identity(path, source, kind):
    require(re.fullmatch('[0-9a-f]{40}', source), 'invalid declared source')
    info = json.loads(subprocess.run(['go', 'version', '-m', '-json', str(path)],
                                    capture_output=True, text=True, check=True, timeout=15).stdout)
    settings = {item['Key']: item.get('Value', '') for item in info.get('Settings', [])}
    require(not any('Replace' in item for item in info.get('Deps', [])), 'program has module replacement')
    revision = settings.get('vcs.revision')
    require(not revision or revision == source, 'program VCS source mismatch')
    if kind == 'candidate-source-build':
        require(revision == source and settings.get('vcs.modified') == 'false', 'candidate is not a clean VCS build')
    return {'binarySHA256': PROTOCOL.digest(path), 'source': source, 'kind': kind,
            'embeddedVCS': revision, 'embeddedModified': settings.get('vcs.modified'),
            'goVersion': info.get('GoVersion'), 'goos': settings.get('GOOS'), 'goarch': settings.get('GOARCH')}


def tree_digest(directory):
    digest = hashlib.sha256()
    for path in sorted(Path(directory).rglob('*')):
        if path.is_file():
            data = path.read_bytes()
            digest.update(path.relative_to(directory).as_posix().encode() + b'\0')
            digest.update(str(len(data)).encode() + b'\0' + data)
    return digest.hexdigest()


@contextlib.contextmanager
def fixture_directory(processes, logs):
    with tempfile.TemporaryDirectory(prefix='oc-version-upgrade-') as temporary:
        try:
            yield Path(temporary)
        finally:
            # Stop writers before deleting the temporary storage, including on
            # the first failed assertion. Never race a live server with rmtree.
            interrupted = False
            for process in reversed(processes):
                try:
                    PROTOCOL.stop(process)
                except KeyboardInterrupt:
                    # A first termination can arrive during stop(), after the
                    # fixture body succeeded. Finish stopping this writer and
                    # all remaining children before TemporaryDirectory exits.
                    interrupted = True
                    PROTOCOL.stop(process)
            for log in logs: log.close()
            if interrupted:
                raise KeyboardInterrupt


def run_profile(args, profile):
    record = {'profile': profile, 'status': 'running', 'checks': [], 'phase': 'prepare'}
    processes, logs = [], []
    started = time.monotonic()
    try:
        with fixture_directory(processes, logs) as root:
            env = {k: v for k, v in os.environ.items()
                   if not k.startswith(('OC_', 'MC_', 'OTTERIO_', 'MINIO_')) and k not in ('HTTP_PROXY', 'HTTPS_PROXY', 'ALL_PROXY')}
            access, secret = 'upgrade-' + secrets.token_hex(5), secrets.token_hex(24)
            user, user_secret = 'reader-' + secrets.token_hex(5), secrets.token_hex(24)
            env.update(OTTERIO_ROOT_USER=access, OTTERIO_ROOT_PASSWORD=secret, OTTERIO_BROWSER='off', OTTERIO_UPDATE='off')
            s3_port = PROTOCOL.free_port()
            admin_port = PROTOCOL.free_port() if profile == 'dual-tls' else s3_port
            while profile == 'dual-tls' and admin_port == s3_port:
                admin_port = PROTOCOL.free_port()
            s3_ca = PROTOCOL.certificate(root / 's3-tls')
            admin_ca = PROTOCOL.certificate(root / 'admin-tls') if profile == 'dual-tls' else s3_ca
            web_ca = PROTOCOL.certificate(root / 'web-tls')
            endpoint, admin = f'https://127.0.0.1:{s3_port}', f'https://127.0.0.1:{admin_port}'
            context = ssl.create_default_context(cafile=str(s3_ca))
            admin_context = ssl.create_default_context(cafile=str(admin_ca))
            config = root / 'config'
            (config / 'certs/CAs').mkdir(parents=True)
            (config / 'certs/CAs/s3.crt').write_bytes(s3_ca.read_bytes())
            config_path = config / 'config.json'
            config_path.write_text(json.dumps({'version': '10', 'aliases': {'store': {
                'url': endpoint, 'adminURL': admin, 'adminCAFile': str(admin_ca),
                'accessKey': access, 'secretKey': secret, 'api': 'S3v4', 'path': 'on'}}}))
            config_path.chmod(0o600)
            config_before = config_path.read_bytes()
            volumes = [root / f'data-{i}' for i in range(4)]
            server = None
            bucket = 'upgrade-' + secrets.token_hex(5)
            versions = []
            post_upgrade = False
            seeded = False

            def check(name):
                record['checks'].append({'name': name, 'status': 'passed'})

            def cli(binary, *arguments):
                try:
                    result = subprocess.run([str(binary), '--config-dir', str(config), '--no-color', *arguments],
                                            env=env, capture_output=True, timeout=30)
                except subprocess.TimeoutExpired:
                    raise RuntimeError('fixture CLI timeout') from None
                require(result.returncode == 0, 'fixture CLI failed')
                return result.stdout

            def start_server(binary, phase):
                nonlocal server
                record['phase'] = phase
                log_path = root / (phase + '-server.log')
                log = log_path.open('wb')
                logs.append(log)
                command = [str(binary), 'server', '--address', f'127.0.0.1:{s3_port}', '--certs-dir', str(s3_ca.parent)]
                if profile == 'dual-tls':
                    command += ['--console-address', f'127.0.0.1:{admin_port}', '--console-certs-dir', str(admin_ca.parent)]
                server_env = env.copy()
                if phase == 'candidate-upgrade':
                    server_env.pop('OTTERIO_BROWSER')
                server = subprocess.Popen(command + list(map(str, volumes)), env=server_env, stdout=log, stderr=log)
                processes.append(server)
                deadline = time.monotonic() + 90
                while True:
                    require(server.poll() is None, 'server exited before readiness')
                    try:
                        with PROTOCOL.local_urlopen(endpoint + '/otterio/health/ready', context=context, timeout=1) as reply:
                            if reply.status == 200 and 'IAM initialization complete' in log_path.read_text(errors='replace'):
                                status, _, _ = PROTOCOL.signed_request(endpoint, 'GET', '/', b'', access, secret, context)
                                # Health can become ready before storage/IAM
                                # initialization. Poll only this read-only
                                # readiness probe, never retry acceptance writes.
                                if status == 200:
                                    if not seeded:
                                        return
                                    iam_status, iam_body, _ = PROTOCOL.signed_request(admin, 'GET',
                                        '/otterio/admin/v3/user-info', b'', access, secret, admin_context,
                                        query=urllib.parse.urlencode({'accessKey': user}))
                                    # IAM loads asynchronously after storage.
                                    # Wait for the persisted user through the
                                    # authenticated management API; never retry
                                    # the later allow/deny acceptance assertions.
                                    if iam_status == 200 and iam_body:
                                        return
                    except (OSError, urllib.error.HTTPError):
                        pass
                    require(time.monotonic() < deadline, 'server readiness timeout')
                    time.sleep(.1)

            def request(method, key='', payload=b'', query='', who='root', expected=200, headers=None):
                uri = '/' + bucket + ('/' + key if key else '')
                code, body, response_headers = PROTOCOL.signed_request(endpoint, method, uri, payload,
                    access if who == 'root' else user, secret if who == 'root' else user_secret,
                    context, query=query, extra_headers=headers)
                require(code == expected, f'fixture S3 status mismatch: {method} {key or "bucket"} ({who}) expected {expected}, got {code}')
                return body, response_headers

            def verify_state(binary, stage):
                record['phase'] = stage
                require(request('GET', 'plain.txt')[0] == b'previous version bytes\n', 'plain bytes changed')
                for version, payload in versions:
                    query = urllib.parse.urlencode({'versionId': version})
                    require(request('GET', 'history.txt', query=query)[0] == payload, 'historical bytes changed')
                    require(request('GET', 'history.txt', query=query, who='user')[0] == payload, 'historical permission changed')
                versioning = ET.fromstring(request('GET', query='versioning=')[0])
                require(versioning.findtext('{*}Status') == 'Enabled', 'bucket versioning changed')
                tags = ET.fromstring(request('GET', 'plain.txt', query='tagging=')[0])
                require([(t.findtext('{*}Key'), t.findtext('{*}Value')) for t in tags.findall('.//{*}Tag')] == [('migration', 'preserve')], 'object tags changed')
                require(request('GET', 'plain.txt', who='user')[0] == b'previous version bytes\n', 'user read permission changed')
                request('GET', 'denied.txt', who='user', expected=403)
                request('PUT', 'forbidden.txt', b'forbidden', who='user', expected=403)
                cli(binary, 'admin', 'user', 'info', 'store', user)
                cli(binary, 'admin', 'group', 'info', 'store', 'upgrade-readers')
                require(config_path.read_bytes() == config_before, 'CLI configuration mutated')
                if post_upgrade:
                    require(request('GET', 'after-upgrade.txt', who='user')[0] == b'new candidate write\n', 'new write unavailable after rollback')
                else:
                    request('GET', 'after-upgrade.txt', expected=404)
                check(stage + ': objects versions tags bucket configuration IAM group policy denies and CLI config preserved')

            def preferences(binary, mode, expected, set_language=None):
                port = PROTOCOL.free_port()
                base = f'{"http" if mode == "local" else "https"}://127.0.0.1:{port}'
                command = [str(binary), '--address', f'127.0.0.1:{port}', '--data-dir', str(root / 'preferences')]
                if mode == 'local':
                    command += ['--config-dir', str(config), '--alias', 'store']
                else:
                    command += ['--auth-mode', 'native', '--s3-url', endpoint, '--admin-url', admin,
                                '--s3-ca', str(s3_ca), '--admin-ca', str(admin_ca), '--public-url', base,
                                '--tls-cert', str(web_ca), '--tls-key', str(web_ca.parent / 'private.key')]
                log_path = root / ('console-' + secrets.token_hex(3) + '.log')
                log = log_path.open('wb'); logs.append(log)
                process = subprocess.Popen(command, env=env, stdout=log, stderr=log)
                processes.append(process)
                try:
                    deadline = time.monotonic() + 15
                    while True:
                        output = log_path.read_text(errors='replace')
                        require(process.poll() is None, 'console exited before readiness')
                        if 'URL: ' in output and (mode != 'local' or 'Login code: ' in output):
                            break
                        require(time.monotonic() < deadline, 'console startup timeout')
                        time.sleep(.05)
                    handlers = [urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar())]
                    if mode != 'local':
                        handlers.append(urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=str(web_ca))))
                    opener = urllib.request.build_opener(*handlers)
                    def web(path, body=None, csrf=None):
                        headers = {'Origin': base, 'Content-Type': 'application/json'}
                        if csrf: headers['X-CSRF-Token'] = csrf
                        with opener.open(urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None,
                            method='PUT' if path == '/api/preferences' and body is not None else ('POST' if body is not None else 'GET'),
                            headers=headers), timeout=15) as reply:
                            return json.loads(reply.read())
                    login = {'code': output.split('Login code: ')[-1].splitlines()[0].strip()} if mode == 'local' else {'accessKey': user, 'secretKey': user_secret}
                    session = web('/api/login', login)
                    require(web('/api/preferences')['language'] == expected, 'preferences crossed mode or changed across version/restart')
                    if set_language:
                        web('/api/preferences', {'action': 'language', 'language': set_language}, session['csrfToken'])
                        require(web('/api/preferences')['language'] == set_language, 'preferences write did not persist')
                finally:
                    PROTOCOL.stop(process)

            start_server(args.previous_server, 'previous-release')
            request('PUT')
            request('PUT', 'plain.txt', b'previous version bytes\n')
            request('PUT', 'denied.txt', b'explicit deny\n')
            request('PUT', 'plain.txt', b'<Tagging><TagSet><Tag><Key>migration</Key><Value>preserve</Value></Tag></TagSet></Tagging>', query='tagging=', headers={'content-type': 'application/xml'})
            request('PUT', payload=b'<VersioningConfiguration xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Status>Enabled</Status></VersioningConfiguration>', query='versioning=', headers={'content-type': 'application/xml'})
            for payload in (b'historical one\n', b'historical two\n'):
                _, headers = request('PUT', 'history.txt', payload)
                require(bool(headers.get('X-Amz-Version-Id')), 'version seed lacks version ID')
                versions.append((headers['X-Amz-Version-Id'], payload))
            policy = root / 'policy.json'
            policy.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
                {'Effect': 'Allow', 'Action': ['s3:GetBucketLocation', 's3:ListBucket'], 'Resource': ['arn:aws:s3:::' + bucket]},
                {'Effect': 'Allow', 'Action': ['s3:GetObject', 's3:GetObjectVersion'], 'Resource': ['arn:aws:s3:::' + bucket + '/*']},
                {'Effect': 'Deny', 'Action': ['s3:GetObject'], 'Resource': ['arn:aws:s3:::' + bucket + '/denied.txt']}]}))
            cli(args.previous_cli, 'admin', 'user', 'add', 'store', user, user_secret)
            cli(args.previous_cli, 'admin', 'group', 'add', 'store', 'upgrade-readers', user)
            cli(args.previous_cli, 'admin', 'policy', 'add', 'store', 'upgrade-read', str(policy))
            cli(args.previous_cli, 'admin', 'policy', 'set', 'store', 'upgrade-read', 'group=upgrade-readers')
            seeded = True
            verify_state(args.previous_cli, 'previous-baseline')
            preferences(args.previous_console, 'local', 'zh', 'en')
            check('previous published console writes shared local preferences')
            PROTOCOL.stop(server)
            snapshot = root / 'snapshot'
            snapshot.mkdir()
            for volume in volumes: shutil.copytree(volume, snapshot / volume.name)
            snapshot_hash = tree_digest(snapshot)
            start_server(args.server, 'candidate-upgrade')
            check('candidate accepts omitted browser environment with retained TLS S3/Admin listeners')
            verify_state(args.cli, 'candidate-without-new-writes')
            preferences(args.console, 'local', 'en')
            preferences(args.console, 'native', 'zh', 'en')
            preferences(args.console, 'native', 'en')
            check('old-to-new local preferences preserved; native identity isolated and persistent after restart')
            for method, path in [('GET', '/otterio/'), ('POST', '/otterio/webrpc'), ('PUT', '/otterio/upload/fixture/key'), ('GET', '/otterio/download/fixture/key?token=x'), ('POST', '/otterio/zip?token=x')]:
                req = urllib.request.Request(admin + path, method=method, headers={'User-Agent': 'Mozilla/5.0'}, data=b'{}' if method in ('POST', 'PUT') else None)
                try:
                    with PROTOCOL.local_urlopen(req, context=admin_context, timeout=5) as reply:
                        require(False, 'legacy route unexpectedly available')
                except urllib.error.HTTPError as error:
                    require(PROTOCOL.legacy_route_disabled(error.code, error.read()), 'legacy route not a confirmed backend rejection')
            check('candidate legacy routes unavailable while verified S3 and Admin remain reachable')
            PROTOCOL.stop(server)
            start_server(args.previous_server, 'rollback-without-new-writes')
            verify_state(args.previous_cli, 'rollback-without-new-writes')
            preferences(args.previous_console, 'local', 'en')
            check('previous console can read local preferences after current console')
            PROTOCOL.stop(server)
            start_server(args.server, 'candidate-new-write')
            check('candidate accepts explicit browser off with retained TLS S3/Admin listeners')
            request('PUT', 'after-upgrade.txt', b'new candidate write\n')
            post_upgrade = True
            verify_state(args.cli, 'candidate-after-new-write')
            PROTOCOL.stop(server)
            start_server(args.previous_server, 'rollback-after-new-write')
            verify_state(args.previous_cli, 'rollback-after-new-write')
            PROTOCOL.stop(server)
            require(tree_digest(snapshot) == snapshot_hash, 'pre-upgrade snapshot changed')
            for volume in volumes:
                shutil.rmtree(volume)
                shutil.copytree(snapshot / volume.name, volume)
            post_upgrade = False
            start_server(args.previous_server, 'snapshot-restore')
            verify_state(args.previous_cli, 'snapshot-restored-baseline')
            check('pre-upgrade snapshot recovery omits subsequent writes as expected')
            record['status'] = 'passed'
    except (Exception, KeyboardInterrupt) as error:
        record['status'] = 'failed'
        record['errorClass'] = type(error).__name__
        if isinstance(error, KeyboardInterrupt): record['interrupted'] = True
        # Never retain raw CLI output, arguments, server logs or user secrets.
        if isinstance(error, RuntimeError):
            record['reason'] = str(error)
    finally:
        for process in reversed(processes): PROTOCOL.stop(process)
        for log in logs: log.close()
        record['cleanup'] = 'passed' if all(p.poll() is not None for p in processes) else 'failed'
        if record['cleanup'] != 'passed': record['status'] = 'failed'
        record['durationSeconds'] = round(time.monotonic() - started, 3)
    return record


def handle_termination(signum, frame):
    # The first cancellation enters finally. Ignore subsequent termination
    # signals while stopping child processes and saving the failure evidence.
    signal.signal(signal.SIGTERM, signal.SIG_IGN)
    signal.signal(signal.SIGINT, signal.SIG_IGN)
    raise KeyboardInterrupt


def main():
    signal.signal(signal.SIGTERM, handle_termination)
    signal.signal(signal.SIGINT, handle_termination)
    parser = argparse.ArgumentParser(description=__doc__)
    for option in ('previous-server', 'previous-cli', 'previous-console', 'server', 'cli', 'console',
                   'previous-manifest', 'previous-checksums', 'previous-oc-manifest', 'previous-oc-checksums', 'previous-oc-archive'):
        parser.add_argument('--' + option, type=Path, required=True)
    for option in ('previous-source', 'server-source', 'cli-source', 'previous-release', 'previous-oc-release'):
        parser.add_argument('--' + option, required=True)
    parser.add_argument('--profiles', nargs='+', choices=('single-tls', 'dual-tls'), default=['single-tls', 'dual-tls'])
    parser.add_argument('--output', type=Path, required=True)
    args = parser.parse_args()
    require(len(args.profiles) == len(set(args.profiles)), 'duplicate requested profile')
    report = {'schemaVersion': 1, 'evidenceKind': 'previous-release-to-candidate-upgrade',
              'dateUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'platform': platform.platform(),
              'releaseAssetCertification': False, 'passed': False, 'profiles': [],
              'harnesses': {p.name: PROTOCOL.digest(p) for p in (Path(__file__), ROOT / 'buildscripts/test-console-integration.py', ROOT / 'buildscripts/local_http.py', ROOT / 'buildscripts/console_acceptance_protocol.py')}}
    try:
        old_server = manifest_bound(args.previous_manifest, args.previous_release, args.previous_source)
        old_oc = manifest_bound(args.previous_oc_manifest, args.previous_oc_release, kind='oc')
        old_server['verifiedAssetSHA256'] = checksum_bound(args.previous_server, args.previous_checksums)
        old_oc['verifiedArchiveSHA256'] = checksum_bound(args.previous_oc_archive, args.previous_oc_checksums)
        checksum_bound(args.previous_oc_manifest, args.previous_oc_checksums)
        archive_bound(args.previous_oc_archive, {'oc': args.previous_cli, 'oc-console': args.previous_console})
        report['previousReleaseManifests'] = {'server': old_server, 'oc': old_oc}
        report['programs'] = {
            'previousServer': program_identity(args.previous_server, args.previous_source, 'release-asset'),
            'previousCLI': program_identity(args.previous_cli, old_oc['source'], 'release-asset'),
            'previousConsole': program_identity(args.previous_console, old_oc['source'], 'release-asset'),
            'server': program_identity(args.server, args.server_source, 'candidate-source-build'),
            'cli': program_identity(args.cli, args.cli_source, 'candidate-source-build'),
            'console': program_identity(args.console, args.cli_source, 'candidate-source-build')}
        for profile in args.profiles:
            result = run_profile(args, profile)
            report['profiles'].append(result)
            if result.get('interrupted'):
                report['interrupted'] = True
                break
        report['passed'] = (len(report['profiles']) == len(args.profiles) and
                            all(p['status'] == 'passed' and p['cleanup'] == 'passed' for p in report['profiles']))
    except (Exception, KeyboardInterrupt) as error:
        report['preparationErrorClass'] = type(error).__name__
        if isinstance(error, KeyboardInterrupt): report['interrupted'] = True
        if isinstance(error, RuntimeError): report['reason'] = str(error)
    finally:
        args.output.parent.mkdir(parents=True, exist_ok=True)
        args.output.write_text(json.dumps(report, indent=2) + '\n')
        print(json.dumps({'passed': report['passed'], 'profiles': [{k: p.get(k) for k in ('profile', 'status', 'phase', 'reason', 'cleanup')} for p in report['profiles']], 'preparationErrorClass': report.get('preparationErrorClass'), 'reason': report.get('reason')}))
    return 0 if report['passed'] else 1


if __name__ == '__main__':
    raise SystemExit(main())
