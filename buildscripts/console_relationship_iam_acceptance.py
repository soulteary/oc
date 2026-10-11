"""Real relationship IAM checks, called only with a disposable integration fixture."""
import json
import importlib.util
from pathlib import Path
import secrets
import ssl
import subprocess
import time
import urllib.parse


def binding_action(kind, target, snapshot, policies):
    if snapshot.get('kind') != kind or snapshot.get('target') != target:
        raise RuntimeError('binding snapshot target mismatch')
    revision = snapshot.get('revision', '')
    if not snapshot.get('conditional') or len(revision) != 64:
        raise RuntimeError('conditional binding capability missing')
    return {'action': kind + '.policies', kind: target, 'policies': policies,
            'revision': revision, 'confirmTarget': target}


def require_confirmed(result, self_target=False):
    if result.get('outcome') != 'confirmed' or result.get('loginRequired') is not self_target:
        raise RuntimeError('relationship acknowledgement does not match actor scope')


def verify_relationship_iam(args, root, cli, browser, stop, certificate, free_port, env,
                            credentials, endpoint, admin, s3ca, adminca, bucket, viewer_access,
                            viewer_secret, root_access, root_secret, checks):
    """No fake backend, discovery rewriting, root Console login, or capability fallback."""
    tls_dir = root / 'relationship-browser-tls'
    browser_ca = certificate(tls_dir)
    base = 'https://127.0.0.1:' + str(free_port())
    tls_context = ssl.create_default_context(cafile=str(browser_ca))
    log_path = root / 'relationship-console.log'
    prefix = 'rel-' + secrets.token_hex(4)
    names = {role: prefix + '-' + role for role in ('manager', 'target', 'unrelated', 'denied')}
    secrets_by_role = {role: secrets.token_hex(24) for role in names}
    policies, groups = [], []
    processes = []
    child = None
    config_path = root / 'config' / 'config.json'
    original_config = config_path.read_bytes()

    def add_policy(label, statements):
        name = prefix + '-' + label
        path = root / (name + '.json')
        path.write_text(json.dumps({'Version': '2012-10-17', 'Statement': statements}))
        cli('admin', 'policy', 'add', 'store', name, str(path))
        policies.append(name)
        return name

    def start(writes=True):
        destination = log_path if writes else root / 'relationship-readonly-console.log'
        address = base if writes else 'https://127.0.0.1:' + str(free_port())
        log = destination.open('wb')
        command = [args.console, '--auth-mode', 'native', '--s3-url', endpoint,
                   '--admin-url', admin, '--s3-ca', str(s3ca), '--admin-ca', str(adminca),
                   '--public-url', address, '--address', urllib.parse.urlsplit(address).netloc,
                   '--tls-cert', str(browser_ca), '--tls-key', str(tls_dir / 'private.key'),
                   '--data-dir', str(root / ('relationship-data' if writes else 'relationship-readonly-data'))]
        if writes:
            command.append('--allow-writes')
        process = subprocess.Popen(command, env=env, stdout=log, stderr=log)
        processes.append((process, log))
        deadline = time.monotonic() + 15
        while 'URL: ' not in destination.read_text():
            if process.poll() is not None or time.monotonic() >= deadline:
                raise RuntimeError('relationship Console startup failed')
            time.sleep(.02)
        return address

    def login(role, secret=None, address=base):
        client = browser(address, tls_context)
        result = json.loads(client('/api/login', 'POST', {'accessKey': names[role],
                            'secretKey': secret or secrets_by_role[role]})[0])
        client.csrf = {'X-CSRF-Token': result['csrfToken']}
        return client

    def action(client, body, expected=200, csrf=True):
        payload = client('/api/iam/actions', 'POST', body, expected=expected,
                         headers=client.csrf if csrf else {},
                         allowed_credentials=tuple(names.values()) + ((child['accessKey'],) if child else ()))[0]
        return json.loads(payload)

    def bindings(client, kind, target):
        query = urllib.parse.urlencode({'kind': kind, 'target': target})
        return json.loads(client('/api/iam/bindings?' + query, allowed_credentials=tuple(names.values()))[0])

    def child_request(method, key, payload=b'', expected=200):
        spec = importlib.util.spec_from_file_location('relationship_fixture_signer', Path(__file__).with_name('test-console-integration.py'))
        module = importlib.util.module_from_spec(spec)
        spec.loader.exec_module(module)
        uri = '/' + urllib.parse.quote(bucket + '/' + key, safe='/-_.~')
        status, body, _ = module.signed_request(endpoint, method, uri, payload, child['accessKey'], child['secretKey'], ssl.create_default_context(cafile=str(s3ca)))
        if status != expected:
            raise RuntimeError('real service child signed S3 permission mismatch')
        return body

    def rotate(client, role, secret, expected=200):
        credentials.append(secret)
        return action(client, {'action': 'user.rotate', 'user': names[role],
                              'secretKey': secret, 'confirmTarget': names[role]}, expected)

    try:
        for role, name in names.items():
            credentials.extend([name, secrets_by_role[role]])
            cli('admin', 'user', 'add', 'store', name, secrets_by_role[role])
            cli('admin', 'policy', 'set', 'store', 'consoleAdmin' if role in ('manager', 'target') else 'readonly', 'user=' + name)
        denied_policy = add_policy('denied', [
            {'Effect': 'Allow', 'Action': ['admin:*', 's3:*'], 'Resource': ['arn:aws:s3:::*']},
            {'Effect': 'Deny', 'Action': ['admin:CreateUser', 'admin:EnableGroup', 'admin:DisableGroup', 'admin:AddUserToGroup', 'admin:RemoveUserFromGroup', 'admin:UpdateServiceAccount', 'admin:RemoveServiceAccount'], 'Resource': ['arn:aws:s3:::*']}])
        cli('admin', 'policy', 'set', 'store', denied_policy, 'user=' + names['denied'])
        start()
        manager, target, target_second, unrelated, denied = (login(role) for role in ('manager', 'target', 'target', 'unrelated', 'denied'))
        readonly_address = start(False)
        readonly_manager = login('manager', address=readonly_address)
        candidate = secrets.token_hex(24)
        credentials.append(candidate)
        rotate(readonly_manager, 'target', candidate, 403)
        rotate(unrelated, 'target', candidate, 403)
        rotate(denied, 'target', candidate, 403)
        body = {'action': 'user.rotate', 'user': names['target'], 'secretKey': candidate, 'confirmTarget': names['target']}
        action(manager, body, 403, csrf=False)
        target('/api/session');target_second('/api/session');unrelated('/api/session')
        checks.append('relationship user rotation rejects default-readonly, ordinary user, explicit deny and missing CSRF without retiring unaffected sessions')

        if getattr(args, 'expect_relationship_unsupported', False):
            error = rotate(manager, 'target', candidate, (403, 501))
            if error.get('code') not in ('identity_changes_disabled', 'identity_relationship_unknown', 'identity_relationships_unknown', 'iam_relationships_unsupported', 'identity_impact_unsupported'):
                raise RuntimeError('missing capability rejection was not explicit')
            snapshot = bindings(manager, 'user', names['target'])
            if snapshot.get('conditional'):
                action(manager, binding_action('user', names['target'], snapshot, ['readonly']), (403, 501))
            else:
                action(manager, {'action': 'user.policies', 'user': names['target'], 'policies': ['readonly'], 'revision': '0'*64, 'confirmTarget': names['target']}, (403, 501))
            login('target');target('/api/session');target_second('/api/session');unrelated('/api/session')
            browser(base, tls_context)('/api/login', 'POST', {'accessKey': names['target'], 'secretKey': candidate}, expected=401)
            checks.append('real legacy server missing coupled relationship capability rejects secret/binding mutations; original credentials and sessions remain valid')
            return {'capabilityMode': 'expected-unsupported', 'unsupportedActionsPassed': ['user.rotate', 'user.policies'], 'positiveMutationCoverage': []}

        # Real child credential exists, but native browser login intentionally accepts only IAM users.
        created = action(manager, {'action': 'service-account.create', 'user': names['target'], 'confirmTarget': names['target']})
        child = created['credentials']
        credentials.extend([child['accessKey'], child['secretKey']])
        browser(base, tls_context)('/api/login', 'POST', child, expected=401)
        child_request('GET', 'plain.txt')
        child_request('PUT', prefix + '-child.txt', b'child-object')
        checks.append('real service child native browser login is rejected; child browser retirement is outside the supported native login scope')
        replacement = secrets.token_hex(24)
        require_confirmed(rotate(manager, 'target', replacement))
        target('/api/session', expected=401);target_second('/api/session', expected=401)
        unrelated('/api/session');manager('/api/session')
        browser(base, tls_context)('/api/login', 'POST', {'accessKey': names['target'], 'secretKey': secrets_by_role['target']}, expected=401)
        child_request('GET', 'plain.txt')
        checks.append('parent secret rotation does not claim upstream service credential revocation; real child retains authorized signed S3 read')
        secrets_by_role['target'] = replacement
        target, target_second = login('target'), login('target')
        checks.append('administrator rotates another user with a real service child; all parent sessions retire, new credentials work and unrelated sessions continue')

        cli('admin', 'user', 'disable', 'store', names['target'])
        target('/api/session', expected=401)
        disabled_secret = secrets.token_hex(24)
        require_confirmed(rotate(manager, 'target', disabled_secret))
        secrets_by_role['target'] = disabled_secret
        browser(base, tls_context)('/api/login', 'POST', {'accessKey': names['target'], 'secretKey': disabled_secret}, expected=401)
        cli('admin', 'user', 'enable', 'store', names['target'])
        target, target_second = login('target'), login('target')
        checks.append('administrator changes disabled user secret without enabling account; login stays rejected until explicit activation')

        for kind in ('user', 'group'):
            name = names['target'] if kind == 'user' else prefix + '-group'
            if kind == 'group':
                groups.append(name)
                cli('admin', 'group', 'add', 'store', name, names['target'])
                cli('admin', 'policy', 'set', 'store', 'readonly', 'group=' + name)
            stale = bindings(manager, kind, name)
            cli('admin', 'policy', 'set', 'store', 'readwrite', kind + '=' + name)
            failure = action(manager, binding_action(kind, name, stale, ['readonly']), 409)
            if failure.get('code') != 'identity_revision_conflict':
                raise RuntimeError('stale binding review did not produce revision conflict')
            current = bindings(manager, kind, name)
            if current['policies'] != ['readwrite']:
                raise RuntimeError('stale binding request overwrote newer policy')
            target('/api/session');target_second('/api/session');unrelated('/api/session')
            require_confirmed(action(manager, binding_action(kind, name, current, ['readonly'])))
            target('/api/session', expected=401);target_second('/api/session', expected=401)
            manager('/api/session');unrelated('/api/session')
            child_request('GET', 'plain.txt')
            child_request('PUT', prefix + '-child.txt', b'denied-overwrite', 403)
            target, target_second = login('target'), login('target')
            checks.append(kind + ' binding stale revision conflicts without overwrite/retirement; fresh coupled edit retires affected parent sessions only')

        group = groups[0]
        def group_action(name, members=None):
            body = {'action': 'group.' + name, 'group': group, 'confirmTarget': group}
            if members is not None:
                body['members'] = members
            result = action(manager, body)
            require_confirmed(result)
            expected_status = {'enable': 'enabled', 'disable': 'disabled', 'delete': 'deleted'}.get(name)
            if expected_status and result['group']['status'] != expected_status:
                raise RuntimeError('group mutation did not confirm requested status')
            return result

        group_body = {'action': 'group.disable', 'group': group, 'confirmTarget': group}
        for rejected_actor in (readonly_manager, unrelated, denied):
            action(rejected_actor, group_body, 403)
        action(manager, group_body, 403, csrf=False)
        target('/api/session');unrelated('/api/session')
        checks.append('group mutation rejects readonly, ordinary user, explicit deny and missing CSRF without affecting existing sessions')
        group_action('disable')
        target('/api/session', expected=401);target_second('/api/session', expected=401)
        unrelated('/api/session');manager('/api/session')
        target, target_second = login('target'), login('target')
        group_action('enable')
        target('/api/session', expected=401);target_second('/api/session', expected=401)
        target, target_second = login('target'), login('target')
        group_action('add-members', [names['unrelated']])
        target('/api/session', expected=401);unrelated('/api/session', expected=401)
        manager('/api/session')
        target, target_second, unrelated = login('target'), login('target'), login('unrelated')
        group_action('remove-members', [names['target'], names['unrelated']])
        target('/api/session', expected=401);target_second('/api/session', expected=401);unrelated('/api/session', expected=401)
        manager('/api/session')
        target, target_second, unrelated = login('target'), login('target'), login('unrelated')
        group_action('delete')
        groups.remove(group)
        manager('/api/session');target('/api/session');unrelated('/api/session')
        checks.append('group disable/enable and member add/remove retire captured affected user sessions; empty group deletion keeps unrelated sessions available')

        def service_action(name, **extra):
            result = action(manager, {'action': 'service-account.' + name,
                            'accessKey': child['accessKey'], 'confirmTarget': child['accessKey'], **extra})
            require_confirmed(result)
            if result['serviceAccount']['accessKey'] != child['accessKey'] or result['serviceAccount']['parentUser'] != names['target']:
                raise RuntimeError('service mutation response changed target or parent')
            expected_status = {'enable': 'on', 'disable': 'off', 'delete': 'deleted'}.get(name)
            if expected_status and result['serviceAccount']['status'] != expected_status:
                raise RuntimeError('service mutation did not confirm requested status')
            return result

        service_body = {'action': 'service-account.disable', 'accessKey': child['accessKey'], 'confirmTarget': child['accessKey']}
        for rejected_actor in (readonly_manager, unrelated, denied):
            action(rejected_actor, service_body, 403)
        action(manager, service_body, 403, csrf=False)
        child_request('GET', 'plain.txt');target('/api/session');unrelated('/api/session')
        checks.append('service mutation rejects readonly, ordinary user, explicit deny and missing CSRF without changing child credentials or parent sessions')
        service_action('disable')
        child_request('GET', 'plain.txt', expected=403)
        manager('/api/session');target('/api/session');unrelated('/api/session')
        service_action('enable')
        child_request('GET', 'plain.txt')
        new_child_secret = secrets.token_hex(24)
        credentials.append(new_child_secret)
        service_action('rotate', secretKey=new_child_secret)
        child_request('GET', 'plain.txt', expected=403)
        child['secretKey'] = new_child_secret
        child_request('GET', 'plain.txt')
        narrow_policy = json.dumps({'Version': '2012-10-17', 'Statement': [
            {'Effect': 'Allow', 'Action': ['s3:GetObject'], 'Resource': ['arn:aws:s3:::' + bucket + '/stream.bin']}]})
        service_action('policy', policy=narrow_policy)
        child_request('GET', 'plain.txt', expected=403)
        child_request('GET', 'stream.bin')
        child_request('PUT', prefix + '-child.txt', b'denied', 403)
        service_action('delete')
        child_request('GET', 'stream.bin', expected=403)
        child = None
        cli('rm', 'store/' + bucket + '/' + prefix + '-child.txt')
        target('/api/session');target_second('/api/session');unrelated('/api/session');manager('/api/session')
        checks.append('real service child disable/enable, secret rotation, parent-intersected policy and deletion enforce signed S3 credentials while native parent/unrelated browser sessions continue')

        # Regrant admin directly before testing admin self-targeted rotation.
        cli('admin', 'policy', 'set', 'store', 'consoleAdmin', 'user=' + names['manager'])
        manager_second = login('manager')
        self_secret = secrets.token_hex(24)
        require_confirmed(rotate(manager, 'manager', self_secret), True)
        manager('/api/session', expected=401);manager_second('/api/session', expected=401)
        unrelated('/api/session');target('/api/session')
        secrets_by_role['manager'] = self_secret
        login('manager')
        checks.append('administrator self rotation acknowledges loginRequired before retiring own sessions; new login and unrelated users remain available')
        return {'capabilityMode': 'required-supported', 'positiveMutationCoverage': ['user.rotate-other', 'user.rotate-self', 'disabled-user.rotate', 'user.policies', 'group.policies', 'group.status-members-delete', 'service-account.status-secret-policy-delete'], 'serviceChildBrowserSessions': 'unsupported: native service login is rejected, not counted as retirement coverage', 'serviceChildSignedS3': 'parent secret rotation retains authorized read; policy narrowing retains read and denies write', 'tls': 'verified independent fixture browser and storage/Admin CA'}
    finally:
        for process, log in reversed(processes):
            stop(process)
            log.close()
        # Restore the fixture IAM set before other acceptance phases run.
        if child:
            config = json.loads(original_config)
            config['aliases']['relationship-manager'] = {**config['aliases']['store'], 'accessKey': names['manager'], 'secretKey': secrets_by_role['manager']}
            config_path.write_text(json.dumps(config))
            config_path.chmod(0o600)
            try:
                cli('admin', 'user', 'svcacct', 'rm', 'relationship-manager', child['accessKey'])
            finally:
                config_path.write_bytes(original_config)
                config_path.chmod(0o600)
            cli('rm', 'store/' + bucket + '/' + prefix + '-child.txt')
        for group in groups:
            cli('admin', 'group', 'remove', 'store', group, names['target'])
            cli('admin', 'group', 'remove', 'store', group)
        for name in names.values():
            cli('admin', 'user', 'remove', 'store', name)
        for policy in policies:
            cli('admin', 'policy', 'remove', 'store', policy)
        config_path.write_bytes(original_config)
        config_path.chmod(0o600)
        for log in root.glob('relationship*console.log'):
            if any(value and value in log.read_text() for value in credentials):
                raise RuntimeError('relationship Console log disclosed fixture credentials')
