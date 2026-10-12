"""Native browser login against disposable real IAM users over verified HTTPS."""
import json
import secrets
import ssl
import subprocess
import time
import urllib.parse


def native_iam_action(client, body, headers, public_access_keys, expected=200):
    # These names were created by this fixture; no existing or generated secret
    # enters the exemption. Coupled acknowledgements include their public scope.
    return json.loads(client('/api/iam/actions', 'POST', body, expected=expected,
                             headers=headers, allowed_credentials=tuple(public_access_keys))[0])


def require_user_change(result, target, status, login_required, expected_access_keys):
    impact = result.get('impact', {})
    affected = impact.get('affectedAccessKeys')
    expected = set(expected_access_keys)
    if (result.get('outcome') != 'confirmed' or
            result.get('user') != {'accessKey': target, 'status': status} or
            result.get('loginRequired') is not login_required or
            result.get('restartRequired', False) is not False or
            impact.get('kind') != 'user' or impact.get('target') != target or
            impact.get('state') != 'known' or impact.get('coupledUsers') is not True or
            target not in expected or not isinstance(affected, list) or
            not all(isinstance(key, str) for key in affected) or
            len(affected) != len(expected) or set(affected) != expected):
        raise RuntimeError('native user mutation acknowledgement does not match its target and retirement scope')


def require_service_account_inventory(client, user, access_key, present, public_access_keys):
    # Require a successful authenticated management read. A missing parent,
    # denied query or malformed response cannot establish child deletion.
    query = urllib.parse.urlencode({'user': user})
    result = json.loads(client('/api/iam/service-accounts?' + query, expected=200,
                               allowed_credentials=tuple(public_access_keys))[0])
    if not isinstance(result, dict) or result.get('user') != user or not isinstance(result.get('serviceAccounts'), list):
        raise RuntimeError('native service-account inventory did not confirm the requested parent')
    accounts = result['serviceAccounts']
    if (any(not isinstance(account, dict) or not isinstance(account.get('accessKey'), str) or
            account.get('parentUser') != user for account in accounts) or
            (any(account['accessKey'] == access_key for account in accounts)) is not present):
        raise RuntimeError('native service-account inventory did not confirm the expected child presence')


