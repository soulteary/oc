"""Native browser login against disposable real IAM users over verified HTTPS."""
import json
import secrets
import ssl
import subprocess
import time
import urllib.parse


def verify_native(args, root, cli, browser, stop, certificate, free_port, env,
                  credentials, endpoint, admin, s3ca, adminca, bucket, viewer_access,
                  viewer_secret, root_access, root_secret, checks):
    tls_dir = root / 'native-browser-tls'
    browser_ca = certificate(tls_dir)
    base = f'https://127.0.0.1:{free_port()}'
    data_dir = root / 'native-preferences'
    log_path = root / 'native-console.log'
    process = None
    other_access, other_secret = 'native-' + secrets.token_hex(6), secrets.token_hex(24)
    wrong_secret = secrets.token_hex(24)
    credentials.extend([other_access, other_secret, wrong_secret])
    cli('admin', 'user', 'add', 'store', other_access, other_secret)
    policy = root / 'native-exact-object-policy.json'
    policy.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
        {'Effect': 'Allow', 'Action': ['s3:GetBucketLocation'], 'Resource': ['arn:aws:s3:::' + bucket]},
        {'Effect': 'Allow', 'Action': ['s3:GetObject'], 'Resource': ['arn:aws:s3:::' + bucket + '/stream.bin']},
    ]}))
    cli('admin', 'policy', 'add', 'store', 'console-native-exact-object', str(policy))
    cli('admin', 'policy', 'set', 'store', 'console-native-exact-object', 'user=' + other_access)
    with log_path.open('wb') as log:
        try:
            process = subprocess.Popen([args.console, '--auth-mode', 'native',
                '--s3-url', endpoint, '--admin-url', admin, '--s3-ca', str(s3ca),
                '--admin-ca', str(adminca), '--address', urllib.parse.urlsplit(base).netloc,
                '--public-url', base, '--tls-cert', str(browser_ca),
                '--tls-key', str(tls_dir / 'private.key'), '--data-dir', str(data_dir)],
                env=env, stdout=log, stderr=log)
            deadline = time.monotonic() + 10
            while 'URL: ' not in log_path.read_text():
                if process.poll() is not None or time.monotonic() > deadline:
                    raise RuntimeError('native HTTPS console did not start')
                time.sleep(0.02)
            tls_context = ssl.create_default_context(cafile=str(browser_ca))
            alice, bob, rejected = [browser(base, tls_context) for _ in range(3)]
            page = alice('/')[0]
            assert b'data-auth-mode="native"' in page
            alice('/api/session', expected=401)
            for key, secret in ((root_access, root_secret), (viewer_access, wrong_secret)):
                rejected('/api/login', 'POST', {'accessKey': key, 'secretKey': secret}, expected=401)
                assert not list(rejected.jar)
            rejected('/api/login', 'POST', {'code': 'no-shared-code'}, expected=401)
            alice_session = json.loads(alice('/api/login', 'POST', {'accessKey': viewer_access, 'secretKey': viewer_secret})[0])
            bob_session = json.loads(bob('/api/login', 'POST', {'accessKey': other_access, 'secretKey': other_secret})[0])
            for request in (alice, bob):
                cookies = list(request.jar)
                assert len(cookies) == 1
                cookie = cookies[0]
                assert cookie.name == '__Host-oc_console_session' and cookie.secure and not cookie.domain_specified
                assert cookie.path == '/' and cookie.has_nonstandard_attr('HttpOnly')
                assert cookie.get_nonstandard_attr('SameSite') == 'Lax'
            assert alice_session['readOnly'] and bob_session['readOnly']
            query = urllib.parse.urlencode({'bucket': bucket, 'key': 'plain.txt'})
            assert alice('/api/download?' + query)[0] == b'console acceptance\n'
            bob('/api/download?' + query, expected=403)
            query = urllib.parse.urlencode({'bucket': bucket, 'key': 'stream.bin'})
            bob('/api/download?' + query)
            alice('/api/download?' + query, expected=403)
            # A native user without ListAllMyBuckets can authenticate and read
            # its exact object; neither root permissions nor another user leak.
            bob('/api/buckets', expected=403)
            checks.append('native HTTPS login requires signed enabled IAM identity; root, bad secret and local code rejected; host-only Secure HttpOnly cookie')
            checks.append('two native users retain independent object permissions; exact-object user logs in without bucket-list permission')
            alice_headers = {'X-CSRF-Token': alice_session['csrfToken']}
            alice('/api/preferences', 'PUT', {'action': 'language', 'language': 'en'}, headers=alice_headers)
            assert json.loads(alice('/api/preferences')[0])['language'] == 'en'
            assert json.loads(bob('/api/preferences')[0])['language'] == 'zh'
            another = browser(base, tls_context)
            another_session = json.loads(another('/api/login', 'POST', {'accessKey': viewer_access, 'secretKey': viewer_secret})[0])
            assert json.loads(another('/api/preferences')[0])['language'] == 'en'
            for path in ('/api/uploads', '/api/iam/actions', '/api/self-secret'):
                alice(path, 'POST', {}, expected=403, headers=alice_headers)
            alice('/api/logout', 'POST', expected=403)
            alice('/api/logout', 'POST', expected=204, headers=alice_headers)
            alice('/api/session', expected=401)
            bob('/api/session')
            another('/api/session')
            another('/api/logout', 'POST', expected=204, headers={'X-CSRF-Token': another_session['csrfToken']})
            # Reload after all of that principal's sessions have ended: preference
            # identity is stable across newly-created credential clients.
            another('/api/login', 'POST', {'accessKey': viewer_access, 'secretKey': viewer_secret})
            assert json.loads(another('/api/preferences')[0])['language'] == 'en'
            assert len(list(data_dir.glob('*.json'))) == 1  # only Alice wrote preferences
            for path in data_dir.glob('*.json'):
                assert len(path.stem) == 64
                content = path.read_bytes()
                assert not any(value.encode() in content for value in credentials)
            checks.append('native preferences persist per target/user; same-user sessions share them, other users do not; logout is browser-specific and writes are blocked')
            stop(process)
            assert process.returncode == 0
            # Restore the fixture's IAM set before later feature acceptance lists
            # users. Only this phase's disposable user/policy are removed.
            cli('admin', 'user', 'remove', 'store', other_access)
            cli('admin', 'policy', 'remove', 'store', 'console-native-exact-object')
            assert not any(value in log_path.read_text() for value in credentials)
            checks.append('native process shutdown drains clients and startup/preferences disclose no credentials')
            return {'users': 2, 'browserTLS': 'verified', 'storageTLS': 'verified', 'rootLogin': 'denied', 'writes': 'disabled'}
        finally:
            stop(process)
