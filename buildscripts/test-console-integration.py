#!/usr/bin/env python3
"""Console acceptance against disposable real OtterIO instances.

No external Python dependencies. Never reads a user's OC configuration. All
fixtures and credentials are temporary, and all processes stop in finally.
"""
import argparse
import datetime
import hashlib
import hmac
import http.cookiejar
import json
import os
from pathlib import Path
import platform
import secrets
import signal
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

from local_http import local_urlopen


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def certificate(directory):
    directory.mkdir(parents=True)
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                    '-keyout', str(directory / 'private.key'),
                    '-out', str(directory / 'public.crt'), '-days', '1',
                    '-subj', '/CN=localhost', '-addext',
                    'subjectAltName=IP:127.0.0.1,DNS:localhost'],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return directory / 'public.crt'


def digest(path):
    with open(path, 'rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def verify_binary_identity(build_info, sdk_pin, server_source):
    for name in ('oc', 'oc-console'):
        dependencies = build_info[name].get('Deps', [])
        if any('Replace' in item for item in dependencies):
            raise ValueError(f'{name} binary uses a dependency replacement')
        sdk = next((item for item in dependencies if item['Path'] == 'github.com/soulteary/otterio'), None)
        if sdk is None or sdk.get('Version') != sdk_pin:
            raise ValueError(f'{name} binary SDK differs from the requested module pin')
    settings = {item['Key']: item.get('Value', '') for item in build_info['otterio'].get('Settings', [])}
    revision = settings.get('vcs.revision')
    if revision and revision != server_source:
        raise ValueError('server binary VCS revision differs from the supplied source')
    if revision:
        source_kind = 'verified-base-vcs-revision' if settings.get('vcs.modified') == 'true' else 'verified-vcs-revision'
    else:
        # Module archives have no .git directory. Keep the declared source and
        # binary hash separate rather than asserting a binary-derived revision.
        source_kind = 'declared-module-source-without-binary-vcs'
    return {'sdkPin': 'verified-client-build-info', 'serverSource': source_kind}


def run_fixture_cli(command, env, credentials):
    # TimeoutExpired includes the entire argv (including user-add secrets).
    # Suppress its exception chain as well as the report's error text.
    try:
        result = subprocess.run(command, env=env, capture_output=True, timeout=30)
    except subprocess.TimeoutExpired:
        raise RuntimeError('fixture CLI exceeded its 30-second deadline') from None
    if result.returncode:
        detail = (result.stdout + result.stderr).decode(errors='replace')
        for credential in credentials:
            detail = detail.replace(credential, '[redacted]')
        raise RuntimeError(f'fixture CLI failed: {detail[:1500]}')
    return result.stdout


def stop(process):
    if process is None or process.poll() is not None:
        return
    process.terminate()
    try:
        process.wait(timeout=12)
    except subprocess.TimeoutExpired:
        process.kill()
        process.wait(timeout=3)


def signed_request(endpoint, method, uri, payload, access, secret, context, query='', extra_headers=None, service='s3'):
    """Sign raw S3 requests without path cleaning or implicit retries."""
    now = datetime.datetime.now(datetime.timezone.utc)
    stamp, day = now.strftime('%Y%m%dT%H%M%SZ'), now.strftime('%Y%m%d')
    payload_hash = hashlib.sha256(payload).hexdigest()
    host = urllib.parse.urlsplit(endpoint).netloc
    signing_headers = {'host': host, 'x-amz-content-sha256': payload_hash, 'x-amz-date': stamp,
                       **{key.lower(): value for key, value in (extra_headers or {}).items()}}
    signed_headers = ';'.join(sorted(signing_headers))
    canonical_headers = ''.join(f'{key}:{signing_headers[key]}\n' for key in sorted(signing_headers))
    canonical = f'{method}\n{uri}\n{query}\n{canonical_headers}\n{signed_headers}\n{payload_hash}'
    scope = f'{day}/us-east-1/{service}/aws4_request'
    signing_key = ('AWS4' + secret).encode()
    for value in (day, 'us-east-1', service, 'aws4_request'):
        signing_key = hmac.new(signing_key, value.encode(), hashlib.sha256).digest()
    string_to_sign = f'AWS4-HMAC-SHA256\n{stamp}\n{scope}\n{hashlib.sha256(canonical.encode()).hexdigest()}'
    signature = hmac.new(signing_key, string_to_sign.encode(), hashlib.sha256).hexdigest()
    request = urllib.request.Request(endpoint + uri + ('?' + query if query else ''), data=payload, method=method, headers={
        'X-Amz-Date': stamp, 'X-Amz-Content-Sha256': payload_hash,
        'Authorization': f'AWS4-HMAC-SHA256 Credential={access}/{scope}, SignedHeaders={signed_headers}, Signature={signature}',
        'Content-Type': 'application/octet-stream',
        **(extra_headers or {}),
    })
    try:
        response = local_urlopen(request, timeout=20, context=context)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, response.read(), response.headers


def put_object(endpoint, bucket, key, payload, access, secret, context):
    """Sign PUT directly so CLI path cleaning cannot alter unusual fixture keys."""
    uri = '/' + urllib.parse.quote(bucket + '/' + key, safe='/-_.~')
    status, payload, _ = signed_request(endpoint, 'PUT', uri, payload, access, secret, context)
    if status != 200:
        detail = payload[:2000].decode(errors='replace')
        for credential in (access, secret):
            detail = detail.replace(credential, '[redacted]')
        raise RuntimeError(f'fixture PUT {key!r}: HTTP {status}: {detail}')


def scenario(args, name, preview=False, record=None):
    split, tls = name.startswith('dual'), name.endswith('tls')
    checks = []
    record = {} if record is None else record
    record.update(scenario=name, status='running', checks=checks)
    server = console = restricted_console = write_console = restricted_writer = None
    with tempfile.TemporaryDirectory(prefix='oc-console-real-') as temp:
        root = Path(temp)
        cfg_dir = root / 'config'
        cfg_dir.mkdir()
        access, secret = 'console-' + secrets.token_hex(6), secrets.token_hex(24)
        restricted_access, restricted_secret = 'viewer-' + secrets.token_hex(6), secrets.token_hex(24)
        credentials = [access, secret, restricted_access, restricted_secret]
        env = {k: v for k, v in os.environ.items() if not k.startswith(('OC_', 'MC_', 'OTTERIO_'))}
        env.update(OTTERIO_ROOT_USER=access, OTTERIO_ROOT_PASSWORD=secret,
                   OTTERIO_BROWSER='off', NO_PROXY='*', no_proxy='*')
        ports = [free_port(), free_port()] if split else [free_port()]
        while split and ports[0] == ports[1]:
            ports[1] = free_port()
        scheme = 'https' if tls else 'http'
        endpoint = f'{scheme}://127.0.0.1:{ports[0]}'
        admin = f'{scheme}://127.0.0.1:{ports[-1]}'
        s3cert_dir = root / 's3-certs'
        admincert_dir = root / 'admin-certs' if split else s3cert_dir
        s3ca = certificate(s3cert_dir) if tls else None
        adminca = certificate(admincert_dir) if split and tls else s3ca
        if not tls:
            s3cert_dir.mkdir()
        if tls:
            ca_dir = cfg_dir / 'certs' / 'CAs'
            ca_dir.mkdir(parents=True)
            (ca_dir / 's3.crt').write_bytes(s3ca.read_bytes())
        context = ssl.create_default_context(cafile=str(s3ca)) if tls else None
        config_path = cfg_dir / 'config.json'
        config_path.write_text(json.dumps({'version': '10', 'aliases': {
            'store': {'url': endpoint, 'adminURL': admin, 'adminCAFile': str(adminca) if adminca else '',
                      'accessKey': access, 'secretKey': secret, 'api': 'S3v4', 'path': 'on'},
            'viewer': {'url': endpoint, 'adminURL': admin, 'adminCAFile': str(adminca) if adminca else '',
                       'accessKey': restricted_access, 'secretKey': restricted_secret, 'api': 'S3v4', 'path': 'on'},
        }}))
        config_path.chmod(0o600)
        original_config = config_path.read_bytes()
        log_files = []

        def cli(*options):
            return run_fixture_cli([args.cli, '--config-dir', str(cfg_dir), '--no-color', *options], env, credentials)

        def start_console(alias, writes=False, config_dir=cfg_dir, sharing=False):
            output_path = root / (alias + ('-writer' if writes else '') + '-console.log')
            log = output_path.open('wb')
            log_files.append(log)
            command = [args.console, '--config-dir', str(config_dir), '--alias', alias, '--address', '127.0.0.1:0']
            if writes:
                command.append('--allow-writes')
            if sharing:
                command += ['--allow-sharing', '--share-url', endpoint]
            process = subprocess.Popen(command, env=env, stdout=log, stderr=log)
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                output = output_path.read_text(errors='replace')
                if any(credential in output for credential in credentials):
                    stop(process)
                    raise RuntimeError('console startup disclosed storage credentials')
                fields = dict(line.split(': ', 1) for line in output.splitlines() if ': ' in line)
                if 'URL' in fields and 'Login code' in fields:
                    return process, fields['URL'], fields['Login code']
                if process.poll() is not None:
                    raise RuntimeError('console did not start: ' + output[:500])
                time.sleep(0.05)
            stop(process)
            raise RuntimeError('console startup timeout')

        def browser(base):
            jar = http.cookiejar.CookieJar()
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(jar))

            def request(path, method='GET', body=None, expected=200, headers=None, allowed_credentials=()):
                request_headers = {'Origin': base, **(headers or {})}
                data = None
                if body is not None:
                    if isinstance(body, bytes):
                        data = body
                        request_headers['Content-Type'] = 'application/octet-stream'
                    else:
                        data = json.dumps(body).encode()
                        request_headers['Content-Type'] = 'application/json'
                req = urllib.request.Request(base + path, data=data, method=method, headers=request_headers)
                try:
                    response = opener.open(req, timeout=60)
                except urllib.error.HTTPError as error:
                    response = error
                with response:
                    payload = response.read()
                    if any(credential.encode() in payload for credential in credentials if credential not in allowed_credentials):
                        raise RuntimeError('browser response disclosed storage credentials')
                    accepted = expected if isinstance(expected, tuple) else (expected,)
                    if expected is not None and response.status not in accepted:
                        raise RuntimeError(f'{method} {path.split("?")[0]}: expected {expected}, got {response.status}: {payload[:300]!r}')
                    return payload, response.headers
            request.jar = jar
            request.base = base
            return request

        try:
            log = (root / 'server.log').open('wb')
            log_files.append(log)
            command = [args.server, 'server', '--address', f'127.0.0.1:{ports[0]}', '--certs-dir', str(s3cert_dir)]
            if split:
                command += ['--console-address', f'127.0.0.1:{ports[1]}']
                if tls:
                    command += ['--console-certs-dir', str(admincert_dir)]
            command += [str(root / f'data-{i}') for i in range(4)] if args.writes or args.settings or args.features or args.version_copy else [str(root / 'data')]
            server = subprocess.Popen(command, env=env, stdout=log, stderr=log)
            deadline = time.monotonic() + 30
            while True:
                if server.poll() is not None:
                    raise RuntimeError('disposable OtterIO exited before readiness')
                try:
                    with local_urlopen(endpoint + '/otterio/health/ready', timeout=1, context=context) as response:
                        if response.status == 200:
                            break
                except (OSError, urllib.error.HTTPError):
                    pass
                if time.monotonic() > deadline:
                    raise RuntimeError('OtterIO readiness timeout')
                time.sleep(0.1)
            # Browser=off must remove legacy routes without removing Admin.
            legacy_endpoint = admin
            legacy_context = ssl.create_default_context(cafile=str(adminca)) if tls else None
            for method, path in [('GET', '/otterio/'),
                                 ('POST', '/otterio/webrpc'),
                                 ('PUT', '/otterio/upload/console-fixture/legacy.txt'),
                                 ('GET', '/otterio/download/console-fixture/legacy.txt?token=invalid'),
                                 ('POST', '/otterio/zip?token=invalid')]:
                req = urllib.request.Request(legacy_endpoint + path, method=method,
                    headers={'User-Agent': 'Mozilla/5.0', 'Content-Type': 'application/json'},
                    data=b'{"id":1,"jsonrpc":"2.0","method":"web.Login","params":{"username":"disabled-web-probe","password":"disabled-web-probe"}}' if method == 'POST' else (b'probe' if method == 'PUT' else None))
                try:
                    with local_urlopen(req, timeout=5, context=legacy_context) as response:
                        raise RuntimeError(f'legacy Web route unexpectedly available: {method} {path}: {response.status}')
                except urllib.error.HTTPError as error:
                    payload = error.read()
                    try:
                        protocol_error = ET.fromstring(payload).findtext('Code')
                    except ET.ParseError:
                        protocol_error = None
                    if error.code != 404 and (error.code not in (400, 403) or not protocol_error):
                        raise RuntimeError(f'legacy Web route expected storage protocol rejection: {method} {path}: {error.code}') from None
            checks.append('browser off rejects legacy assets, RPC, upload, download and ZIP routes')
            bucket = 'console-fixture'
            cli('mb', 'store/' + bucket)
            if preview and (args.writes or args.features):
                cli('version', 'enable', 'store/' + bucket)
            objects = {'empty.txt': b'', 'plain.txt': b'console acceptance\n',
                       '中文/含 空格+%.txt': '原样对象内容'.encode(),
                       'nested/literal.txt': b'nested key preserved',
                       'percent%2Fplus+ space.txt': b'literal percent plus space',
                       'stream.bin': b'0123456789abcdef' * 65536}
            objects.update({f'page-{i:02}.txt': str(i).encode() for i in range(105 if preview else 5)})
            for key, payload in objects.items():
                put_object(endpoint, bucket, key, payload, access, secret, context)
            checks.append('real S3 fixture with unusual literal keys')
            console, base, code = start_console('store',
                writes=preview and (args.writes or args.features), sharing=preview and args.features)
            if preview:
                print(json.dumps({'previewURL': base, 'loginCode': code, 'alias': 'store', 'fixtureConfig': str(config_path), 'previewPID': os.getpid()}), flush=True)
                while True:
                    time.sleep(1)
            request = browser(base)
            request('/api/buckets', expected=401)
            request('/api/login', 'POST', {'code': 'wrong'}, expected=401)
            request('/api/login', 'POST', {'code': code}, expected=403, headers={'Origin': 'http://evil.example'})
            reply, headers = request('/api/login', 'POST', {'code': code})
            session = json.loads(reply)
            assert session['alias'] == 'store' and session['readOnly'] is True
            assert 'HttpOnly' in headers['Set-Cookie'] and 'SameSite=Strict' in headers['Set-Cookie']
            checks.append('anonymous denial, login code, Origin and session cookie')
            page, headers = request('/')
            assert b'/app.js' in page and headers['X-Frame-Options'] == 'DENY'
            assert "script-src 'self'" in headers['Content-Security-Policy'] and headers['Cache-Control'] == 'no-store'
            request('/app.js')
            request('/style.css')
            request('/api/no-such-route', expected=404)
            request('/api/upload', 'POST', {}, expected=404)
            request('/api/buckets', 'POST', {}, expected=405)
            request('/api/buckets', expected=403, headers={'Host': 'evil.example'})
            checks.append('embedded assets, security headers, readonly route surface and Host')
            buckets = json.loads(request('/api/buckets')[0])['buckets']
            assert [item['name'] for item in buckets] == [bucket]
            cursor, seen, cursor_seen = '', [], set()
            while True:
                query = urllib.parse.urlencode({'bucket': bucket, 'limit': 2, 'cursor': cursor})
                page = json.loads(request('/api/objects?' + query)[0])
                assert len(page['entries']) <= 2
                seen.extend((item['key'], item['isPrefix']) for item in page['entries'])
                cursor = page.get('nextCursor', '')
                if not cursor:
                    break
                assert cursor not in cursor_seen
                cursor_seen.add(cursor)
            expected_root = {(key.split('/')[0] + '/', True) if '/' in key else (key, False) for key in objects}
            assert set(seen) == expected_root and len(seen) == len(expected_root) and cursor_seen
            checks.append('real continuation-token pagination without duplicates')
            for prefix, expected_key in [('中文/', '中文/含 空格+%.txt'), ('nested/', 'nested/literal.txt')]:
                query = urllib.parse.urlencode({'bucket': bucket, 'prefix': prefix})
                entries = json.loads(request('/api/objects?' + query)[0])['entries']
                assert len(entries) == 1 and entries[0]['key'] == expected_key
            for key, original in objects.items():
                query = urllib.parse.urlencode({'bucket': bucket, 'key': key})
                downloaded, headers = request('/api/download?' + query)
                assert hashlib.sha256(downloaded).digest() == hashlib.sha256(original).digest()
                assert headers['Content-Disposition'].startswith('attachment;')
                assert int(headers['Content-Length']) == len(original)
            query = urllib.parse.urlencode({'bucket': bucket, 'key': 'absent.txt'})
            request('/api/download?' + query, expected=404)
            checks.append('prefix fidelity, empty/special/1MiB download hashes and pre-header missing object error')
            account = json.loads(request('/api/account')[0])
            assert set(account) == {'buckets'} and any(item['name'] == bucket for item in account['buckets'])
            assert all(set(item) == {'name', 'size', 'read', 'write'} for item in account['buckets'])
            checks.append('signed AccountInfo whitelist through selected management endpoint/CA')
            policy_path = root / 'viewer-policy.json'
            policy_path.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
                {'Effect': 'Allow', 'Action': ['s3:ListBucket'], 'Resource': ['arn:aws:s3:::' + bucket]},
                {'Effect': 'Allow', 'Action': ['s3:GetObject'], 'Resource': ['arn:aws:s3:::' + bucket + '/plain.txt']},
            ]}))
            cli('admin', 'user', 'add', 'store', restricted_access, restricted_secret)
            cli('admin', 'policy', 'add', 'store', 'console-viewer', str(policy_path))
            cli('admin', 'policy', 'set', 'store', 'console-viewer', 'user=' + restricted_access)
            restricted_console, restricted_base, restricted_code = start_console('viewer')
            viewer = browser(restricted_base)
            viewer('/api/login', 'POST', {'code': restricted_code})
            assert json.loads(viewer('/api/buckets')[0])['buckets'][0]['name'] == bucket
            query = urllib.parse.urlencode({'bucket': bucket, 'key': 'plain.txt'})
            assert viewer('/api/download?' + query)[0] == objects['plain.txt']
            query = urllib.parse.urlencode({'bucket': bucket, 'key': 'stream.bin'})
            viewer('/api/download?' + query, expected=403)
            checks.append('restricted identity: permitted read succeeds, different object denied')
            if args.settings or args.settings_legacy:
                from console_settings_acceptance import verify_settings
                record['settingsMetrics'] = verify_settings(
                    root, bucket, cli, request, session, viewer, start_console, browser, stop,
                    credentials, endpoint, access, secret, context, signed_request,
                    checks, legacy=args.settings_legacy, base_protocol=args.settings_base)
            if args.version_copy:
                from version_copy_acceptance import verify_version_copy
                record['versionCopyMetrics'] = verify_version_copy(
                    root, bucket, cli, credentials, endpoint, access, secret, context,
                    signed_request, put_object, checks)
            if args.writes:
                from console_write_acceptance import verify_writes
                write_metrics = {}
                record['writeMetrics'] = write_metrics
                request('/api/uploads', 'POST', {'bucket': bucket, 'key': 'readonly-denied', 'size': 1},
                        expected=403, headers={'X-CSRF-Token': session['csrfToken']})
                write_console, write_base, write_code = start_console('store', writes=True)
                restricted_writer, viewer_write_base, viewer_write_code = start_console('viewer', writes=True)
                write_request, viewer_write = browser(write_base), browser(viewer_write_base)
                write_session = json.loads(write_request('/api/login', 'POST', {'code': write_code})[0])
                viewer_session = json.loads(viewer_write('/api/login', 'POST', {'code': viewer_write_code})[0])
                write_metrics = verify_writes(root, bucket, cli, write_base, write_request, write_session, viewer_write, viewer_session,
                              endpoint, access, secret, context, signed_request, put_object, checks,
                              start_console, browser, stop, credentials, write_console, metrics=write_metrics)
            if args.features:
                from console_features_acceptance import verify_features
                record['featureMetrics'] = verify_features(
                    root, bucket, cli, request, session, viewer, start_console, browser, stop,
                    credentials, endpoint, access, secret, context, signed_request, put_object,
                    checks, legacy=args.features_legacy)
            request('/api/logout', 'POST', expected=403)
            request('/api/logout', 'POST', expected=204, headers={'X-CSRF-Token': session['csrfToken']})
            request('/api/session', expected=401)
            request('/api/buckets', expected=401)
            assert config_path.read_bytes() == original_config
            checks.append('CSRF logout, revocation and unchanged CLI config')
            stop(restricted_console)
            restricted_console = None
            shutdown_start = time.monotonic()
            stop(console)
            assert console.returncode == 0 and time.monotonic() - shutdown_start < 5
            console = None
            checks.append('bounded graceful console process shutdown')
            record['status'] = 'passed'
            return record
        finally:
            stop(restricted_writer)
            stop(write_console)
            stop(restricted_console)
            stop(console)
            stop(server)
            for log in log_files:
                log.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cli', required=True)
    parser.add_argument('--console', required=True)
    parser.add_argument('--server', required=True)
    parser.add_argument('--server-source', required=True, help='source revision, checked against binary VCS when present')
    parser.add_argument('--sdk-pin', help='module version used to build the client; defaults to the compatibility manifest')
    parser.add_argument('--server-patch', action='append', default=[], help='patch applied on top of the recorded server source (repeatable)')
    parser.add_argument('--output', required=True)
    parser.add_argument('--scenarios', default='single-http,dual-http,dual-tls')
    parser.add_argument('--preview', action='store_true', help='hold the last disposable scenario for manual browser QA')
    parser.add_argument('--writes', action='store_true', help='also test opt-in uploads and deletes on disposable four-disk storage')
    parser.add_argument('--features', action='store_true', help='test bucket/history/sharing/ZIP/IAM features on disposable four-disk storage')
    parser.add_argument('--features-legacy', action='store_true', help='with --features, verify protected features refuse the unpatched server')
    parser.add_argument('--version-copy', action='store_true', help='verify source version authorization and copied bytes for CopyObject and UploadPartCopy')
    parser.add_argument('--settings-base', action='store_true', help='with --settings, verify the base protocol refuses lifecycle runtime features')
    settings_group = parser.add_mutually_exclusive_group()
    settings_group.add_argument('--settings', action='store_true', help='test v1 bucket CAS and own IAM rotation on disposable four-disk storage')
    settings_group.add_argument('--settings-legacy', action='store_true', help='test safe read-only fallback on the current unpatched module pin')
    args = parser.parse_args()
    if args.features_legacy and not args.features:
        parser.error('--features-legacy requires --features')
    if args.settings_base and not args.settings:
        parser.error('--settings-base requires --settings')
    names = args.scenarios.split(',')
    if any(name not in ('single-http', 'dual-http', 'single-tls', 'dual-tls') for name in names):
        parser.error('unknown scenario')
    report = {'dateUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'platform': platform.platform(),
              'serverSource': args.server_source, 'sdkPin': args.sdk_pin,
              'binaries': {},
              'scenarios': [], 'status': 'running'}
    report['scope'] = 'read-only-and-writes' if args.writes else ('read-only-and-protected-settings' if args.settings else 'read-only')
    if args.features:
        report['scope'] = 'console-features-legacy-fallback' if args.features_legacy else 'console-five-features'
    if args.settings or args.settings_legacy:
        report['settingsProfile'] = 'legacy-read-only-fallback' if args.settings_legacy else ('v1-base-protected-settings' if args.settings_base else 'v1-protected-settings')
    if args.version_copy:
        report['scope'] += '-and-version-copy'
    report['storage'] = 'single-node-four-disk-erasure' if args.writes or args.settings or args.features or args.version_copy else 'filesystem'
    harness_paths = [Path(__file__), Path(__file__).with_name('console_write_acceptance.py'),
                     Path(__file__).with_name('local_http.py')]
    if args.settings or args.settings_legacy:
        settings_path = Path(__file__).with_name('console_settings_acceptance.py')
        harness_paths.append(settings_path)
    if args.features:
        features_path = Path(__file__).with_name('console_features_acceptance.py')
        harness_paths.append(features_path)
    if args.version_copy:
        copy_path = Path(__file__).with_name('version_copy_acceptance.py')
        harness_paths.append(copy_path)
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    result = {}
    try:
        output.write_text(json.dumps(report, indent=2) + '\n')
        report['sdkPin'] = args.sdk_pin or json.loads((Path(__file__).resolve().parents[1] / 'docs' / 'compatibility.json').read_text())['otterioSDK']
        report['binaries'] = {name: {'sha256': digest(path)} for name, path in
                              [('oc', args.cli), ('oc-console', args.console), ('otterio', args.server)]}
        report['harnesses'] = [{'name': path.name, 'sha256': digest(path)} for path in harness_paths]
        if args.server_patch:
            report['serverPatches'] = [{'name': Path(patch).name, 'sha256': digest(patch)} for patch in args.server_patch]
        build_info = {name: json.loads(subprocess.check_output(['go', 'version', '-m', '-json', path], text=True))
                      for name, path in [('oc', args.cli), ('oc-console', args.console), ('otterio', args.server)]}
        for name, info in build_info.items():
            report['binaries'][name]['buildInfo'] = info
        report['identityEvidence'] = verify_binary_identity(build_info, report['sdkPin'], args.server_source)
        for index, name in enumerate(names):
            result = {'scenario': name, 'status': 'running'}
            report['scenarios'].append(result)
            scenario(args, name, args.preview and index == len(names) - 1, record=result)
            output.write_text(json.dumps(report, indent=2) + '\n')
            print(f'{name}: {len(result["checks"])} acceptance groups passed', flush=True)
        report['status'] = 'passed'
    except Exception as error:
        report['status'] = 'failed'
        result['status'] = 'failed'
        report['error'] = str(error)
        raise
    except BaseException:
        report['status'] = 'interrupted'
        result['status'] = 'interrupted'
        raise
    finally:
        output.write_text(json.dumps(report, indent=2) + '\n')


if __name__ == '__main__':
    signal.signal(signal.SIGTERM, lambda *_: (_ for _ in ()).throw(SystemExit(1)))
    main()
