"""Console feature acceptance using only disposable fixture accounts and data."""

import io
import concurrent.futures
import json
import secrets
import shutil
import ssl
import time
import urllib.error
import urllib.parse
import zipfile

from local_http import local_urlopen


def verify_features(root, bucket, cli, readonly, session, viewer, start_console, browser, stop,
                    credentials, endpoint, access, secret, context, signed_request, put_object,
                    checks, legacy=False):
    processes = []
    known_access = [access, credentials[2]]
    metrics = {'profile': 'legacy-protected-fallback' if legacy else 'protected-features-v1'}

    def console(alias='store', writes=False, sharing=False, config_dir=None):
        options = {'writes': writes, 'sharing': sharing}
        if config_dir is not None:
            options['config_dir'] = config_dir
        process, base, code = start_console(alias, **options)
        processes.append(process)
        request = browser(base)
        reply = json.loads(request('/api/login', 'POST', {'code': code})[0])
        return request, {'X-CSRF-Token': reply['csrfToken']}

    def response(request, path, method='GET', body=None, expected=200, headers=None, allow_keys=()):
        return json.loads(request(path, method, body, expected=expected, headers=headers,
                                  allowed_credentials=allow_keys)[0])

    def action(request, headers, name, target, expected=200, **fields):
        document = {'action': name, 'confirmTarget': target, **fields}
        return response(request, '/api/iam/actions', 'POST', document, expected, headers, known_access)

    def query(**values):
        return urllib.parse.urlencode(values)

    def bindings(request, kind, target):
        return response(request, '/api/iam/bindings?' + query(kind=kind, target=target), allow_keys=known_access)

    def poll_archive(request, task):
        deadline = time.monotonic() + 20
        while task['status'] in ('planning', 'running'):
            if time.monotonic() > deadline:
                raise AssertionError('archive did not finish within the fixture budget')
            time.sleep(0.02)
            task = response(request, '/api/archives/' + task['id'])
        return task

    def denied_archive(request, headers, refs):
        task = response(request, '/api/archives', 'POST', {'refs': refs}, 202, headers)
        task = poll_archive(request, task)
        assert task['status'] == 'failed' and task.get('error', {}).get('code') == 'AccessDenied', task
        request('/api/archives/' + task['id'] + '/download', expected=409)
        return task

    def archive(request, headers, document, expected_payloads):
        initial = response(request, '/api/archives', 'POST', document, 202, headers)
        task = poll_archive(request, initial)
        assert task['status'] == 'ready', task.get('error', {'status': task['status']})
        raw, headers = request('/api/archives/' + task['id'] + '/download')
        assert headers['Content-Type'] == 'application/octet-stream'
        with zipfile.ZipFile(io.BytesIO(raw)) as bundle:
            manifest = json.loads(bundle.read('manifest.json'))
            assert manifest['version'] == 1 and len(manifest['objects']) == len(expected_payloads)
            paths = [entry['archivePath'] for entry in manifest['objects']]
            assert len(paths) == len(set(paths)) and all(path.startswith('objects/') for path in paths)
            assert all('..' not in path.split('/') and '\\' not in path for path in paths)
            actual = {}
            for entry in manifest['objects']:
                assert entry['bucket'] == bucket and entry['etag']
                data = bundle.read(entry['archivePath'])
                assert len(data) == entry['size']
                actual[(entry['key'], entry.get('versionId', ''))] = data
            assert actual == expected_payloads
        return len(raw)

    try:
        writer, headers = console(writes=True)
        sharer, share_headers = console(sharing=True)
        capabilities = response(writer, '/api/capabilities')
        assert capabilities['bucketManagement'] and capabilities['archives']
        # Both state-changing capabilities are opt-in and use independent gates.
        readonly('/api/buckets/create', 'POST', {'bucket': 'readonly-must-not-exist'},
                 expected=403, headers={'X-CSRF-Token': session['csrfToken']})
        writer('/api/shares', 'POST', {'bucket': bucket, 'key': 'plain.txt'}, expected=403, headers=headers)
        created_bucket = 'features-empty-' + secrets.token_hex(4)
        writer('/api/buckets/create', 'POST', {'bucket': created_bucket}, headers=headers)
        try:
            with local_urlopen(endpoint + '/' + created_bucket + '?list-type=2', timeout=15, context=context):
                raise AssertionError('new private bucket permits anonymous listing')
        except urllib.error.HTTPError as denial:
            with denial:
                assert denial.code == 403
        writer('/api/buckets/create', 'POST', {'bucket': created_bucket}, expected=409, headers=headers)
        writer('/api/buckets/delete', 'POST', {'bucket': created_bucket, 'confirmBucket': 'wrong'}, expected=400, headers=headers)
        writer('/api/buckets/delete', 'POST', {'bucket': bucket, 'confirmBucket': bucket}, expected=409, headers=headers)
        writer('/api/buckets/delete', 'POST', {'bucket': created_bucket, 'confirmBucket': created_bucket}, headers=headers)
        checks.append('feature bucket create/private default, duplicate conflict, exact confirmation and nonempty protection')

        # Current sharing works in read-only mode only with the separate opt-in.
        share = response(sharer, '/api/shares', 'POST', {'bucket': bucket, 'key': 'plain.txt', 'expiresSeconds': 60},
                         headers=share_headers, allow_keys=[access])
        with local_urlopen(share['url'], timeout=15, context=context) as downloaded:
            assert downloaded.read() == b'console acceptance\n'
        assert urllib.parse.urlsplit(share['url']).netloc == urllib.parse.urlsplit(endpoint).netloc
        assert urllib.parse.parse_qs(urllib.parse.urlsplit(share['url']).query)['X-Amz-Expires'] == ['60']
        sharer('/api/shares', 'POST', {'bucket': bucket, 'key': 'plain.txt', 'expiresSeconds': 604801}, expected=400, headers=share_headers)
        checks.append('feature current GET bearer share on configured endpoint with bounded expiry and independent sharing gate')

        # A null version predates enabling versioning. Preserve literal keys and
        # interleaved deletion markers across one-entry history pages.
        history_key = 'features/history+ 世界.txt'
        base_payload = b'null-version-bytes'
        put_object(endpoint, bucket, history_key, base_payload, access, secret, context)
        cli('version', 'enable', 'store/' + bucket)
        versions = {}
        for payload in (b'old-version-one', b'old-version-two'):
            uri = '/' + urllib.parse.quote(bucket + '/' + history_key, safe='/-_.~')
            status, _, result_headers = signed_request(endpoint, 'PUT', uri, payload, access, secret, context)
            assert status == 200 and result_headers.get('X-Amz-Version-Id')
            versions[result_headers['X-Amz-Version-Id']] = payload
        put_object(endpoint, bucket, history_key + '.neighbor', b'neighbor must stay private', access, secret, context)
        uri = '/' + urllib.parse.quote(bucket + '/' + history_key, safe='/-_.~')
        status, _, result_headers = signed_request(endpoint, 'DELETE', uri, b'', access, secret, context)
        assert status == 204 and result_headers.get('X-Amz-Delete-Marker') == 'true'
        if legacy:
            denied = response(writer, '/api/versions?' + query(bucket=bucket, key=history_key), expected=501)
            assert denied['code'] == 'versions_unsupported'
            writer('/api/download?' + query(bucket=bucket, key=history_key, versionId='null'), expected=501)
            sharer('/api/shares', 'POST', {'bucket': bucket, 'key': history_key, 'versionId': 'null'}, expected=501, headers=share_headers)
            checks.append('legacy feature history listing/download/share refuse missing authorization capability without falling back')
        else:
            entries, cursor, cursors = [], '', set()
            history_pages = 0
            while True:
                page = response(writer, '/api/versions?' + query(bucket=bucket, key=history_key, limit=1, cursor=cursor))
                history_pages += 1
                assert len(page['entries']) <= 1 and all(entry['key'] == history_key for entry in page['entries'])
                entries.extend(page['entries'])
                cursor = page.get('nextCursor', '')
                if not cursor:
                    break
                assert cursor not in cursors, {'entries': entries, 'page': page}
                cursors.add(cursor)
            assert len(entries) == 4 and entries[0]['deleteMarker'] and entries[0]['latest'], entries
            assert {entry['versionId'] for entry in entries if not entry['deleteMarker']} == {'null', *versions}
            writer('/api/download?' + query(bucket=bucket, key=history_key, versionId=entries[0]['versionId']), expected=404)
            for version, payload in {'null': base_payload, **versions}.items():
                assert writer('/api/download?' + query(bucket=bucket, key=history_key, versionId=version))[0] == payload
            selected = next(iter(versions))
            historical_share = response(sharer, '/api/shares', 'POST', {'bucket': bucket, 'key': history_key,
                'versionId': selected, 'expiresSeconds': 60, 'downloadName': 'history.txt'}, headers=share_headers, allow_keys=[access])
            with local_urlopen(historical_share['url'], timeout=15, context=context) as download:
                assert download.read() == versions[selected] and 'attachment' in download.headers['Content-Disposition']
            assert urllib.parse.parse_qs(urllib.parse.urlsplit(historical_share['url']).query)['versionId'] == [selected]
            viewer('/api/versions?' + query(bucket=bucket, key='plain.txt'), expected=403)
            viewer('/api/download?' + query(bucket=bucket, key='plain.txt', versionId='null'), expected=403)
            history_access, history_secret = 'history-' + secrets.token_hex(6), secrets.token_hex(24)
            known_access.append(history_access)
            credentials.extend([history_access, history_secret])
            history_policy = root / 'history-only-policy.json'
            history_policy.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
                {'Effect': 'Allow', 'Action': ['s3:ListBucketVersions', 's3:GetBucketLocation'],
                 'Resource': ['arn:aws:s3:::' + bucket]},
                {'Effect': 'Allow', 'Action': ['s3:GetObjectVersion'],
                 'Resource': ['arn:aws:s3:::' + bucket + '/*']},
            ]}))
            cli('admin', 'user', 'add', 'store', history_access, history_secret)
            cli('admin', 'policy', 'add', 'store', 'console-history-only', str(history_policy))
            cli('admin', 'policy', 'set', 'store', 'console-history-only', 'user=' + history_access)
            history_config_dir = root / 'features-history-config'
            shutil.copytree(root / 'config', history_config_dir)
            history_config_path = history_config_dir / 'config.json'
            history_config = json.loads(history_config_path.read_text())
            history_config['aliases']['history-only'] = {**history_config['aliases']['store'],
                'accessKey': history_access, 'secretKey': history_secret}
            history_config_path.write_text(json.dumps(history_config))
            history_config_path.chmod(0o600)
            historian, historian_headers = console('history-only', sharing=True, config_dir=history_config_dir)
            assert response(historian, '/api/versions?' + query(bucket=bucket, key=history_key))['entries']
            assert historian('/api/download?' + query(bucket=bucket, key=history_key, versionId=selected))[0] == versions[selected]
            historian('/api/download?' + query(bucket=bucket, key='plain.txt'), expected=403)
            # Authorization and actual reads must agree on the normalized
            # version parameter. Whitespace-only IDs are current reads, not a
            # way for a historical-only identity to obtain current content.
            whitespace_version_query = urllib.parse.urlencode({'versionId': ' \t '}, quote_via=urllib.parse.quote)
            for method in ('GET', 'HEAD'):
                status, _, _ = signed_request(endpoint, method, '/' + bucket + '/plain.txt', b'',
                    history_access, history_secret, context, whitespace_version_query)
                assert status == 403, {'method': method, 'status': status}
                for current_access, current_secret in ((access, secret), (credentials[2], credentials[3])):
                    status, content, _ = signed_request(endpoint, method, '/' + bucket + '/plain.txt', b'',
                        current_access, current_secret, context, whitespace_version_query)
                    assert status == 200
                    if method == 'GET':
                        assert content == b'console acceptance\n'
            history_only_share = response(historian, '/api/shares', 'POST', {'bucket': bucket, 'key': history_key,
                'versionId': selected, 'expiresSeconds': 60}, headers=historian_headers, allow_keys=[history_access])
            with local_urlopen(history_only_share['url'], timeout=15, context=context) as download:
                assert download.read() == versions[selected]
            metrics['historyPages'] = history_pages
            metrics['historyEntries'] = len(entries)
            checks.append('feature exact-key version pages, null/version/deletion distinction, historical downloads/share and independent current-only/history-only IAM permissions')

        # Archive paths always use safe unique slots, while manifests preserve
        # Unicode object references accepted by the storage server.
        zip_key = 'features/zip/世界+ leaf.txt'
        zip_payload = b'archive literal unicode key'
        put_object(endpoint, bucket, zip_key, zip_payload, access, secret, context)
        selected_refs = [{'bucket': bucket, 'key': 'plain.txt'}, {'bucket': bucket, 'key': zip_key}]
        selected_bytes = {('plain.txt', ''): b'console acceptance\n', (zip_key, ''): zip_payload}
        if not legacy:
            selected = next(iter(versions))
            selected_refs.append({'bucket': bucket, 'key': history_key, 'versionId': selected})
            selected_bytes[(history_key, selected)] = versions[selected]
        metrics['zipBytes'] = archive(writer, headers, {'refs': selected_refs}, selected_bytes)
        if not legacy:
            # Give the current object a non-null version while preserving its
            # bytes. A current-only account must still archive it via GetObject;
            # promoting its Stat result to a version ref would wrongly deny it.
            current_payload = b'console acceptance\n'
            status, _, current_headers = signed_request(endpoint, 'PUT', '/' + bucket + '/plain.txt',
                current_payload, access, secret, context)
            assert status == 200 and current_headers.get('X-Amz-Version-Id')
            current_ref = {'bucket': bucket, 'key': 'plain.txt'}
            historical_ref = {'bucket': bucket, 'key': history_key, 'versionId': selected}
            viewer_headers = {'X-CSRF-Token': response(viewer, '/api/session')['csrfToken']}
            archive(viewer, viewer_headers, {'refs': [current_ref]}, {('plain.txt', ''): current_payload})
            # Both denied tasks start with a readable reference. The task must
            # fail as a whole rather than offer an archive of the readable subset.
            denied_archive(viewer, viewer_headers, [current_ref,
                {'bucket': bucket, 'key': 'plain.txt', 'versionId': 'null'}])
            # A subsequent successful task also proves the one archive slot was
            # released after denial. Go's mixed-read regression checks disk cleanup.
            archive(viewer, viewer_headers, {'refs': [current_ref]}, {('plain.txt', ''): current_payload})
            archive(historian, historian_headers, {'refs': [historical_ref]}, {(history_key, selected): versions[selected]})
            denied_archive(historian, historian_headers, [historical_ref, current_ref])
            archive(historian, historian_headers, {'refs': [historical_ref]}, {(history_key, selected): versions[selected]})
            metrics['zipPermissionCases'] = 4
            checks.append('feature ZIP current-only and history-only IAM reads agree with selected refs; mixed permitted/denied refs fail completely, refuse download and release the archive slot')
        prefix_payloads = {}
        for index in range(2):
            key = f'features/zip-prefix/{index}.txt'
            payload = str(index).encode()
            put_object(endpoint, bucket, key, payload, access, secret, context)
            prefix_payloads[(key, '')] = payload
        archive(writer, headers, {'bucket': bucket, 'prefix': 'features/zip-prefix/'}, prefix_payloads)
        failed = response(writer, '/api/archives', 'POST', {'refs': [{'bucket': bucket, 'key': 'must-be-absent'}]}, 202, headers)
        failed = poll_archive(writer, failed)
        assert failed['status'] == 'failed'
        writer('/api/archives/' + failed['id'] + '/download', expected=409)
        canceled = response(writer, '/api/archives', 'POST', {'refs': [{'bucket': bucket, 'key': 'stream.bin'}]}, 202, headers)
        response(writer, '/api/archives/' + canceled['id'] + '/cancel', 'POST', headers=headers)
        assert response(writer, '/api/archives/' + canceled['id'])['status'] == 'canceled'
        another, _ = console()
        another('/api/archives/' + canceled['id'], expected=404)
        checks.append('feature complete ZIP byte fidelity and reference manifest, safe unique paths with Unicode keys, prefix archive, failure refusal, cancellation and session ownership')

        # Native IAM operations and conditional direct policy bindings. Every
        # target and generated key belongs to this temporary server only.
        user = 'features-user-' + secrets.token_hex(4)
        known_access.append(user)
        if legacy:
            action(writer, headers, 'user.create', user, expected=501, user=user)
            old = bindings(writer, 'user', credentials[2])
            assert not old['conditional'] and old['revision'] == ''
            action(writer, headers, 'user.policies', credentials[2], expected=501, user=credentials[2], policies=['readonly'], revision='a' * 64)
            checks.append('legacy feature IAM mutations and binding writes refuse missing native/CAS protocol; binding view remains available')
            return metrics
        action(writer, headers, 'user.disable', access, expected=403, user=access)
        manager = 'features-admin-' + secrets.token_hex(4)
        manager_secret = secrets.token_hex(24)
        credentials.extend([manager, manager_secret])
        known_access.append(manager)
        cli('admin', 'user', 'add', 'store', manager, manager_secret)
        cli('admin', 'policy', 'set', 'store', 'consoleAdmin', 'user=' + manager)
        config_dir = root / 'features-config'
        shutil.copytree(root / 'config', config_dir)
        config_path = config_dir / 'config.json'
        config = json.loads(config_path.read_text())
        config['aliases']['feature-admin'] = {**config['aliases']['store'], 'accessKey': manager, 'secretKey': manager_secret}
        config_path.write_text(json.dumps(config))
        config_path.chmod(0o600)
        root_writer, root_headers = writer, headers
        writer, headers = console('feature-admin', writes=True, config_dir=config_dir)
        assert response(writer, '/api/self-account')['kind'] == 'iam'
        response(writer, '/api/iam/users', allow_keys=known_access)
        admin_ca = config['aliases']['store'].get('adminCAFile')
        admin_context = ssl.create_default_context(cafile=admin_ca) if admin_ca else context
        status, capability_body, capability_headers = signed_request(
            config['aliases']['store']['adminURL'], 'GET', '/otterio/admin/v3/iam-create', b'',
            manager, manager_secret, admin_context, query(kind='user', target=user))
        assert status == 200, {'status': status, 'reply': capability_body[:300]}
        assert capability_headers.get('X-Otterio-IAM-Create') == 'v1'
        assert json.loads(capability_body) == {'kind': 'user', 'target': user, 'createOnly': True}
        created = action(writer, headers, 'user.create', user, user=user)
        assert created['credentials']['accessKey'] == user and created['credentials']['secretKey']
        user_secret = created['credentials']['secretKey']
        credentials.extend([user, user_secret])
        users = response(writer, '/api/iam/users', allow_keys=known_access)['users']
        assert any(item['accessKey'] == user and item['status'] == 'enabled' for item in users)
        contender, contender_headers = console('feature-admin', writes=True, config_dir=config_dir)
        race_user = 'features-race-' + secrets.token_hex(4)
        race_secrets = [secrets.token_hex(24), secrets.token_hex(24)]
        credentials.extend([race_user, *race_secrets])
        known_access.append(race_user)
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            raced = list(executor.map(lambda index: action(
                (writer, contender)[index], (headers, contender_headers)[index],
                'user.create', race_user, expected=(200, 409), user=race_user, secretKey=race_secrets[index]), range(2)))
        winning = [index for index, result in enumerate(raced) if result.get('outcome') == 'confirmed']
        assert len(winning) == 1 and raced[1 - winning[0]]['code'] in ('user_exists', 'iam_exists')
        race_binding = bindings(writer, 'user', race_user)
        action(writer, headers, 'user.policies', race_user, user=race_user, policies=['readonly'], revision=race_binding['revision'])
        for index, race_secret in enumerate(race_secrets):
            status, content, _ = signed_request(endpoint, 'GET', '/' + bucket + '/plain.txt', b'', race_user, race_secret, context)
            assert status == (200 if index == winning[0] else 403)
            if index == winning[0]:
                assert content == b'console acceptance\n'
        race_group = 'features-race-group-' + secrets.token_hex(4)
        race_members = [[race_user], [user]]
        with concurrent.futures.ThreadPoolExecutor(max_workers=2) as executor:
            raced_groups = list(executor.map(lambda index: action(
                (writer, contender)[index], (headers, contender_headers)[index],
                'group.create', race_group, expected=(200, 409), group=race_group, members=race_members[index]), range(2)))
        winning_group = [index for index, result in enumerate(raced_groups) if result.get('outcome') == 'confirmed']
        assert len(winning_group) == 1 and raced_groups[1 - winning_group[0]]['code'] in ('group_exists', 'iam_exists')
        actual_group = next(item for item in response(writer, '/api/iam/groups', allow_keys=known_access)['groups'] if item['name'] == race_group)
        assert actual_group['members'] == race_members[winning_group[0]]
        action(writer, headers, 'group.remove-members', race_group, group=race_group, members=actual_group['members'])
        action(writer, headers, 'group.delete', race_group, group=race_group)
        action(writer, headers, 'user.delete', race_user, user=race_user)
        checks.append('feature concurrent create-only users/groups produce exactly one winner without replacing its secret or membership')
        old = bindings(writer, 'user', user)
        assert old['conditional'] and len(old['revision']) == 64
        action(writer, headers, 'user.policies', user, user=user, policies=['readonly'], revision=old['revision'])
        action(writer, headers, 'user.policies', user, expected=409, user=user, policies=['readwrite'], revision=old['revision'])
        assert bindings(writer, 'user', user)['policies'] == ['readonly']
        action(writer, headers, 'user.disable', user, user=user)
        disabled_secret = secrets.token_hex(24)
        credentials.append(disabled_secret)
        action(writer, headers, 'user.rotate', user, user=user, secretKey=disabled_secret)
        disabled_users = response(writer, '/api/iam/users', allow_keys=known_access)['users']
        assert next(item for item in disabled_users if item['accessKey'] == user)['status'] == 'disabled'
        status, _, _ = signed_request(endpoint, 'GET', '/' + bucket + '/plain.txt', b'', user, disabled_secret, context)
        assert status == 403
        action(writer, headers, 'user.enable', user, user=user)
        replacement_secret = secrets.token_hex(24)
        credentials.append(replacement_secret)
        before_rotation = bindings(writer, 'user', user)
        action(writer, headers, 'user.rotate', user, user=user, secretKey=replacement_secret)
        action(writer, headers, 'user.policies', user, expected=409, user=user, policies=['readonly'], revision=before_rotation['revision'])
        group = 'features-group-' + secrets.token_hex(4)
        action(writer, headers, 'group.create', group, group=group)
        before_membership = bindings(writer, 'group', group)
        action(writer, headers, 'group.add-members', group, group=group, members=[user])
        action(writer, headers, 'group.policies', group, expected=409, group=group, policies=['readonly'], revision=before_membership['revision'])
        action(writer, headers, 'group.delete', group, expected=409, group=group)
        action(writer, headers, 'user.delete', user, expected=409, user=user)
        old_group = bindings(writer, 'group', group)
        action(writer, headers, 'group.policies', group, group=group, policies=['readonly'], revision=old_group['revision'])
        action(writer, headers, 'group.policies', group, expected=409, group=group, policies=[], revision=old_group['revision'])
        action(writer, headers, 'group.disable', group, group=group)
        action(writer, headers, 'group.enable', group, group=group)
        service = action(writer, headers, 'service-account.create', user, user=user)['credentials']
        service_key = service['accessKey']
        known_access.append(service_key)
        credentials.extend([service_key, service['secretKey']])
        accounts = response(writer, '/api/iam/service-accounts?' + query(user=user), allow_keys=known_access)['serviceAccounts']
        assert any(item['accessKey'] == service_key and item['parentUser'] == user for item in accounts)
        def service_read(secret_key, object_key='plain.txt', expected=200):
            status, content, _ = signed_request(endpoint, 'GET', '/' + bucket + '/' + object_key,
                b'', service_key, secret_key, context)
            assert status == expected, {'object': object_key, 'status': status, 'expected': expected}
            if expected == 200 and object_key == 'plain.txt':
                assert content == b'console acceptance\n'
        service_read(service['secretKey'])
        service_read(service['secretKey'], 'empty.txt')
        response(root_writer, '/api/iam/service-accounts?' + query(user=user), expected=403, allow_keys=known_access)
        action(root_writer, root_headers, 'service-account.disable', service_key, expected=403, accessKey=service_key)
        action(writer, headers, 'user.delete', user, expected=409, user=user)
        action(writer, headers, 'service-account.disable', service_key, accessKey=service_key)
        service_read(service['secretKey'], expected=403)
        action(writer, headers, 'service-account.enable', service_key, accessKey=service_key)
        service_read(service['secretKey'])
        service_secret = secrets.token_hex(24)
        credentials.append(service_secret)
        action(writer, headers, 'service-account.rotate', service_key, accessKey=service_key, secretKey=service_secret)
        service_read(service['secretKey'], expected=403)
        service_read(service_secret)
        subpolicy = json.dumps({'Version': '2012-10-17', 'Statement': [{'Effect': 'Allow', 'Action': ['s3:GetObject'], 'Resource': ['arn:aws:s3:::' + bucket + '/plain.txt']}]})
        action(writer, headers, 'service-account.policy', service_key, accessKey=service_key, policy=subpolicy)
        service_read(service_secret)
        service_read(service_secret, 'empty.txt', expected=403)
        action(writer, headers, 'service-account.delete', service_key, accessKey=service_key)
        service_read(service_secret, expected=403)
        action(writer, headers, 'group.remove-members', group, group=group, members=[user])
        action(writer, headers, 'group.delete', group, group=group)
        action(writer, headers, 'user.delete', user, user=user)
        viewer('/api/iam/users', expected=403)
        checks.append('feature native users/groups/service accounts lifecycle and actual credential/policy effects through ordinary consoleAdmin identity, root restrictions, one-time keys, parent protection and policy-binding CAS')
        return metrics
    finally:
        for process in reversed(processes):
            stop(process)
