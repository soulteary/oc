#!/usr/bin/env python3
"""Run OC against disposable real OtterIO servers; never use a user's aliases."""
import argparse
from stability_checks import stability_checks
import hashlib
import hmac
import datetime
import gzip
import zipfile
import json
import os
from pathlib import Path
import secrets
import selectors
import platform
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def certificate(directory):
    directory.mkdir(parents=True, exist_ok=True)
    subprocess.run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes',
                    '-keyout', str(directory / 'private.key'), '-out', str(directory / 'public.crt'),
                    '-days', '1', '-subj', '/CN=localhost',
                    '-addext', 'subjectAltName=IP:127.0.0.1,DNS:localhost'],
                   check=True, stdout=subprocess.DEVNULL, stderr=subprocess.DEVNULL)
    return directory / 'public.crt'


def digest(path):
    with path.open('rb') as source:
        return hashlib.file_digest(source, 'sha256').hexdigest()


def migration_checks(oc):
    checks = 0
    with tempfile.TemporaryDirectory(prefix='oc-migrate-') as temp:
        root = Path(temp)
        legacy = root / 'mc' / 'config.json'
        legacy.parent.mkdir()
        original = json.dumps({'version': '10', 'aliases': {'migrated': {
            'url': 'http://127.0.0.1:9000', 'accessKey': 'migration-key',
            'secretKey': 'migration-secret', 'api': 'S3v4', 'path': 'on',
            'adminURL': 'https://127.0.0.1:9001', 'adminCAFile': 'ca.pem'}}})
        legacy.write_text(original)
        config = root / 'oc'
        env = {k: v for k, v in os.environ.items() if not k.startswith(('OC_', 'MC_'))}
        env.update(OC_CONFIG_DIR=str(config), MC_CONFIG_DIR=str(root / 'unused'), NO_PROXY='*')
        def run(*args, failure=False):
            nonlocal checks
            result = subprocess.run([oc, '--json', '--no-color', *args], env=env,
                                    stdout=subprocess.PIPE, stderr=subprocess.PIPE, timeout=15)
            checks += 1
            if (result.returncode != 0) != failure:
                raise RuntimeError('migration command failed: ' + str(args[:2]))
            return result.stdout
        run('alias', 'list')
        invalid_timeout = json.loads(run('admin', 'service', 'restart', '--timeout', '0s', 'local', failure=True))
        assert invalid_timeout['status'] == 'error'
        checks += 1
        before = (config / 'config.json').read_bytes()
        imported = json.loads(run('config', 'import', str(legacy)))
        assert imported == {'status': 'success', 'aliases': 1}
        assert legacy.read_text() == original and not (root / 'unused').exists()
        checks += 2
        backups = list(config.glob('config.json.backup-*'))
        assert len(backups) == 1 and backups[0].read_bytes() == before
        checks += 1
        saved = json.loads((config / 'config.json').read_text())
        assert saved['aliases']['migrated']['adminCAFile'] == str(legacy.parent / 'ca.pem')
        checks += 1
        listed = [json.loads(line) for line in run('alias', 'list', 'migrated').splitlines()]
        assert listed[0]['alias'] == 'migrated'
        checks += 1
        explicit = root / 'explicit'
        run('--config-dir', str(explicit), 'alias', 'list')
        assert (explicit / 'config.json').exists()
        checks += 1
        invalid = root / 'invalid.json'
        invalid.write_text('{"version":"9","aliases":{}}')
        error = json.loads(run('config', 'import', str(invalid), failure=True))
        assert error['status'] == 'error' and json.loads((config / 'config.json').read_text()) == saved
        checks += 1
        for endpoint in ['http://localhost:badport', 'http://bad host', 'http://localhost?x=1', 'http://localhost#fragment']:
            invalid.write_text(json.dumps({'version': '10', 'aliases': {'bad': {'url': endpoint, 'api': 'S3v4'}}}))
            run('config', 'import', str(invalid), failure=True)
            assert json.loads((config / 'config.json').read_text()) == saved
            assert len(list(config.glob('config.json.backup-*'))) == 1
            checks += 1
    print(f'migration: {checks} checks passed', flush=True)
    return {'scenario': 'migration', 'checks': checks, 'status': 'passed'}


