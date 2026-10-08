#!/usr/bin/env python3
"""Exercise durable tiering with independent, disposable OtterIO processes.

Uses only synthetic credentials, isolated configuration and temporary disks.
Actual scanners perform transitions; there are no production test endpoints.
"""
import argparse
import base64
import datetime
import hashlib
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import secrets
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

spec = importlib.util.spec_from_file_location('console_fixture', Path(__file__).with_name('test-console-integration.py'))
fixture = importlib.util.module_from_spec(spec)
spec.loader.exec_module(fixture)


def scenario(args, name, record):
    tls, split = name.endswith('tls'), name.startswith('dual')
    record.update(scenario=name, checks=[], status='running')
    processes, handles = {}, []
    with tempfile.TemporaryDirectory(prefix='oc-tier-acceptance-') as directory:
        root = Path(directory)
        access, secret = 'tier-' + secrets.token_hex(6), secrets.token_hex(24)
        credentials = [access, secret]
        secret_values = [secret]
        env = {key: value for key, value in os.environ.items() if not key.startswith(('OC_', 'MC_', 'OTTERIO_'))}
        env.update(OTTERIO_ROOT_USER=access, OTTERIO_ROOT_PASSWORD=secret, OTTERIO_BROWSER='off',
                   OTTERIO_SCANNER_CYCLE='1s', OTTERIO_SCANNER_DELAY='0', OTTERIO_SCANNER_MAX_WAIT='1ms',
                   NO_PROXY='*', no_proxy='*')
        scheme = 'https' if tls else 'http'
        ports = []
        while len(ports) < (4 if split else 2):
            port = fixture.free_port()
            if port not in ports:
                ports.append(port)
        source, tier = f'{scheme}://127.0.0.1:{ports[0]}', f'{scheme}://127.0.0.1:{ports[1]}'
        source_admin = f'{scheme}://127.0.0.1:{ports[2]}' if split else source
        tier_admin = f'{scheme}://127.0.0.1:{ports[3]}' if split else tier
        certs = {}
        for instance in ('source', 'tier'):
            cert_dir = root / (instance + '-certs')
            certs[instance] = fixture.certificate(cert_dir) if tls else None
            if not tls:
                cert_dir.mkdir()
        if tls:
            for instance in ('source', 'tier'):
                ca_dir = root / (instance + '-certs') / 'CAs'
                ca_dir.mkdir()
                for peer, cert in certs.items():
                    (ca_dir / (peer + '.crt')).write_bytes(cert.read_bytes())
        contexts = {name: ssl.create_default_context(cafile=str(cert)) if tls else None for name, cert in certs.items()}
        config_dir = root / 'config'
        config_dir.mkdir()
        if tls:
            ca_dir = config_dir / 'certs' / 'CAs'
            ca_dir.mkdir(parents=True)
            for peer, cert in certs.items():
                (ca_dir / (peer + '.crt')).write_bytes(cert.read_bytes())
        config = config_dir / 'config.json'
        config.write_text(json.dumps({'version': '10', 'aliases': {
            alias: {'url': endpoint, 'adminURL': admin, 'adminCAFile': str(certs[instance]) if tls else '',
                    'accessKey': access, 'secretKey': secret, 'api': 'S3v4', 'path': 'on'}
            for alias, endpoint, admin, instance in [('store', source, source_admin, 'source'), ('tier', tier, tier_admin, 'tier')]
        }}))
        config.chmod(0o600)
        original_config = config.read_bytes()

        def cli(*options, expected=0, error_code=None):
            try:
                result = subprocess.run([args.cli, '--config-dir', str(config_dir), '--no-color', *options],
                                        env=env, capture_output=True, timeout=30)
            except subprocess.TimeoutExpired:
                raise RuntimeError('fixture CLI exceeded its deadline') from None
            detail = (result.stdout + result.stderr).decode(errors='replace')
            if any(value in detail for value in secret_values):
                raise AssertionError('fixture CLI exposed a target secret in its output')
            for value in credentials:
                detail = detail.replace(value, '[redacted]')
            if (result.returncode == 0) != (expected == 0):
                raise AssertionError('fixture CLI result: ' + detail[:1000])
            if error_code is not None and error_code not in detail:
                raise AssertionError('fixture CLI failure was not the expected ' + error_code + ': ' + detail[:1000])
            return result.stdout

        def start(instance):
            endpoint = source if instance == 'source' else tier
            port = ports[0] if instance == 'source' else ports[1]
            log = (root / (instance + '.log')).open('ab')
            handles.append(log)
            command = [args.server, 'server', '--address', f'127.0.0.1:{port}', '--certs-dir', str(root / (instance + '-certs'))]
            if split:
                command += ['--console-address', f'127.0.0.1:{ports[2] if instance == "source" else ports[3]}',
                            '--console-certs-dir', str(root / (instance + '-certs'))]
            command += [str(root / (instance + f'-disk-{index}')) for index in range(4)]
            processes[instance] = subprocess.Popen(command, env=env, stdout=log, stderr=log)
            deadline = time.monotonic() + 30
            while time.monotonic() < deadline:
                if processes[instance].poll() is not None:
                    raise RuntimeError(instance + ' exited during startup')
                try:
                    with fixture.local_urlopen(endpoint + '/otterio/health/ready', timeout=1, context=contexts[instance]) as response:
                        if response.status == 200:
                            return
                except (OSError, urllib.error.HTTPError):
                    pass
                time.sleep(0.1)
            raise RuntimeError(instance + ' readiness timeout')

        def request(method, key='', body=b'', query=None, headers=None, instance='source', bucket='source-fixture'):
            uri = '/' + urllib.parse.quote(bucket + ('/' + key if key else ''), safe='/-_.~')
            return fixture.signed_request(source if instance == 'source' else tier, method, uri, body,
                                          access, secret, contexts[instance],
                                          query=urllib.parse.urlencode(sorted((query or {}).items())), extra_headers=headers)

        def check(method, key='', body=b'', query=None, headers=None, status=200, **kwargs):
            code, data, result_headers = request(method, key, body, query, headers, **kwargs)
            # Restart readiness is asynchronous for IAM/bucket metadata. Only
            # repeat reads that explicitly report uninitialized startup state.
            deadline = time.monotonic() + 20
            while method in ('GET', 'HEAD') and code == 503 and (method == 'HEAD' or b'XOtterioServerNotInitialized' in data) and time.monotonic() < deadline:
                time.sleep(0.1)
                code, data, result_headers = request(method, key, body, query, headers, **kwargs)
            if code != status:
                raise AssertionError(f'{method} {key}: expected {status}, got {code}: {data[:300]!r}')
            return data, result_headers

        def wait_head(key, version, predicate, timeout=90):
            deadline = time.monotonic() + timeout
            latest = None
            while time.monotonic() < deadline:
                latest = check('HEAD', key, query={'versionId': version})[1]
                if predicate(latest):
                    return latest
                time.sleep(0.2)
            raise AssertionError('scanner did not reach expected state for ' + key + ': ' + str(dict(latest or {})))

        def put(key, payload, when):
            _, headers = check('PUT', key, payload, headers={'x-otterio-source-mtime': when,
                'X-Amz-Meta-Owner': 'owned tier fixture', 'X-Amz-Tagging': 'environment=archive'})
            return headers['X-Amz-Version-Id']

        def list_tier(bucket):
            data, _ = check('GET', query={'versions': ''}, instance='tier', bucket=bucket)
            return [item.findtext('{*}Key') for item in ET.fromstring(data).findall('{*}Version')]

        try:
            start('tier')
            start('source')
            cli('mb', 'store/source-fixture')
            cli('version', 'enable', 'store/source-fixture')
            arns = {}
            for label, bucket in [('CURRENT', 'current-tier'), ('NONCURRENT', 'noncurrent-tier')]:
                cli('mb', 'tier/' + bucket)
                cli('version', 'enable', 'tier/' + bucket)
                target_url = f'{scheme}://{access}:{secret}@127.0.0.1:{ports[1]}/{bucket}'
                reply = cli('--json', 'admin', 'bucket', 'remote', 'add', 'store/source-fixture', target_url,
                            '--service', 'ilm', '--label', label, '--path', 'on')
                arns[label] = json.loads(reply)['RemoteARN']
            assert arns['CURRENT'] != arns['NONCURRENT']
            unauthenticated = urllib.request.Request(source_admin + '/otterio/admin/v3/list-remote-targets?bucket=source-fixture&type=ilm')
            try:
                unauthenticated_response = fixture.local_urlopen(unauthenticated, context=contexts['source'], timeout=10)
            except urllib.error.HTTPError as error:
                unauthenticated_response = error
            with unauthenticated_response:
                assert unauthenticated_response.status >= 400
                assert unauthenticated_response.headers.get('X-Otterio-Lifecycle-Transition') is None
            record['checks'].append('OC registers two independent ILM labels and distinct ARNs on the remote service')
            old = (datetime.datetime.now(datetime.timezone.utc) - datetime.timedelta(days=12)).replace(microsecond=0)
            old_stamp = old.isoformat().replace('+00:00', 'Z')
            newer_stamp = (old + datetime.timedelta(days=1)).isoformat().replace('+00:00', 'Z')
            current_payload, old_payload, new_payload = b'current tier content with range support', b'old noncurrent tier content', b'new latest stays local'
            current_version = put('converted/item.txt', current_payload, old_stamp)
            old_version = put('versions/item.txt', old_payload, old_stamp)
            latest_version = put('versions/item.txt', new_payload, newer_stamp)
            empty_version = put('versions/empty.bin', b'', old_stamp)
            put('versions/empty.bin', b'latest empty successor', newer_stamp)
            mismatch_version = put('outside/item.txt', b'filter must not convert', old_stamp)

            # Save the mixed current/noncurrent XML through the actual OC
            # browser API and verify the exact canonical document/revision.
            log_path = root / 'console.log'
            console_log = log_path.open('wb')
            handles.append(console_log)
            processes['console'] = subprocess.Popen([args.console, '--config-dir', str(config_dir), '--alias', 'store',
                '--address', '127.0.0.1:0', '--allow-writes'], env=env, stdout=console_log, stderr=console_log)
            fields = {}
            deadline = time.monotonic() + 10
            while time.monotonic() < deadline:
                fields = dict(line.split(': ', 1) for line in log_path.read_text().splitlines() if ': ' in line)
                if 'URL' in fields and 'Login code' in fields:
                    break
                time.sleep(0.05)
            base = fields['URL']
            opener = urllib.request.build_opener(urllib.request.ProxyHandler({}), urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))

            def console_api(path, method='GET', body=None, token=''):
                headers = {'Origin': base, 'Content-Type': 'application/json'}
                if token:
                    headers['X-CSRF-Token'] = token
                req = urllib.request.Request(base + path, data=json.dumps(body).encode() if body is not None else None,
                                             method=method, headers=headers)
                try:
                    response = opener.open(req, timeout=30)
                except urllib.error.HTTPError as error:
                    response = error
                with response:
                    data = response.read()
                    if response.status != 200:
                        raise AssertionError('OC settings HTTP ' + str(response.status) + ': ' + repr(data[:300]))
                    return json.loads(data)

            login = console_api('/api/login', 'POST', {'code': fields['Login code']})
            settings_path = '/api/bucket-settings?' + urllib.parse.urlencode({'bucket': 'source-fixture', 'kind': 'lifecycle'})
            previous = console_api(settings_path)
            doc = ('<LifecycleConfiguration><Rule><ID>future-first</ID><Status>Enabled</Status><Filter><Prefix>converted/</Prefix></Filter>'
                   '<Transition><Days>36500</Days><StorageClass>NONCURRENT</StorageClass></Transition></Rule>'
                   '<Rule><ID>current-due</ID><Status>Enabled</Status><Filter><Prefix>converted/</Prefix></Filter>'
                   '<Transition><Days>1</Days><StorageClass>CURRENT</StorageClass></Transition>'
                   '<NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>NONCURRENT</StorageClass></NoncurrentVersionTransition></Rule>'
                   '<Rule><ID>noncurrent-due</ID><Status>Enabled</Status><Filter><Prefix>versions/</Prefix></Filter>'
                   '<NoncurrentVersionTransition><NoncurrentDays>1</NoncurrentDays><StorageClass>NONCURRENT</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>')
            saved = console_api('/api/bucket-settings', 'POST', {'bucket': 'source-fixture', 'kind': 'lifecycle',
                'document': doc, 'revision': previous['revision'], 'confirm': True}, login['csrfToken'])
            canonical = console_api(settings_path)
            assert saved['revision'] == canonical['revision'] and canonical['revision'] != previous['revision']
            assert 'NoncurrentVersionTransition' in canonical['document'] and 'CURRENT' in canonical['document']
            record['checks'].append('OC protects and saves complete mixed lifecycle XML, then reads the new canonical document and revision')
            wait_head('converted/item.txt', current_version, lambda h: h.get('X-Amz-Storage-Class') == 'CURRENT')
            wait_head('versions/item.txt', old_version, lambda h: h.get('X-Amz-Storage-Class') == 'NONCURRENT')
            wait_head('versions/empty.bin', empty_version, lambda h: h.get('X-Amz-Storage-Class') == 'NONCURRENT')
            assert check('GET', 'versions/item.txt', query={'versionId': latest_version})[0] == new_payload
            assert check('HEAD', 'outside/item.txt', query={'versionId': mismatch_version})[1].get('X-Amz-Storage-Class') not in ('CURRENT', 'NONCURRENT')
            assert len(list_tier('current-tier')) == 1 and len(list_tier('noncurrent-tier')) == 2
            record['checks'].append('real scanners select the due rule, transition noncurrent versions and empty objects, and preserve latest/filter-mismatched data')
            # Rotate credentials while both destinations have durable source
            # references. The ARN, protocol and physical destination must stay.
            tier_user, tier_secret = 'tier-reader-' + secrets.token_hex(6), secrets.token_hex(24)
            credentials.extend([tier_user, tier_secret])
            secret_values.append(tier_secret)
            cli('admin', 'user', 'add', 'tier', tier_user, tier_secret)
            cli('admin', 'policy', 'set', 'tier', 'readwrite', 'user=' + tier_user)
            for label, bucket in [('CURRENT', 'current-tier'), ('NONCURRENT', 'noncurrent-tier')]:
                target_url = f'{scheme}://{tier_user}:{tier_secret}@127.0.0.1:{ports[1]}/{bucket}'
                reply = cli('--json', 'admin', 'bucket', 'remote', 'edit', 'store/source-fixture', target_url,
                            '--arn', arns[label])
                assert json.loads(reply)['RemoteARN'] == arns[label]
                cli('admin', 'bucket', 'remote', 'edit', 'store/source-fixture', '--arn', arns[label], '--healthcheck-seconds', '30')
            targets = [json.loads(line) for line in cli('--json', 'admin', 'bucket', 'remote', 'ls', 'store/source-fixture', '--service', 'ilm').splitlines()]
            assert {item['accessKey'] for item in targets} == {tier_user}
            assert {item['label'] for item in targets} == {'CURRENT', 'NONCURRENT'}
            wrong_secret = secrets.token_hex(24)
            secret_values.append(wrong_secret)
            credentials.append(wrong_secret)
            wrong_url = f'{scheme}://{tier_user}:{wrong_secret}@127.0.0.1:{ports[1]}/current-tier'
            cli('--json', '--debug', 'admin', 'bucket', 'remote', 'edit', 'store/source-fixture', wrong_url,
                '--arn', arns['CURRENT'], expected=1)
            for key, version, payload, storage_class in [
                    ('converted/item.txt', current_version, current_payload, 'CURRENT'),
                    ('versions/item.txt', old_version, old_payload, 'NONCURRENT')]:
                data, headers = check('GET', key, query={'versionId': version})
                assert data == payload and headers.get('X-Amz-Storage-Class') == storage_class
            record['checks'].append('authenticated lifecycle capability gates target writes; CLI credential and metadata edits retain exact destinations and referenced objects')
            successor = put('converted/item.txt', b'current successor stays on source', datetime.datetime.now(datetime.timezone.utc).replace(microsecond=0).isoformat().replace('+00:00', 'Z'))
            check('DELETE', query={'lifecycle': ''}, status=204)
            check('GET', query={'lifecycle': ''}, status=404)
            fixture.stop(processes['source'])
            start('source')
            for key, version, payload, storage_class in [
                    ('converted/item.txt', current_version, current_payload, 'CURRENT'),
                    ('versions/item.txt', old_version, old_payload, 'NONCURRENT'),
                    ('versions/empty.bin', empty_version, b'', 'NONCURRENT')]:
                data, headers = check('GET', key, query={'versionId': version})
                assert data == payload and headers.get('X-Amz-Storage-Class') == storage_class
            data, headers = check('GET', 'converted/item.txt', query={'versionId': current_version}, headers={'Range': 'bytes=2-8'}, status=206)
            assert data == current_payload[2:9] and headers['X-Amz-Meta-Owner'] == 'owned tier fixture' and headers.get('X-Amz-Storage-Class') == 'CURRENT'
            assert check('GET', 'converted/item.txt', query={'versionId': successor})[0] == b'current successor stays on source'
            record['checks'].append('source restart, rule deletion and current-to-noncurrent changes retain exact remote versions, metadata and range reads')
            for arn in arns.values():
                cli('--json', 'admin', 'bucket', 'remote', 'rm', 'store/source-fixture', '--arn', arn, expected=1,
                    error_code='XOtterioAdminRemoteRemoveDisallowed')
            record['checks'].append('durable target references prevent removing both destinations after lifecycle deletion and restart')
            restore = b'<RestoreRequest xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Days>1</Days></RestoreRequest>'
            check('POST', 'versions/item.txt', restore, query={'restore': '', 'versionId': old_version}, status=202)
            wait_head('versions/item.txt', old_version, lambda h: str(h.get('X-Amz-Restore', '')).startswith('ongoing-request=false'))
            assert check('HEAD', 'versions/item.txt', query={'versionId': latest_version})[1].get('X-Amz-Restore') is None
            fixture.stop(processes['tier'])
            data, headers = check('GET', 'versions/item.txt', query={'versionId': old_version})
            assert data == old_payload and headers.get('X-Amz-Storage-Class') == 'NONCURRENT'
            code, _, _ = request('DELETE', 'converted/item.txt', query={'versionId': current_version})
            assert code >= 400
            assert check('HEAD', 'converted/item.txt', query={'versionId': current_version})[1].get('X-Amz-Storage-Class') == 'CURRENT'
            fixture.stop(processes['source'])
            start('source')
            assert check('HEAD', 'converted/item.txt', query={'versionId': current_version})[1].get('X-Amz-Storage-Class') == 'CURRENT'
            start('tier')
            deadline = time.monotonic() + 60
            while time.monotonic() < deadline:
                code, data, _ = request('HEAD', 'converted/item.txt', query={'versionId': current_version})
                if code == 404:
                    break
                assert code in (200, 503), (code, data[:100])
                time.sleep(0.2)
            else:
                raise AssertionError('persisted deletion did not resume after source restart and tier recovery')
            record['checks'].append('version-specific restore serves local data during tier outage; deletion intent retains its reference and resumes after restart without lifecycle rules')
            check('DELETE', 'converted/item.txt', query={'versionId': current_version}, status=204)
            check('GET', 'converted/item.txt', query={'versionId': current_version}, status=404)
            assert list_tier('current-tier') == []
            cli('admin', 'bucket', 'remote', 'rm', 'store/source-fixture', '--arn', arns['CURRENT'])
            multi = ('<Delete><Object><Key>versions/item.txt</Key><VersionId>' + old_version + '</VersionId></Object>'
                     '<Object><Key>versions/empty.bin</Key><VersionId>' + empty_version + '</VersionId></Object></Delete>').encode()
            data, _ = check('POST', body=multi, query={'delete': ''}, headers={'Content-Md5': base64.b64encode(hashlib.md5(multi).digest()).decode()})
            assert not ET.fromstring(data).findall('{*}Error')
            assert list_tier('noncurrent-tier') == []
            cli('admin', 'bucket', 'remote', 'rm', 'store/source-fixture', '--arn', arns['NONCURRENT'])
            record['checks'].append('single and bulk permanent deletes remove exact tier versions before source references; drained targets can be removed')
            assert config.read_bytes() == original_config
            record['checks'].append('synthetic OC aliases remain unchanged and all owned fixture processes are stopped')
            record['status'] = 'passed'
        finally:
            for process in processes.values():
                fixture.stop(process)
            for handle in handles:
                handle.close()


