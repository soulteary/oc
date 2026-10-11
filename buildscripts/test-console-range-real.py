#!/usr/bin/env python3
"""Disposable formal-server Range matrix; run from the repository root."""
import argparse
import sys, importlib.util, tempfile, pathlib, subprocess, os, time, json, ssl, urllib.parse
sys.path.insert(0, str(pathlib.Path.cwd() / 'buildscripts'))
spec = importlib.util.spec_from_file_location('harness', 'buildscripts/test-console-integration.py')
h = importlib.util.module_from_spec(spec)
spec.loader.exec_module(h)
parser = argparse.ArgumentParser()
parser.add_argument('--cli', required=True)
parser.add_argument('--console', required=True)
parser.add_argument('--server', required=True)
parser.add_argument('--server-source', required=True)
parser.add_argument('--output', required=True)
options = parser.parse_args()
results = []
for profile in ['single-http', 'dual-http', 'dual-tls']:
    with tempfile.TemporaryDirectory(prefix='oc-range-real-') as temp:
        root = pathlib.Path(temp)
        tls = profile == 'dual-tls'
        split = profile != 'single-http'
        ports = [h.free_port(), h.free_port()]
        scheme = 'https' if tls else 'http'
        endpoint = f'{scheme}://127.0.0.1:{ports[0]}'
        admin = f'{scheme}://127.0.0.1:{ports[1]}' if split else endpoint
        sc = root / 's3-certs'
        ac = root / 'admin-certs'
        s3ca = h.certificate(sc) if tls else None
        adminca = h.certificate(ac) if tls else None
        access = 'range-fixture-root'
        secret = 'range-fixture-secret-long-2026'
        env = dict(os.environ, OTTERIO_ROOT_USER=access, OTTERIO_ROOT_PASSWORD=secret, OTTERIO_BROWSER='off', NO_PROXY='127.0.0.1,localhost')
        ctx = ssl.create_default_context(cafile=str(s3ca)) if tls else None
        command = [options.server, 'server', '--address', f'127.0.0.1:{ports[0]}', '--certs-dir', str(sc)]
        if split:
            command += ['--console-address', f'127.0.0.1:{ports[1]}']
        if tls:
            command += ['--console-certs-dir', str(ac)]
        command += [str(root / f'data-{i}') for i in range(4)]
        server = console = None
        with (root / 'server.log').open('wb') as log, (root / 'console.log').open('wb') as clog:
            try:
                server = subprocess.Popen(command, env=env, stdout=log, stderr=log)
                for _ in range(300):
                    try:
                        with h.local_urlopen(endpoint + '/otterio/health/ready', timeout=1, context=ctx):
                            break
                    except Exception:
                        time.sleep(0.1)
                else:
                    raise RuntimeError('readiness')
                cfg = root / 'config'
                cfg.mkdir()
                (cfg / 'config.json').write_text(json.dumps({'version': '10', 'aliases': {'rangefixture': {'url': endpoint, 'adminURL': admin, 'adminCAFile': str(adminca) if tls else '', 'accessKey': access, 'secretKey': secret, 'api': 'S3v4', 'path': 'on'}}}))
                (cfg / 'certs' / 'CAs').mkdir(parents=True)
                if tls:
                    (cfg / 'certs' / 'CAs' / 's3.crt').write_bytes(s3ca.read_bytes())
                cli = lambda *args: h.run_fixture_cli([options.cli, '--config-dir', str(cfg), '--no-color', *args], env, [access, secret, 'range-denied-user-secret-2026'])
                cli('mb', 'rangefixture/range-fixture')
                data = b'0123456789abcdef' * 65536
                fixture = root / 'payload'
                fixture.write_bytes(data)
                key = '中文 range+%.bin'
                h.put_object(endpoint, 'range-fixture', key, data, access, secret, ctx)
                console = subprocess.Popen([options.console, '--config-dir', str(cfg), '--alias', 'rangefixture', '--address', '127.0.0.1:0'], env=env, stdout=clog, stderr=clog)
                for _ in range(200):
                    fields = dict((line.split(': ', 1) for line in (root / 'console.log').read_text().splitlines() if ': ' in line))
                    if 'URL' in fields and 'Login code' in fields:
                        break
                    time.sleep(0.05)
                else:
                    raise RuntimeError('console startup')
                command = [sys.executable, 'buildscripts/test-console-range.py', '--console-url', fields['URL'], '--login-code', fields['Login code'], '--object-path', '/api/download?' + urllib.parse.urlencode({'bucket': 'range-fixture', 'key': key}), '--fixture', str(fixture)]
                out = subprocess.run(command, env=env, capture_output=True, text=True)
                assert out.returncode == 0, out.stderr
                import http.cookiejar, urllib.request, urllib.error
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

                def probe(query, method='GET', headers=None):
                    req = urllib.request.Request(fields['URL'] + '/api/download?' + urllib.parse.urlencode(query), method=method, headers=headers or {})
                    try:
                        r = opener.open(req, timeout=10)
                    except urllib.error.HTTPError as e:
                        r = e
                    with r:
                        return (r.status, r.headers, r.read())
                req = urllib.request.Request(fields['URL'] + '/api/login', data=json.dumps({'code': fields['Login code']}).encode(), headers={'Origin': fields['URL'], 'Content-Type': 'application/json'})
                with opener.open(req, timeout=10):
                    pass
                h.put_object(endpoint, 'range-fixture', 'empty', b'', access, secret, ctx)
                status, headers, body = probe({'bucket': 'range-fixture', 'key': 'empty'}, headers={'Range': 'bytes=0-0'})
                assert status == 416 and headers['Content-Range'] == 'bytes */0' and (not body)
                status, headers, body = probe({'bucket': 'range-fixture', 'key': 'empty'}, method='HEAD')
                assert status == 200 and headers['Content-Length'] == '0' and (not body)
                status, headers, body = probe({'bucket': 'range-fixture', 'key': key, 'versionId': 'null'}, headers={'Range': 'bytes=0-1'})
                assert status in (206, 501), (status, body)
                if status == 206:
                    assert body == data[:2]
                null_status = status
                cli('version', 'enable', 'rangefixture/range-fixture')
                h.put_object(endpoint, 'range-fixture', 'history', b'OLD-BYTES', access, secret, ctx)
                old_version = json.loads(cli('--json', 'stat', 'rangefixture/range-fixture/history'))['versionID']
                h.put_object(endpoint, 'range-fixture', 'history', b'NEW-BYTES', access, secret, ctx)
                status, headers, body = probe({'bucket': 'range-fixture', 'key': 'history', 'versionId': old_version}, headers={'Range': 'bytes=0-2'})
                assert status == 206 and body == b'OLD', (status, body)
                denied_access = 'range-denied-user'
                denied_secret = 'range-denied-user-secret-2026'
                cli('admin', 'user', 'add', 'rangefixture', denied_access, denied_secret)
                policy = root / 'denied-policy.json'
                policy.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [{'Effect': 'Allow', 'Action': ['s3:GetObject', 's3:GetBucketLocation'], 'Resource': ['arn:aws:s3:::range-fixture', 'arn:aws:s3:::range-fixture/*']}]}))
                cli('admin', 'policy', 'add', 'rangefixture', 'range-current-only', str(policy))
                cli('admin', 'policy', 'set', 'rangefixture', 'range-current-only', 'user=' + denied_access)
                config = json.loads((cfg / 'config.json').read_text())
                config['aliases']['currentonly'] = {**config['aliases']['rangefixture'], 'accessKey': denied_access, 'secretKey': denied_secret}
                (cfg / 'config.json').write_text(json.dumps(config))
                h.stop(console)
                console = subprocess.Popen([options.console, '--config-dir', str(cfg), '--alias', 'currentonly', '--address', '127.0.0.1:0'], env=env, stdout=clog, stderr=clog)
                previous = fields['URL']
                for _ in range(200):
                    fields = dict((line.split(': ', 1) for line in (root / 'console.log').read_text().splitlines() if ': ' in line))
                    if fields.get('URL') != previous and 'Login code' in fields:
                        break
                    time.sleep(0.05)
                else:
                    raise RuntimeError('restricted console startup')
                opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
                req = urllib.request.Request(fields['URL'] + '/api/login', data=json.dumps({'code': fields['Login code']}).encode(), headers={'Origin': fields['URL'], 'Content-Type': 'application/json'})
                with opener.open(req, timeout=10):
                    pass
                status, headers, body = probe({'bucket': 'range-fixture', 'key': 'history'}, headers={'Range': 'bytes=0-2'})
                assert status == 206 and body == b'NEW', (status, body)
                status, headers, body = probe({'bucket': 'range-fixture', 'key': 'history', 'versionId': old_version}, headers={'Range': 'bytes=0-2'})
                assert status == 403, (status, body)
                results.append({'profile': profile, **json.loads(out.stdout), 'emptyObjectChecks': 2, 'nullVersionStatus': null_status, 'historicalVersionStatus': 206, 'restrictedCurrentStatus': 206, 'restrictedHistoricalStatus': 403})
            finally:
                h.stop(console)
                h.stop(server)
pathlib.Path(options.output).write_text(json.dumps({'serverSource': options.server_source, 'profiles': results}, indent=2))
print(json.dumps(results))