def scenario(oc, otterio, split, tls, public, extended=False, stability=False, soak_seconds=0, record=None, artifacts=None):
    record = record if record is not None else {}
    name = ('dual' if split else 'single') + ('-tls' if tls else '-http') + ('-public' if public else '')
    if extended:
        name += '-erasure-extended'
    record.update(scenario=name, status="running", checks=0)
    checks = 0
    stability_result = None
    with tempfile.TemporaryDirectory(prefix='oc-core-') as temp:
        root = Path(temp)
        config = root / 'config'
        certs = root / 'server-certs'
        certs.mkdir()
        s3ca = certificate(certs) if tls else None
        admincerts = root / 'admin-certs' if split else certs
        adminca = certificate(admincerts) if split and tls else s3ca
        if tls:
            client_cas = config / 'certs' / 'CAs'
            client_cas.mkdir(parents=True)
            shutil.copyfile(s3ca, client_cas / 's3.crt')
        access = 'oc-test-' + secrets.token_hex(6)
        secret = secrets.token_hex(24)
        restricted_secret = secrets.token_hex(24)
        secrets_to_redact = [access, secret, restricted_secret]
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('MC_', 'OC_', 'OTTERIO_'))}
        env.update(OTTERIO_ROOT_USER=access, OTTERIO_ROOT_PASSWORD=secret,
                   OTTERIO_BROWSER='off', NO_PROXY='*', no_proxy='*')
        if public:
            env['OTTERIO_PROMETHEUS_AUTH_TYPE'] = 'public'
        ports = [free_port(), free_port()] if split else [free_port()]
        while split and ports[0] == ports[1]:
            ports[1] = free_port()
        scheme = 'https' if tls else 'http'
        s3 = f'{scheme}://127.0.0.1:{ports[0]}'
        admin = f'{scheme}://127.0.0.1:{ports[-1]}'
        command = [otterio, 'server', '--address', f'127.0.0.1:{ports[0]}', '--certs-dir', str(certs)]
        if split:
            command += ['--console-address', f'127.0.0.1:{ports[1]}']
            if tls:
                command += ['--console-certs-dir', str(admincerts)]
        data = root / 'data'
        data.mkdir()
        if extended:
            command += [str(data / ('disk' + str(i))) for i in range(4)]
        else:
            command += [str(data)]
        context = ssl.create_default_context(cafile=str(s3ca)) if tls else None
        log_path = root / 'server.log'
        with log_path.open('wb') as log:
            server = subprocess.Popen(command, env=env, stdout=log, stderr=log)
            try:
                deadline = time.monotonic() + 30
                while True:
                    if server.poll() is not None:
                        raise RuntimeError('server exited: ' + log_path.read_text().replace(secret, 'REDACTED').replace(access, 'REDACTED'))
                    try:
                        with urllib.request.urlopen(s3 + '/otterio/health/ready', context=context, timeout=1) as response:
                            if response.status == 200:
                                break
                    except (OSError, urllib.error.HTTPError):
                        pass
                    if time.monotonic() >= deadline:
                        raise RuntimeError('server readiness timed out')
                    time.sleep(0.2)

                def run(*args, failure=False, extra_env=None, output=None, working_dir=None, input_data=None, command_timeout=45):
                    nonlocal checks
                    record["lastCommand"] = list(args[:2])
                    child_env = dict(env)
                    child_env.update(extra_env or {})
                    try:
                        completed = subprocess.run([oc, '--config-dir', str(config), '--no-color', *args],
                                                   env=child_env, stdout=output or subprocess.PIPE,
                                                   stderr=subprocess.PIPE, timeout=command_timeout, cwd=working_dir, input=input_data)
                    except subprocess.TimeoutExpired:
                        diagnostic = log_path.read_text(errors='replace')
                        for credential in secrets_to_redact:
                            diagnostic = diagnostic.replace(credential, 'REDACTED')
                        Path(artifacts or tempfile.gettempdir(), name + '-timeout.log').write_text(diagnostic)
                        raise RuntimeError(f'{name}: {args[:2]} timed out; sanitized server diagnostic saved') from None
                    checks += 1
                    record["checks"] = checks
                    if (completed.returncode != 0) != failure:
                        message = completed.stderr.decode(errors='replace')
                        if completed.stdout and isinstance(completed.stdout, bytes):
                            message += completed.stdout.decode(errors='replace')
                        for credential in secrets_to_redact:
                            message = message.replace(credential, 'REDACTED')
                        diagnostic = log_path.read_text(errors='replace')
                        for credential in secrets_to_redact:
                            diagnostic = diagnostic.replace(credential, 'REDACTED')
                        Path(artifacts or tempfile.gettempdir(), name + '-failure.log').write_text(diagnostic)
                        raise RuntimeError(f'{name}: {args[:2]} returned {completed.returncode}: {message[:2000]}')
                    return completed.stdout

                def emit_console_error():
                    # A signed malformed encrypted body generates a real
                    # OtterIO log without creating or changing a user.
                    payload = b'console-compatibility-invalid-ciphertext'
                    now = datetime.datetime.now(datetime.timezone.utc)
                    stamp, day = now.strftime('%Y%m%dT%H%M%SZ'), now.strftime('%Y%m%d')
                    endpoint = urllib.parse.urlsplit(admin)
                    path, query = '/otterio/admin/v3/add-user', 'accessKey=console-probe'
                    body_hash = hashlib.sha256(payload).hexdigest()
                    headers = {'host': endpoint.netloc, 'x-amz-content-sha256': body_hash, 'x-amz-date': stamp}
                    signed = ';'.join(sorted(headers))
                    canonical = '\n'.join(['PUT', path, query, ''.join(key + ':' + headers[key] + '\n' for key in sorted(headers)), signed, body_hash])
                    scope = day + '/us-east-1/s3/aws4_request'
                    to_sign = '\n'.join(['AWS4-HMAC-SHA256', stamp, scope, hashlib.sha256(canonical.encode()).hexdigest()])
                    key = ('AWS4' + secret).encode()
                    for part in [day, 'us-east-1', 's3', 'aws4_request']:
                        key = hmac.new(key, part.encode(), hashlib.sha256).digest()
                    headers['Authorization'] = 'AWS4-HMAC-SHA256 Credential=' + access + '/' + scope + ', SignedHeaders=' + signed + ', Signature=' + hmac.new(key, to_sign.encode(), hashlib.sha256).hexdigest()
                    request = urllib.request.Request(admin + path + '?' + query, data=payload, headers=headers, method='PUT')
                    try:
                        urllib.request.urlopen(request, context=ssl.create_default_context(cafile=str(adminca)) if tls else None, timeout=5)
                    except urllib.error.HTTPError as error:
                        result = json.loads(error.read())
                        assert error.code == 400 and 'BadJSON' in result['Code'], 'console probe did not reach decryption validation'
                    else:
                        raise AssertionError('malformed admin payload was accepted')


                options = ['--api', 's3v4', '--path', 'on']
                if split:
                    options += ['--admin-url', admin]
                if tls:
                    options += ['--admin-ca', os.path.relpath(adminca)]
                run('alias', 'set', 'test', s3, access, secret, *options)
                run('admin', 'info', 'test', working_dir=root)
                for doctor_options in ([], ['--online']):
                    diagnostic = json.loads(run('--json', 'doctor', *doctor_options, 'test'))
                    assert diagnostic['status'] == 'success'
                    assert diagnostic['separateAdmin'] == split
                    assert diagnostic['s3Scheme'] == scheme
                    assert all(credential not in json.dumps(diagnostic) for credential in secrets_to_redact)
                    if doctor_options:
                        assert diagnostic['serverCount'] >= 1
                    checks += 1
                run('doctor', '--online', failure=True)
                offline = json.loads(run('--json', '--admin-url', 'http://127.0.0.1:1', 'doctor', 'test'))
                assert offline['status'] == 'success' and not offline['online']
                failed = json.loads(run('--json', '--admin-url', 'http://127.0.0.1:1', 'doctor', '--online', 'test', failure=True))
                assert failed['status'] == 'error' and failed['errorCategory'] == 'network'
                assert all(credential not in json.dumps(failed) for credential in secrets_to_redact)
                checks += 2
                run('ls', 'test')
                run('mb', 'test/core-check')
                files = root / 'files'
                files.mkdir()
                (files / 'empty').write_bytes(b'')
                (files / 'small').write_bytes(b'otterio-core-check\n' * 4096)
                with (files / 'large').open('wb') as large:
                    block = bytes(range(256)) * 4096
                    for _ in range(65):
                        large.write(block)
                for local, object_name in [('empty', 'empty'), ('small', '中文 空格+#?.txt'), ('large', 'multipart')]:
                    target = 'test/core-check/' + object_name
                    run('cp', str(files / local), target)
                    downloaded = root / 'download'
                    with downloaded.open('wb') as destination:
                        run('cat', target, output=destination)
                    assert digest(files / local) == digest(downloaded), 'object digest mismatch'
                    checks += 1
                run('cp', 'test/core-check/multipart', 'test/core-check/copied')
                run('stat', 'test/core-check/copied')
                run('stat', 'test/core-check/missing', failure=True)
                sync = root / 'sync'
                sync.mkdir()
                (sync / 'keep').write_text('before')
                (sync / 'remove').write_text('remove')
                run('mirror', str(sync), 'test/core-check/mirror')
                (sync / 'keep').write_text('after')
                (sync / 'remove').unlink()
                run('mirror', '--overwrite', '--remove', str(sync), 'test/core-check/mirror')
                assert run('cat', 'test/core-check/mirror/keep') == b'after'
                run('stat', 'test/core-check/mirror/remove', failure=True)

                # Overrides are transient and use the same authenticated admin endpoint.
                run('--admin-url', admin, 'admin', 'info', 'test',
                    extra_env={'OC_ADMIN_URL': 'http://127.0.0.1:1'})
                run('admin', 'info', 'test',
                    extra_env={'OC_ADMIN_URL': 'http://127.0.0.1:1', 'OC_ADMIN_URL_test': admin})
                run('admin', 'info', 'test', extra_env={'OC_ADMIN_URL': admin})
                run('admin', 'info', 'test',
                    extra_env={'MC_HOST_test': f'{scheme}://{access}:{secret}@127.0.0.1:{ports[0]}'})
                if tls:
                    run('--admin-ca', str(adminca), 'admin', 'info', 'test',
                        extra_env={'OC_ADMIN_CA': str(root / 'missing-ca')})
                    run('admin', 'info', 'test',
                        extra_env={'OC_ADMIN_CA': str(root / 'missing-ca'), 'OC_ADMIN_CA_test': str(adminca)})
                    run('admin', 'info', 'test', extra_env={'OC_ADMIN_CA': str(adminca)})
                run('--admin-url', admin + '/unsupported', 'admin', 'info', 'test', failure=True)
                if split:
                    wrong_endpoint = ['--admin-url', s3]
                    if tls:
                        wrong_endpoint += ['--admin-ca', str(config / 'certs' / 'CAs' / 's3.crt')]
                    run(*wrong_endpoint, 'admin', 'info', 'test', failure=True)

                for kind in ('cluster', 'node', 'legacy'):
                    args = ['--json', 'admin', 'prometheus', 'generate', '--metrics-type', kind]
                    if tls:
                        args += ['--metrics-ca', str(s3ca)]
                    if public:
                        args += ['--public']
                    generated = json.loads(run(*args, 'test'))
                    assert generated['staticConfigs'][0]['targets'] == [f'127.0.0.1:{ports[0]}']
                    headers = {} if public else {'Authorization': 'Bearer ' + generated['bearerToken']}
                    request = urllib.request.Request(s3 + generated['metricsPath'], headers=headers)
                    metrics_context = None
                    if tls:
                        assert generated['tlsConfig']['caFile'] == str(s3ca)
                        metrics_context = ssl.create_default_context(cafile=generated['tlsConfig']['caFile'])
                    else:
                        assert 'tlsConfig' not in generated
                    with urllib.request.urlopen(request, context=metrics_context, timeout=10) as response:
                        assert response.status == 200 and response.read(), 'metrics not scrapeable'
                    checks += 1
                if not public:
                    try:
                        urllib.request.urlopen(s3 + '/otterio/v2/metrics/cluster', context=context, timeout=5)
                    except urllib.error.HTTPError as error:
                        assert error.code == 403
                        checks += 1
                    else:
                        raise AssertionError('unauthenticated metrics accepted')
                shared = json.loads(run('--json', 'share', 'download', 'test/core-check/mirror/keep'))
                with urllib.request.urlopen(shared['share'], context=context, timeout=10) as response:
                    assert response.read() == b'after'
                checks += 1

                run('admin', 'user', 'add', 'test', 'restricted-user', restricted_secret)
                policy = root / 'readonly.json'
                policy.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
                    {'Effect': 'Allow', 'Action': ['s3:GetBucketLocation', 's3:ListBucket'],
                     'Resource': ['arn:aws:s3:::core-check']},
                    {'Effect': 'Allow', 'Action': ['s3:GetObject'],
                     'Resource': ['arn:aws:s3:::core-check/*']}]}))
                run('admin', 'policy', 'add', 'test', 'core-readonly', str(policy))
                run('admin', 'policy', 'set', 'test', 'core-readonly', 'user=restricted-user')
                run('alias', 'set', 'restricted', s3, 'restricted-user', restricted_secret, *options)
                run('ls', 'restricted/core-check')
                assert run('cat', 'restricted/core-check/mirror/keep') == b'after'
                run('cp', str(files / 'small'), 'restricted/core-check/denied', failure=True)
                run('admin', 'info', 'restricted', failure=True)
                run('alias', 'set', 'invalid', s3, access, 'incorrect-secret', *options)
                run('ls', 'invalid', failure=True)
                run('admin', 'info', 'invalid', failure=True)
                if extended:
                    # Management round trips, including policy/group and service account state.
                    run('admin', 'user', 'info', 'test', 'restricted-user')
                    run('admin', 'user', 'list', 'test')
                    run('admin', 'user', 'disable', 'test', 'restricted-user')
                    run('ls', 'restricted/core-check', failure=True)
                    run('admin', 'user', 'enable', 'test', 'restricted-user')
                    run('ls', 'restricted/core-check')
                    group_secret = secrets.token_hex(24)
                    secrets_to_redact.append(group_secret)
                    run('admin', 'user', 'add', 'test', 'group-member', group_secret)
                    run('alias', 'set', 'groupmember', s3, 'group-member', group_secret, *options)
                    run('ls', 'groupmember/core-check', failure=True)
                    run('admin', 'group', 'add', 'test', 'compat-group', 'restricted-user', 'group-member')
                    run('admin', 'group', 'info', 'test', 'compat-group')
                    run('admin', 'group', 'list', 'test')
                    run('admin', 'policy', 'set', 'test', 'core-readonly', 'group=compat-group')
                    run('ls', 'groupmember/core-check')
                    run('cp', str(files / 'small'), 'groupmember/core-check/denied', failure=True)
                    run('admin', 'group', 'disable', 'test', 'compat-group')
                    run('ls', 'groupmember/core-check', failure=True)
                    run('admin', 'group', 'enable', 'test', 'compat-group')
                    run('ls', 'groupmember/core-check')
                    run('admin', 'policy', 'unset', 'test', 'core-readonly', 'group=compat-group')
                    run('ls', 'groupmember/core-check', failure=True)
                    run('admin', 'group', 'remove', 'test', 'compat-group', 'restricted-user', 'group-member')
                    run('admin', 'group', 'remove', 'test', 'compat-group')
                    run('admin', 'user', 'remove', 'test', 'group-member')
                    manager_secret = secrets.token_hex(24)
                    secrets_to_redact.append(manager_secret)
                    run('admin', 'user', 'add', 'test', 'service-manager', manager_secret)
                    manager_policy = root / 'service-manager.json'
                    manager_policy.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
                        {'Effect': 'Allow', 'Action': ['admin:*'], 'Resource': ['arn:aws:s3:::*']}]}))
                    run('admin', 'policy', 'add', 'test', 'service-manager', str(manager_policy))
                    run('admin', 'policy', 'set', 'test', 'service-manager', 'user=service-manager')
                    run('alias', 'set', 'manager', s3, 'service-manager', manager_secret, *options)
                    service_key = 'compat-service'
                    service_secret = secrets.token_hex(16)
                    secrets_to_redact.append(service_secret)
                    created = json.loads(run('--json', 'admin', 'user', 'svcacct', 'add',
                                             '--access-key', service_key, '--secret-key', service_secret,
                                             'manager', 'restricted-user'))
                    assert created['accessKey'] == service_key
                    checks += 1
                    run('admin', 'user', 'svcacct', 'info', 'manager', service_key)
                    run('admin', 'user', 'svcacct', 'ls', 'manager', 'restricted-user')
                    run('alias', 'set', 'service', s3, service_key, service_secret, *options)
                    run('ls', 'service/core-check')
                    run('cp', str(files / 'small'), 'service/core-check/denied', failure=True)
                    run('admin', 'user', 'svcacct', 'disable', 'manager', service_key)
                    run('ls', 'service/core-check', failure=True)
                    run('admin', 'user', 'svcacct', 'enable', 'manager', service_key)
                    run('ls', 'service/core-check')
                    rotated = secrets.token_hex(16)
                    secrets_to_redact.append(rotated)
                    run('admin', 'user', 'svcacct', 'set', '--secret-key', rotated, 'manager', service_key)
                    run('ls', 'service/core-check', failure=True)
                    run('alias', 'set', 'service', s3, service_key, rotated, *options)
                    run('ls', 'service/core-check')
                    run('admin', 'user', 'svcacct', 'rm', 'manager', service_key)
                    run('ls', 'service/core-check', failure=True)
                    run('admin', 'config', 'get', 'test', 'api')
                    exported = run('admin', 'config', 'export', 'test')
                    run('admin', 'config', 'set', 'test', 'api', 'requests_max=37')
                    assert b'requests_max=37' in run('admin', 'config', 'get', 'test', 'api')
                    checks += 1
                    run('admin', 'config', 'import', 'test', input_data=exported)
                    assert run('admin', 'config', 'export', 'test') == exported
                    checks += 1
                    run('admin', 'config', 'get', 'test', 'api')
                    run('admin', 'bucket', 'quota', 'test/core-check', '--hard', '1GiB')
                    quota = json.loads(run('--json', 'admin', 'bucket', 'quota', 'test/core-check'))
                    assert quota['status'] == 'success' and quota['quota'] == 1073741824 and quota['type'] == 'hard'
                    checks += 1
                    run('admin', 'bucket', 'quota', 'test/core-check', '--clear')
                    cleared = json.loads(run('--json', 'admin', 'bucket', 'quota', 'test/core-check'))
                    assert cleared.get('quota', 0) == 0 and cleared.get('type', '') == ''
                    checks += 1
                    # Error JSON must be one parseable object with a stable classification.
                    denied = json.loads(run('--json', 'admin', 'info', 'restricted', failure=True))
                    assert denied['status'] == 'error' and denied['error']['category'] == 'permission'
                    checks += 1
                    run('admin', 'top', 'locks', 'test', failure=True)  # single-node erasure is not distributed
                    run('admin', 'kms', 'key', 'status', 'test', failure=True)  # no KMS configured
                    run('admin', 'user', 'remove', 'test', 'service-manager')
                    run('admin', 'policy', 'remove', 'test', 'service-manager')
                    # Version, metadata, lifecycle and object-lock round trips.
                    run('mb', '--with-lock', 'test/advanced-check')
                    run('version', 'info', 'test/advanced-check')
                    target = 'test/advanced-check/object'
                    run('cp', str(files / 'small'), target)
                    run('tag', 'set', target, 'purpose=compat&language=zh')
                    tags = run('tag', 'list', target)
                    assert b'compat' in tags
                    checks += 1
                    run('tag', 'remove', target)
                    run('tag', 'set', 'test/advanced-check', 'purpose=compat')
                    assert b'compat' in run('tag', 'list', 'test/advanced-check')
                    checks += 1
                    run('tag', 'remove', 'test/advanced-check')
                    run('legalhold', 'set', target)
                    assert b'ON' in run('legalhold', 'info', target).upper()
                    checks += 1
                    run('legalhold', 'clear', target)
                    run('retention', 'set', 'governance', '1d', target)
                    assert b'GOVERNANCE' in run('retention', 'info', target).upper()
                    checks += 1
                    run('cp', str(files / 'empty'), target)
                    versions = [json.loads(line) for line in run('--json', 'ls', '--versions', target).splitlines()]
                    assert len(versions) >= 2
                    checks += 1
                    run('ilm', 'add', '--expiry-days', '30', 'test/advanced-check')
                    assert b'30' in run('ilm', 'ls', 'test/advanced-check')
                    checks += 1
                    run('ilm', 'rm', '--all', '--force', 'test/advanced-check')
                    run('rm', '--recursive', '--force', '--versions', '--bypass', 'test/advanced-check')
                    run('rb', 'test/advanced-check')
                    # Plain CSV Select verifies event-stream decoding and SQL semantics.
                    csv = root / 'data.csv'
                    csv.write_text('name,value\none,1\ntwo,2\n')
                    run('cp', str(csv), 'test/core-check/data.csv')
                    result = run('sql', '--query', 'select * from S3Object', 'test/core-check/data.csv')
                    assert b'one' in result and b'two' in result
                    checks += 1
                    # Customer-provided encryption over TLS, including the OC environment name.
                    customer_key = secrets.token_hex(16)
                    secrets_to_redact.append(customer_key)
                    encryption = {'OC_ENCRYPT_KEY': 'test/core-check/=' + customer_key}
                    run('cp', str(files / 'small'), 'test/core-check/encrypted', extra_env=encryption)
                    assert run('cat', 'test/core-check/encrypted', extra_env=encryption) == (files / 'small').read_bytes()
                    checks += 1
                    run('cat', 'test/core-check/encrypted', failure=True)
                    run('cat', 'test/core-check/encrypted', failure=True,
                        extra_env={'OC_ENCRYPT_KEY': 'test/core-check/=' + secrets.token_hex(16)})
                    # Real event streaming: start a listener, upload, and require a matching event.
                    watcher = subprocess.Popen([oc, '--config-dir', str(config), '--json', '--no-color',
                                                'watch', 'test/core-check'], env=env, stdout=subprocess.PIPE,
                                               stderr=subprocess.PIPE)
                    try:
                        time.sleep(1)
                        run('cp', str(files / 'small'), 'test/core-check/watch-event')
                        with selectors.DefaultSelector() as selector:
                            selector.register(watcher.stdout, selectors.EVENT_READ)
                            deadline = time.monotonic() + 15
                            observed = False
                            while time.monotonic() < deadline:
                                if not selector.select(timeout=1):
                                    continue
                                line = watcher.stdout.readline()
                                if not line:
                                    break
                                if b'watch-event' in line:
                                    observed = json.loads(line)['status'] == 'success'
                                    break
                            assert observed, 'object notification not delivered'
                            checks += 1
                    finally:
                        watcher.terminate()
                        try:
                            watcher.wait(timeout=5)
                        except subprocess.TimeoutExpired:
                            watcher.kill()
                            watcher.wait()
                        watcher.stdout.close()
                        watcher.stderr.close()
                    decoder = json.JSONDecoder()
                    def json_records(data):
                        records = []
                        while data.strip():
                            data = data.lstrip()
                            try:
                                value, end = decoder.raw_decode(data)
                            except json.JSONDecodeError:
                                break  # writer may still be completing the final record
                            records.append(value)
                            data = data[end:]
                        return records

                    for log_type in ['otterio', 'minio']:
                        stream_file = root / ('console-' + log_type + '.json')
                        with stream_file.open('wb') as output:
                            stream = subprocess.Popen([oc, '--config-dir', str(config), '--json', '--no-color',
                                                       'admin', 'console', '--type', log_type, '--limit', '10', 'test'],
                                                      env=env, stdout=output, stderr=subprocess.PIPE)
                            try:
                                end = time.monotonic() + 10
                                while time.monotonic() < end:
                                    emit_console_error()
                                    records = json_records(stream_file.read_text())
                                    if any(record.get('errKind') == 'OTTERIO' and record.get('api', {}).get('name') == 'AddUser' for record in records):
                                        break
                                    assert stream.poll() is None, 'console stream exited before receiving logs'
                                    time.sleep(0.1)
                                else:
                                    raise AssertionError('console stream received no OtterIO logs')
                                assert all(record.get('status') == 'success' for record in records)
                                checks += 1
                            finally:
                                stream.terminate()
                                try:
                                    stream.wait(timeout=5)
                                except subprocess.TimeoutExpired:
                                    stream.kill()
                                    stream.wait()
                                stream.stderr.close()

                    stream_file = root / 'trace.json'
                    with stream_file.open('wb') as output:
                        stream = subprocess.Popen([oc, '--config-dir', str(config), '--json', '--no-color',
                                                   'admin', 'trace', '--verbose', '--path', '*trace-event*', 'test'],
                                                  env=env, stdout=output, stderr=subprocess.PIPE)
                        try:
                            end = time.monotonic() + 15
                            while time.monotonic() < end:
                                run('cp', str(files / 'small'), 'test/core-check/trace-event')
                                records = json_records(stream_file.read_text())
                                if any('trace-event' in record.get('request', {}).get('path', '') for record in records):
                                    break
                                assert stream.poll() is None, 'trace stream exited before receiving requests'
                                time.sleep(0.2)
                            else:
                                raise AssertionError('trace did not include the generated object request')
                            checks += 1
                        finally:
                            stream.terminate()
                            try:
                                stream.wait(timeout=5)
                            except subprocess.TimeoutExpired:
                                stream.kill()
                                stream.wait()
                            stream.stderr.close()

                    run('admin', 'profile', 'start', '--type', 'goroutines', 'test')
                    run('admin', 'profile', 'stop', 'test', working_dir=root)
                    with zipfile.ZipFile(root / 'profile.zip') as archive:
                        assert archive.testzip() is None and archive.namelist()
                        assert any(b'goroutine' in archive.read(item) for item in archive.namelist())
                    checks += 1
                    run('admin', 'heal', 'test')
                    run('admin', 'subnet', 'health', '--test', 'otterioinfo', '--deadline', '10s', 'test', working_dir=root)
                    reports = list(root.glob('test-health_*.json.gz'))
                    assert len(reports) == 1
                    with gzip.open(reports[0], 'rt') as report:
                        records = [json.loads(line) for line in report if line.strip()]
                    assert len(records) == 2 and records[1]['status'] == 'Success'
                    assert records[1]['software']['minio']['info']['servers']
                    checks += 1
                    # Environment override works without losing separate admin settings.
                    run('admin', 'info', 'test', extra_env={'OC_HOST_test': f'{scheme}://{access}:{secret}@127.0.0.1:{ports[0]}',
                                                           'MC_HOST_test': 'http://bad:bad@127.0.0.1:1'})
                if stability:
                    stability_result = stability_checks(oc, config, env, root, run, files, soak_seconds, record.setdefault("stability", {}), emit_console_error)
                    checks += stability_result['checks']
                run('admin', 'user', 'remove', 'test', 'restricted-user')
                run('admin', 'policy', 'remove', 'test', 'core-readonly')
                run('rm', '--recursive', '--force', 'test/core-check')
                run('rb', 'test/core-check')
                if extended:
                    run('mb', 'test/restart-check')
                    run('cp', str(files / 'small'), 'test/restart-check/persisted')
                    saved_config = run('admin', 'config', 'export', 'test')
                    run('admin', 'service', 'restart', '--timeout', '90s', 'test', command_timeout=100)
                    deadline = time.monotonic() + 30
                    while True:
                        try:
                            with urllib.request.urlopen(s3 + '/otterio/health/ready', context=context, timeout=1) as response:
                                if response.status == 200:
                                    break
                        except (OSError, urllib.error.HTTPError):
                            pass
                        if time.monotonic() >= deadline:
                            raise RuntimeError('restarted server did not become ready')
                        time.sleep(0.2)
                    run('admin', 'info', 'test')
                    assert run('cat', 'test/restart-check/persisted') == (files / 'small').read_bytes()
                    assert run('admin', 'config', 'export', 'test') == saved_config
                    checks += 2
                    run('rm', '--recursive', '--force', 'test/restart-check')
                    run('rb', 'test/restart-check')
                    run('admin', 'service', 'stop', 'test')
                    server.wait(timeout=10)
                    checks += 1
            except Exception as error:
                diagnostic = log_path.read_text(errors='replace')
                message = str(error)
                for credential in secrets_to_redact:
                    diagnostic = diagnostic.replace(credential, 'REDACTED')
                    message = message.replace(credential, 'REDACTED')
                (artifacts / (name + '-server.log')).write_text(diagnostic)
                record.update(errorMessage=message, diagnosticLog=name + '-server.log')
                raise
            finally:
                server.terminate()
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()
    print(f'{name}: {checks} checks passed', flush=True)
    result = record
    result.update(scenario=name, checks=checks, status='passed')
    if stability_result is not None:
        result['stability'] = stability_result
    return result


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--oc', required=True)
    parser.add_argument('--otterio', required=True)
    parser.add_argument('--report')
    parser.add_argument('--artifacts-dir')
    parser.add_argument('--extended', action='store_true', help='also run management and advanced S3 checks on four-drive erasure storage')
    parser.add_argument('--extended-only', action='store_true')
    parser.add_argument('--stability', action='store_true', help='run sampled memory/concurrency and slow-consumer cancellation checks')
    parser.add_argument('--stability-only', action='store_true', help='run core plus stability on a dual-port TLS server')
    parser.add_argument('--soak-seconds', type=int, default=0, help='watch slow-consumer memory sampling duration (requires stability)')
    args = parser.parse_args()
    if args.soak_seconds < 0 or args.soak_seconds > 3600 or args.soak_seconds and not (args.stability or args.stability_only):
        parser.error('--soak-seconds must be 0..3600 and requires --stability or --stability-only')
    oc = str(Path(args.oc).resolve())
    otterio = str(Path(args.otterio).resolve())
    results = []
    artifacts = Path(args.artifacts_dir or ((args.report + '.artifacts') if args.report else tempfile.mkdtemp(prefix='oc-evidence-')))
    artifacts.mkdir(parents=True, exist_ok=True)
    def execute(*scenario_args, **kwargs):
        record = {}
        results.append(record)
        try:
            scenario(*scenario_args, record=record, artifacts=artifacts, **kwargs)
        except Exception as error:
            record.update(status='failed', errorType=type(error).__name__)
            (artifacts / 'failure-summary.json').write_text(json.dumps({'scenario': record.get('scenario'), 'lastCommand': record.get('lastCommand'), 'errorType': type(error).__name__, 'errorMessage': record.get('errorMessage'), 'diagnosticLog':record.get('diagnosticLog')}, indent=2)+'\n')
            raise
    try:
        results.append(migration_checks(oc))
        if not args.extended_only and not args.stability_only:
            for split, tls, public in [(False, False, False), (True, False, False),
                                       (False, True, False), (True, True, False), (True, False, True)]:
                execute(oc, otterio, split, tls, public)
        if args.stability or args.stability_only:
            execute(oc, otterio, True, True, False, stability=True, soak_seconds=args.soak_seconds)
        if args.extended or args.extended_only:
            execute(oc, otterio, True, True, False, extended=True)
    except Exception as error:
        results.append({'scenario': 'incomplete', 'status': 'failed', 'errorType': type(error).__name__})
        raise
    finally:
        if args.report:
            context_info = {'platform': platform.system(), 'architecture': platform.machine(),
                            'ocSHA256': digest(Path(oc)), 'otterioSHA256': digest(Path(otterio)),
                            'asyncPreemptionDisabled': 'asyncpreemptoff=1' in os.environ.get('GODEBUG', '').split(',')}
            Path(args.report).write_text(json.dumps({'testContext': context_info, 'results': results}, indent=2) + '\n')



if __name__ == '__main__':
    main()
