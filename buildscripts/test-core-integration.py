#!/usr/bin/env python3
"""Run OC against disposable real OtterIO servers; never use a user's aliases."""
import argparse
import hashlib
import json
import os
from pathlib import Path
import secrets
import shutil
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
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


def scenario(oc, otterio, split, tls, public):
    name = ('dual' if split else 'single') + ('-tls' if tls else '-http') + ('-public' if public else '')
    checks = 0
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
        env = {k: v for k, v in os.environ.items()
               if not k.startswith(('MC_HOST_', 'MC_HOSTS_', 'OC_ADMIN_', 'OTTERIO_'))}
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

                def run(*args, failure=False, extra_env=None, output=None, working_dir=None):
                    nonlocal checks
                    child_env = dict(env)
                    child_env.update(extra_env or {})
                    completed = subprocess.run([oc, '--config-dir', str(config), '--no-color', *args],
                                               env=child_env, stdout=output or subprocess.PIPE,
                                               stderr=subprocess.PIPE, timeout=45, cwd=working_dir)
                    checks += 1
                    if (completed.returncode != 0) != failure:
                        message = completed.stderr.decode(errors='replace')
                        if completed.stdout and isinstance(completed.stdout, bytes):
                            message += completed.stdout.decode(errors='replace')
                        for credential in (access, secret, restricted_secret):
                            message = message.replace(credential, 'REDACTED')
                        raise RuntimeError(f'{name}: {args[:2]} returned {completed.returncode}: {message[:2000]}')
                    return completed.stdout

                options = ['--api', 's3v4', '--path', 'on']
                if split:
                    options += ['--admin-url', admin]
                if tls:
                    options += ['--admin-ca', os.path.relpath(adminca)]
                run('alias', 'set', 'test', s3, access, secret, *options)
                run('admin', 'info', 'test', working_dir=root)
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
                run('admin', 'user', 'remove', 'test', 'restricted-user')
                run('admin', 'policy', 'remove', 'test', 'core-readonly')
                run('rm', '--recursive', '--force', 'test/core-check')
                run('rb', 'test/core-check')
            finally:
                server.terminate()
                try:
                    server.wait(timeout=10)
                except subprocess.TimeoutExpired:
                    server.kill()
                    server.wait()
    print(f'{name}: {checks} checks passed', flush=True)
    return {'scenario': name, 'checks': checks, 'status': 'passed'}


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--oc', required=True)
    parser.add_argument('--otterio', required=True)
    parser.add_argument('--report')
    args = parser.parse_args()
    results = [scenario(str(Path(args.oc).resolve()), str(Path(args.otterio).resolve()), split, tls, public)
               for split, tls, public in [(False, False, False), (True, False, False),
                                          (False, True, False), (True, True, False), (True, False, True)]]
    if args.report:
        Path(args.report).write_text(json.dumps({'results': results}, indent=2) + '\n')


if __name__ == '__main__':
    main()