def main():
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--cli', required=True)
    parser.add_argument('--console', required=True)
    parser.add_argument('--server', required=True)
    parser.add_argument('--server-source', required=True)
    parser.add_argument('--server-patch', required=True)
    parser.add_argument('--output', required=True)
    parser.add_argument('--scenarios', default='single-http,dual-tls')
    args = parser.parse_args()
    report = {'dateUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(), 'status': 'running',
              'scope': 'independent source and tier processes, four-disk erasure, actual scanner, OC protected lifecycle writes',
              'serverSource': args.server_source, 'serverPatchSHA256': fixture.digest(args.server_patch),
              'binaries': {name: {'sha256': fixture.digest(path)} for name, path in [('oc', args.cli), ('oc-console', args.console), ('otterio', args.server)]},
              'harnessSHA256': fixture.digest(__file__), 'scenarios': []}
    output = Path(args.output)
    output.parent.mkdir(parents=True, exist_ok=True)
    try:
        for name in args.scenarios.split(','):
            record = {}
            report['scenarios'].append(record)
            scenario(args, name, record)
        report['status'] = 'passed'
    except Exception as error:
        report['status'] = 'failed'
        if report['scenarios']:
            report['scenarios'][-1]['status'] = 'failed'
        report['error'] = str(error)
        raise
    finally:
        output.write_text(json.dumps(report, ensure_ascii=False, indent=2) + '\n')


if __name__ == '__main__':
    main()