def verify_native(args, root, cli, browser, stop, certificate, free_port, env,
                  credentials, endpoint, admin, s3ca, adminca, bucket, viewer_access,
                  viewer_secret, root_access, root_secret, checks, signed_request):
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
    write_bucket = 'native-write-' + secrets.token_hex(6)
    policy = root / 'native-exact-object-policy.json'
    policy.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
        {'Effect': 'Allow', 'Action': ['s3:*'], 'Resource': ['arn:aws:s3:::' + write_bucket, 'arn:aws:s3:::' + write_bucket + '/*']},
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
                '--tls-key', str(tls_dir / 'private.key'), '--data-dir', str(data_dir), '--allow-writes'],
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
            assert not alice_session['readOnly'] and not bob_session['readOnly']
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
            bob_headers = {'X-CSRF-Token': bob_session['csrfToken']}
            alice('/api/buckets/create', 'POST', {'bucket': write_bucket}, expected=403, headers=alice_headers)
            bob('/api/buckets/create', 'POST', {'bucket': write_bucket}, headers=bob_headers)
            payload = b'native isolated upload\n'
            upload = json.loads(bob('/api/uploads', 'POST', {'bucket': write_bucket, 'key': 'own.txt', 'size': len(payload)}, expected=201, headers=bob_headers)[0])
            alice('/api/uploads/' + upload['id'], 'PUT', payload, expected=404, headers=alice_headers)
            completed = json.loads(bob('/api/uploads/' + upload['id'], 'PUT', payload, headers=bob_headers)[0])
            assert completed['status'] == 'succeeded'
            own_query = urllib.parse.urlencode({'bucket': write_bucket, 'key': 'own.txt'})
            assert bob('/api/download?' + own_query)[0] == payload
            alice('/api/download?' + own_query, expected=403)
            cli('rm', 'store/' + write_bucket + '/own.txt')
            bob('/api/buckets/delete', 'POST', {'bucket': write_bucket, 'confirmBucket': write_bucket}, headers=bob_headers)
            checks.append('native write opt-in preserves upstream permissions: authorized user creates/deletes its exact bucket and uploads exact bytes; other user denied object and upload task access')
            for path in ('/api/iam/actions',):
                alice(path, 'POST', {}, expected=403, headers=alice_headers)
            bob_other = browser(base, tls_context)
            bob_other('/api/login', 'POST', {'accessKey': other_access, 'secretKey': other_secret})
            changed_secret = secrets.token_hex(24)
            credentials.append(changed_secret)
            changed = json.loads(bob('/api/self-secret', 'POST', {'newSecret': changed_secret, 'confirm': True}, headers=bob_headers)[0])
            assert changed == {'outcome': 'confirmed', 'loginRequired': True}
            bob('/api/session', expected=401)
            bob_other('/api/session', expected=401)
            alice('/api/session')
            another('/api/session')
            bob('/api/login', 'POST', {'accessKey': other_access, 'secretKey': other_secret}, expected=401)
            bob('/api/login', 'POST', {'accessKey': other_access, 'secretKey': changed_secret})
            checks.append('native self-secret rotation revokes all sessions of that user only; old secret fails, new secret logs in, other users remain available')
            cli('admin', 'user', 'disable', 'store', other_access)
            bob('/api/session', expected=401)
            alice('/api/session')
            cli('admin', 'user', 'enable', 'store', other_access)
            bob('/api/session', expected=401)  # revoked cookie cannot revive
            bob('/api/login', 'POST', {'accessKey': other_access, 'secretKey': changed_secret})
            bob('/api/session')
            checks.append('external IAM disable revokes the next native request; enable cannot revive old session; fresh login and unrelated users remain available')
            external_secret = secrets.token_hex(24)
            credentials.append(external_secret)
            cli('admin', 'user', 'add', 'store', other_access, external_secret)
            bob('/api/session', expected=401)
            bob('/api/login', 'POST', {'accessKey': other_access, 'secretKey': changed_secret}, expected=401)
            bob('/api/login', 'POST', {'accessKey': other_access, 'secretKey': external_secret})
            alice('/api/session')
            checks.append('external secret replacement revokes the old native browser credentials; only the new secret can log in')
            manager_access, manager_secret = 'native-manager-' + secrets.token_hex(4), secrets.token_hex(24)
            credentials.extend([manager_access, manager_secret])
            cli('admin', 'user', 'add', 'store', manager_access, manager_secret)
            cli('admin', 'policy', 'set', 'store', 'consoleAdmin', 'user=' + manager_access)
            manager = browser(base, tls_context)
            manager_session = json.loads(manager('/api/login', 'POST', {'accessKey': manager_access, 'secretKey': manager_secret})[0])
            manager_headers = {'X-CSRF-Token': manager_session['csrfToken']}
            new_user, new_group = 'native-created-' + secrets.token_hex(4), 'native-group-' + secrets.token_hex(4)
            create_user = {'action': 'user.create', 'user': new_user, 'confirmTarget': new_user}
            alice('/api/iam/actions', 'POST', create_user, expected=403, headers=alice_headers)
            created = json.loads(manager('/api/iam/actions', 'POST', create_user, headers=manager_headers)[0])
            assert created['outcome'] == 'confirmed' and not created['restartRequired']
            credentials.extend([created['credentials']['accessKey'], created['credentials']['secretKey']])
            public_access_keys = [manager_access, created['credentials']['accessKey']]
            manager('/api/iam/actions', 'POST', create_user, expected=409, headers=manager_headers)
            manager('/api/iam/actions', 'POST', {'action': 'group.create', 'group': new_group, 'confirmTarget': new_group}, headers=manager_headers)
            if getattr(args, 'expect_relationship_unsupported', False):
                # This historical fixture implements create-only IAM, but no
                # relationship proof. Every coupled write must fail closed.
                created_browser = browser(base, tls_context)
                created_browser('/api/login', 'POST', {'accessKey': new_user, 'secretKey': created['credentials']['secretKey']})
                unsupported_actions = [
                    {'action': 'user.' + operation, 'user': new_user, 'confirmTarget': new_user}
                    for operation in ('enable', 'disable', 'delete')
                ] + [{'action': 'service-account.create', 'user': new_user, 'confirmTarget': new_user}]
                for request in unsupported_actions:
                    for actor, headers in ((alice, alice_headers), (manager, manager_headers)):
                        denied = json.loads(actor('/api/iam/actions', 'POST', request, expected=501, headers=headers)[0])
                        assert denied['code'] == 'identity_relationships_unknown'
                    created_browser('/api/session')
                    manager('/api/session')
                    alice('/api/session')
                    bob('/api/session')
                cli('admin', 'user', 'remove', 'store', new_user)
                cli('admin', 'group', 'remove', 'store', new_group)
                cli('admin', 'user', 'remove', 'store', manager_access)
                manager('/api/session', expected=401)
                checks.append('legacy create-only native user/group creation succeeds; user enable/disable/delete and service creation require relationship proofs, return explicit unsupported errors, and preserve every existing session')
            else:
                created_browser = browser(base, tls_context)
                new_secret = created['credentials']['secretKey']
                created_browser('/api/login', 'POST', {'accessKey': new_user, 'secretKey': new_secret})
                cli('admin', 'user', 'disable', 'store', new_user)
                created_browser('/api/session', expected=401)
                enable_user = {'action': 'user.enable', 'user': new_user, 'confirmTarget': new_user}
                alice('/api/iam/actions', 'POST', enable_user, expected=403, headers=alice_headers)
                require_user_change(native_iam_action(manager, enable_user, manager_headers, public_access_keys), new_user, 'enabled', False, [new_user])
                created_browser('/api/session', expected=401)
                created_browser('/api/login', 'POST', {'accessKey': new_user, 'secretKey': new_secret})
                require_user_change(native_iam_action(manager, {'action': 'user.enable', 'user': manager_access, 'confirmTarget': manager_access}, manager_headers, public_access_keys), manager_access, 'enabled', True, [manager_access])
                manager('/api/session', expected=401)
                manager_session = json.loads(manager('/api/login', 'POST', {'accessKey': manager_access, 'secretKey': manager_secret})[0])
                manager_headers = {'X-CSRF-Token': manager_session['csrfToken']}
                checks.append('native account activation enforces manager permission, never revives revoked sessions and acknowledges self-target enable before requiring a fresh manager login')
                cli('admin', 'policy', 'set', 'store', 'readonly', 'user=' + new_user)
                service = native_iam_action(manager, {'action': 'service-account.create', 'user': new_user, 'confirmTarget': new_user}, manager_headers, public_access_keys)
                service_key = service['credentials']['accessKey']
                credentials.extend([service_key, service['credentials']['secretKey']])
                public_access_keys.append(service_key)
                require_service_account_inventory(manager, new_user, service_key, True, public_access_keys)
                def service_read(expected):
                    uri = '/' + urllib.parse.quote(bucket + '/plain.txt', safe='/-_.~')
                    status, _, _ = signed_request(endpoint, 'GET', uri, b'', service_key,
                                                 service['credentials']['secretKey'], ssl.create_default_context(cafile=str(s3ca)))
                    if status != expected:
                        raise RuntimeError('native user lifecycle did not preserve the expected signed service-child permission')
                service_read(200)
                second_created = browser(base, tls_context)
                second_created('/api/login', 'POST', {'accessKey': new_user, 'secretKey': new_secret})
                disable_user = {'action': 'user.disable', 'user': new_user, 'confirmTarget': new_user}
                alice('/api/iam/actions', 'POST', disable_user, expected=403, headers=alice_headers)
                disabled = native_iam_action(manager, disable_user, manager_headers, public_access_keys)
                require_user_change(disabled, new_user, 'disabled', False, [new_user, service_key])
                service_read(403)
                rejected('/api/login', 'POST', {'accessKey': new_user, 'secretKey': new_secret}, expected=401)
                # Enable before either old browser makes another request: explicit
                # target retirement must not rely on request-time credential denial.
                require_user_change(native_iam_action(manager, enable_user, manager_headers, public_access_keys), new_user, 'enabled', False, [new_user, service_key])
                service_read(200)
                created_browser('/api/session', expected=401)
                second_created('/api/session', expected=401)
                created_browser('/api/login', 'POST', {'accessKey': new_user, 'secretKey': new_secret})
                checks.append('native user disable/enable confirms the complete parent/child scope, rejects/allows signed service-child reads, retires target sessions and permits only a fresh browser login')
                delete_user = {'action': 'user.delete', 'user': new_user, 'confirmTarget': new_user}
                alice('/api/iam/actions', 'POST', delete_user, expected=403, headers=alice_headers)
                cli('admin', 'group', 'add', 'store', new_group, new_user)
                group = json.loads(cli('--json', 'admin', 'group', 'info', 'store', new_group))
                assert new_user in group['members']
                created_browser('/api/session')
                alice('/api/session')
                bob('/api/session')
                second_created('/api/login', 'POST', {'accessKey': new_user, 'secretKey': new_secret})
                deleted = native_iam_action(manager, delete_user, manager_headers, public_access_keys)
                require_user_change(deleted, new_user, 'deleted', False, [new_user, service_key])
                require_service_account_inventory(manager, new_user, service_key, False, public_access_keys)
                group = json.loads(cli('--json', 'admin', 'group', 'info', 'store', new_group))
                assert group['groupName'] == new_group and new_user not in group.get('members', [])
                service_read(403)
                # Recreate the name AND secret before either old browser requests
                # identity again. Only explicit retirement keeps both cookies dead.
                cli('admin', 'user', 'add', 'store', new_user, new_secret)
                cli('admin', 'policy', 'set', 'store', 'readonly', 'user=' + new_user)
                require_service_account_inventory(manager, new_user, service_key, False, public_access_keys)
                created_browser('/api/session', expected=401)
                second_created('/api/session', expected=401)
                created_browser('/api/login', 'POST', {'accessKey': new_user, 'secretKey': new_secret})
                service_read(403)
                checks.append('native user deletion rejects unauthorized operators, removes group membership and service children confirmed by manager inventory before/after deletion and parent recreation, retires target sessions and requires fresh login')
                cli('admin', 'user', 'remove', 'store', new_user)
                cli('admin', 'group', 'remove', 'store', new_group)
                self_disabled = native_iam_action(manager, {'action': 'user.disable', 'user': manager_access, 'confirmTarget': manager_access}, manager_headers, public_access_keys)
                require_user_change(self_disabled, manager_access, 'disabled', True, [manager_access])
                cli('admin', 'user', 'enable', 'store', manager_access)
                manager('/api/session', expected=401)
                manager('/api/login', 'POST', {'accessKey': manager_access, 'secretKey': manager_secret})
                alice('/api/session')
                bob('/api/session')
                checks.append('native self-disable acknowledges before retiring own session, keeps shared console available and requires fresh login after administrative activation')
                self_deleted = native_iam_action(manager, {'action': 'user.delete', 'user': manager_access, 'confirmTarget': manager_access}, {'X-CSRF-Token': json.loads(manager('/api/session')[0])['csrfToken']}, public_access_keys)
                require_user_change(self_deleted, manager_access, 'deleted', True, [manager_access])
                manager('/api/session', expected=401)
                rejected('/api/login', 'POST', {'accessKey': manager_access, 'secretKey': manager_secret}, expected=401)
                alice('/api/session')
                bob('/api/session')
                checks.append('native self-deletion acknowledges before ending own session, rejects deleted credentials and leaves other users available')

                checks.append('native manager creates new IAM user/group/service credential with own permissions; unauthorized create, existing-user replacement rejected; other identity edits remain gated; other sessions continue')
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
            checks.append('native preferences persist per target/user; same-user sessions share them, other users do not; logout is browser-specific; unsupported IAM mutations stay blocked without stopping other sessions')
            cli('admin', 'user', 'remove', 'store', other_access)
            bob('/api/session', expected=401)
            another('/api/session')
            checks.append('external user deletion revokes that browser on its next request without ending another user session')
            stop(process)
            assert process.returncode == 0
            # Restore the fixture's IAM set before later feature acceptance lists
            # users. Only this phase's disposable user/policy are removed.
            cli('admin', 'policy', 'remove', 'store', 'console-native-exact-object')
            assert not any(value in log_path.read_text() for value in credentials)
            checks.append('native process shutdown drains clients and startup/preferences disclose no credentials')
            return {'users': 3, 'iamCreation': 'native user/group; service creation explicitly unsupported' if getattr(args, 'expect_relationship_unsupported', False) else 'native user/group/service-account; permission and duplicate-target checks verified', 'browserTLS': 'verified', 'storageTLS': 'verified', 'rootLogin': 'denied', 'writes': 'explicit opt-in; per-user bucket create/delete and self-secret rotation verified; relationship mutations explicitly unsupported without session retirement' if getattr(args, 'expect_relationship_unsupported', False) else 'explicit opt-in; per-user bucket create/delete verified; self-secret rotation verified; user disable/delete and exact-target session retirement verified; unsupported IAM mutations disabled'}
        finally:
            stop(process)
