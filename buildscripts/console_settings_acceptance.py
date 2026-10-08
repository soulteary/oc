"""P3 acceptance on owned temporary accounts and storage, without retries."""
import hashlib
import json
import secrets
import time
import urllib.parse
import xml.etree.ElementTree as ET


def verify_settings(root, bucket, cli, readonly, session, viewer, start_console, browser, stop,
                    credentials, endpoint, access, secret, context, signed_request, checks, legacy=False):
    processes = []

    def console(alias):
        process, base, code = start_console(alias, writes=True)
        processes.append(process)
        request = browser(base)
        reply = json.loads(request('/api/login', 'POST', {'code': code})[0])
        headers = {'X-CSRF-Token': reply['csrfToken']}
        return process, request, headers

    def get(request, kind, target=bucket):
        query = urllib.parse.urlencode({'bucket': target, 'kind': kind})
        return json.loads(request('/api/bucket-settings?' + query)[0])

    def save(request, headers, setting, doc='', remove=False, expected=200):
        return json.loads(request('/api/bucket-settings', 'POST', {
            'bucket': setting['bucket'], 'kind': setting['kind'], 'document': doc,
            'revision': setting['revision'], 'remove': remove, 'confirm': True,
        }, expected=expected, headers=headers)[0])

    try:
        _, writer, headers = console('store')
        settings = [get(readonly, kind) for kind in ('policy', 'versioning', 'lifecycle')]
        for item in settings:
            assert item['bucket'] == bucket and item['format'] in ('json', 'xml')
            assert item['conditional'] is (not legacy)
            assert bool(item['revision']) is (not legacy)
            if not legacy:
                assert len(item['revision']) == 64 and not item['exists']
        self_info = json.loads(readonly('/api/self-account')[0])
        assert set(self_info) == {'kind', 'status', 'canRotateSecret'}
        assert not self_info['canRotateSecret']
        readonly('/api/self-secret', 'POST', {'newSecret': 'never-committed-secret', 'confirm': True},
                 expected=403, headers={'X-CSRF-Token': session['csrfToken']})
        checks.append('P3 bounded complete document reads, exact revision/exists and credential-free own identity')
        if legacy:
            assert self_info['kind'] == 'unknown'
            missing = {**settings[0], 'revision': 'a' * 64}
            save(writer, headers, missing, '{}', expected=501)
            writer('/api/self-secret', 'POST', {'newSecret': 'never-committed-secret', 'confirm': True}, expected=501, headers=headers)
            assert not get(readonly, 'policy')['exists']
            checks.append('P3 older server denies unprotected configuration writes and secret changes')
            # The pinned legacy server does not implement the native target-list
            # route. Inspect only our owned fixture's persisted bucket metadata.
            disks = sorted(path for path in root.glob('data*') if path.is_dir())
            assert disks and all((disk / bucket).is_dir() for disk in disks)

            def metadata_snapshot():
                snapshot = {}
                for disk in disks:
                    directory = disk / '.otterio.sys' / 'buckets' / bucket
                    for path in sorted(directory.rglob('*')):
                        if path.is_file():
                            snapshot[str(path.relative_to(root))] = hashlib.sha256(path.read_bytes()).hexdigest()
                assert snapshot, 'owned legacy fixture has no persisted bucket metadata'
                return snapshot

            before_targets = metadata_snapshot()
            target = urllib.parse.urlsplit(endpoint)
            target_url = f'{target.scheme}://{access}:{secret}@{target.netloc}/{bucket}'
            try:
                cli('--json', 'admin', 'bucket', 'remote', 'add', 'store/' + bucket, target_url,
                    '--service', 'ilm', '--label', 'LEGACY-MUST-NOT-WRITE', '--path', 'on')
            except RuntimeError as error:
                assert 'does not confirm lifecycle transition v1 support' in str(error)
            else:
                raise AssertionError('legacy server accepted a lifecycle target mutation')
            assert metadata_snapshot() == before_targets, 'refused target mutation changed persisted metadata'
            checks.append('new CLI refuses legacy ILM target mutation; owned persisted bucket metadata is byte-for-byte unchanged')
            return {'profile': 'legacy-read-only-fallback', 'unchangedMetadataFiles': len(before_targets)}
        assert self_info['kind'] == 'root' and self_info['status'] == 'enabled'
        writer('/api/self-secret', 'POST', {'newSecret': 'never-committed-secret', 'confirm': True}, expected=501, headers=headers)
        writer('/api/bucket-settings', 'POST', {}, expected=403)
        checks.append('P3 readonly/CSRF gates and root self rotation denial')

        # Keep the bucket private; deny an unused fixture prefix and retain SID.
        def policy_doc(sid):
            return json.dumps({'Version': '2012-10-17', 'Statement': [{
                'Sid': sid, 'Effect': 'Deny', 'Principal': {'AWS': ['*']},
                'Action': ['s3:GetObject'], 'Resource': ['arn:aws:s3:::' + bucket + '/policy-fixture/*'],
            }]})

        old_policy, old_version, old_lifecycle = settings
        changed = save(writer, headers, old_policy, policy_doc('retain-first'))
        assert changed['exists'] and changed['revision'] != old_policy['revision']
        assert json.loads(changed['document'])['Statement'][0]['Sid'] == 'retain-first'
        assert get(readonly, 'policy')['document'] == changed['document']
        unsupported_policy = json.loads(policy_doc('must-not-discard-field'))
        unsupported_policy['Statement'][0]['UnimplementedExtension'] = True
        save(writer, headers, changed, json.dumps(unsupported_policy), expected=400)
        assert get(readonly, 'policy')['revision'] == changed['revision']
        # Signed bytes must not silently become U+FFFD inside a valid policy.
        for malformed in (b'\xff', b'\\ud800', b'\\udc00'):
            wire = policy_doc('encoding-fixture').encode().replace(b'encoding-fixture', malformed)
            status, _, _ = signed_request(endpoint, 'PUT', '/' + bucket + '/', wire,
                access, secret, context, query='policy=', extra_headers={
                    'X-Otterio-Config-If-Match': changed['revision'], 'Content-Type': 'application/json'})
            assert status == 400
            assert get(readonly, 'policy')['revision'] == changed['revision']
        checks.append('P3 signed policy rejects malformed Unicode without changing persisted revision')
        conflict = save(writer, headers, old_policy, policy_doc('stale-must-not-write'), expected=409)
        assert conflict['code'] == 'setting_conflict'
        assert json.loads(get(readonly, 'policy')['document'])['Statement'][0]['Sid'] == 'retain-first'
        absent = save(writer, headers, changed, remove=True)
        assert not absent['exists'] and absent['revision'] == old_policy['revision']
        checks.append('P3 policy replace/delete CAS, stale rejection and persisted value integrity')
        xml_header = ' xmlns="http://s3.amazonaws.com/doc/2006-03-01/"'
        version_doc = '<VersioningConfiguration' + xml_header + '><Status>Enabled</Status></VersioningConfiguration>'
        enabled = save(writer, headers, old_version, version_doc)
        assert ET.fromstring(enabled['document']).findtext('{*}Status') == 'Enabled'
        assert get(readonly, 'versioning')['document'] == enabled['document']
        unsupported = '<VersioningConfiguration' + xml_header + '><Status>Enabled</Status><MFADelete>Enabled</MFADelete></VersioningConfiguration>'
        save(writer, headers, enabled, unsupported, expected=400)
        assert get(readonly, 'versioning')['revision'] == enabled['revision']
        suspended_doc = '<VersioningConfiguration' + xml_header + '><Status>Suspended</Status></VersioningConfiguration>'
        save(writer, headers, old_version, suspended_doc, expected=409)
        assert ET.fromstring(get(readonly, 'versioning')['document']).findtext('{*}Status') == 'Enabled'
        lifecycle_doc = '<LifecycleConfiguration' + xml_header + '><Rule><ID>fixture-disabled</ID><Status>Disabled</Status><Filter><Prefix>never-expire/</Prefix></Filter><Expiration><Days>36500</Days></Expiration></Rule></LifecycleConfiguration>'
        configured = save(writer, headers, old_lifecycle, lifecycle_doc)
        assert configured['exists'] and ET.fromstring(configured['document']).findtext('{*}Rule/{*}ID') == 'fixture-disabled'
        assert get(readonly, 'lifecycle')['document'] == configured['document']
        unsupported_lifecycle = lifecycle_doc.replace('</Expiration>', '<ExpiredObjectAllVersions>true</ExpiredObjectAllVersions></Expiration>')
        save(writer, headers, configured, unsupported_lifecycle, expected=400)
        assert get(readonly, 'lifecycle')['revision'] == configured['revision']
        widened_filter = lifecycle_doc.replace('<Prefix>never-expire/</Prefix>', '<Tag><Key></Key><Value>prod</Value></Tag>')
        save(writer, headers, configured, widened_filter, expected=400)
        assert get(readonly, 'lifecycle')['revision'] == configured['revision']
        save(writer, headers, old_lifecycle, lifecycle_doc, expected=409)
        # Valid tag filters remain supported for current/noncurrent expiration.
        # Disable owned fixtures so acceptance never schedules real deletion.
        tag_filter = '<Tag><Key>environment + 世界</Key><Value>prod % +</Value></Tag>'
        for action in ('<Expiration><Days>36500</Days></Expiration>',
                       '<NoncurrentVersionExpiration><NoncurrentDays>36500</NoncurrentDays></NoncurrentVersionExpiration>'):
            tagged = '<LifecycleConfiguration' + xml_header + '><Rule><ID>tagged-disabled</ID><Status>Disabled</Status><Filter>' + tag_filter + '</Filter>' + action + '</Rule></LifecycleConfiguration>'
            configured = save(writer, headers, configured, tagged)
            assert ET.fromstring(configured['document']).findtext('{*}Rule/{*}Filter/{*}Tag/{*}Key') == 'environment + 世界'
            assert get(readonly, 'lifecycle')['revision'] == configured['revision']
        marker = '<LifecycleConfiguration' + xml_header + '><Rule><Status>Disabled</Status><Filter>' + tag_filter + '</Filter><Expiration><ExpiredObjectDeleteMarker>true</ExpiredObjectDeleteMarker></Expiration></Rule></LifecycleConfiguration>'
        save(writer, headers, configured, marker, expected=400)
        assert get(readonly, 'lifecycle')['revision'] == configured['revision']
        noncurrent_transition = '<LifecycleConfiguration' + xml_header + '><Rule><Status>Disabled</Status><Filter>' + tag_filter + '</Filter><NoncurrentVersionTransition><NoncurrentDays>36500</NoncurrentDays><StorageClass>archive</StorageClass></NoncurrentVersionTransition></Rule></LifecycleConfiguration>'
        save(writer, headers, configured, noncurrent_transition, expected=404)
        assert get(readonly, 'lifecycle')['revision'] == configured['revision']
        assert not save(writer, headers, configured, remove=True)['exists']
        checks.append('P3 tagged lifecycle expiration preserves Unicode filters; tagged delete markers and unconfigured noncurrent targets rejected without commit')
        checks.append('P3 version/lifecycle independent revisions, conflict, actual saved XML round trip and unsupported fields rejected without commit')

        # The restricted account may self-rotate, but cannot edit bucket policy.
        _, ordinary, ordinary_headers = console('viewer')
        own = json.loads(ordinary('/api/self-account')[0])
        assert own['kind'] == 'iam' and own['canRotateSecret']
        ordinary('/api/bucket-settings?' + urllib.parse.urlencode({'bucket': bucket, 'kind': 'policy'}), expected=403)
        save(ordinary, ordinary_headers, old_policy, policy_doc('unauthorized'), expected=403)
        assert not get(readonly, 'policy')['exists']
        checks.append('P3 ordinary IAM self capability with settings authorization enforced by S3')

        # Add one IAM identity with no ListAllMyBuckets. Its group grants only
        # bucket settings, so exact settings remain reachable when listing fails.
        user, user_secret = 'settings-' + secrets.token_hex(6), secrets.token_hex(24)
        credentials.extend([user, user_secret])
        cli('admin', 'user', 'add', 'store', user, user_secret)
        policy_path = root / 'settings-permissions.json'
        policy_path.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [{
            'Effect': 'Allow', 'Action': ['s3:GetBucketLocation', 's3:GetBucketPolicy', 's3:PutBucketPolicy',
                's3:GetBucketVersioning', 's3:PutBucketVersioning', 's3:GetLifecycleConfiguration', 's3:PutLifecycleConfiguration'],
            'Resource': ['arn:aws:s3:::' + bucket],
        }]}))
        cli('admin', 'policy', 'add', 'store', 'console-settings', str(policy_path))
        cli('admin', 'group', 'add', 'store', 'console-settings-group', user)
        cli('admin', 'policy', 'set', 'store', 'console-settings', 'group=console-settings-group')
        cfg_path = root / 'config' / 'config.json'
        cfg = json.loads(cfg_path.read_text())
        cfg['aliases']['settings'] = {**cfg['aliases']['store'], 'accessKey': user, 'secretKey': user_secret}
        original_cfg = cfg_path.read_bytes()
        cfg_path.write_text(json.dumps(cfg))
        try:
            proc, scoped, scoped_headers = console('settings')
            # ListBuckets omits buckets without ListBucket; either no visible
            # bucket or explicit denial demonstrates settings independence.
            listed = json.loads(scoped('/api/buckets', expected=None)[0])
            assert listed.get('buckets', []) == [] or listed.get('code') == 'AccessDenied'
            scoped_setting = get(scoped, 'policy')
            changed = save(scoped, scoped_headers, scoped_setting, policy_doc('group-retained'))
            assert changed['exists']
            save(writer, headers, changed, remove=True)
            checks.append('P3 explicit bucket setting access without bucket enumeration via inherited group policy')
            # Explicit Deny on self creation overrides implicit self permission.
            deny_path = root / 'deny-self-rotation.json'
            deny_path.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
                {'Effect': 'Deny', 'Action': ['admin:CreateUser']},
            ]}))
            cli('admin', 'policy', 'add', 'store', 'console-deny-self', str(deny_path))
            cli('admin', 'policy', 'set', 'store', 'console-deny-self', 'user=' + user)
            assert not json.loads(scoped('/api/self-account')[0])['canRotateSecret']
            scoped('/api/self-secret', 'POST', {'newSecret': 'never-committed-secret', 'confirm': True}, expected=501, headers=scoped_headers)
            cli('admin', 'policy', 'unset', 'store', 'console-deny-self', 'user=' + user)
            checks.append('P3 explicit self deny disables own-secret mutation without disabling the identity')

            # Real STS discovery must not inherit the parent's rotation rights.
            sts_body = urllib.parse.urlencode({'Action': 'AssumeRole', 'Version': '2011-06-15', 'DurationSeconds': '900'}).encode()
            status, payload, _ = signed_request(endpoint, 'POST', '/', sts_body, user, user_secret, context,
                                                 extra_headers={'Content-Type': 'application/x-www-form-urlencoded'}, service='sts')
            assert status == 200
            minted = ET.fromstring(payload).find('.//{*}Credentials')
            temporary = {**cfg['aliases']['settings'], 'accessKey': minted.findtext('{*}AccessKeyId'),
                         'secretKey': minted.findtext('{*}SecretAccessKey'), 'sessionToken': minted.findtext('{*}SessionToken')}
            assert all(temporary[field] for field in ('accessKey', 'secretKey', 'sessionToken'))
            credentials.extend(temporary[field] for field in ('accessKey', 'secretKey', 'sessionToken'))
            cfg['aliases']['temporary'] = temporary
            cfg_path.write_text(json.dumps(cfg))
            _, temporary_browser, temporary_headers = console('temporary')
            sts_identity = json.loads(temporary_browser('/api/self-account')[0])
            assert sts_identity['kind'] == 'sts' and not sts_identity['canRotateSecret']
            temporary_browser('/api/self-secret', 'POST', {'newSecret': 'never-committed-secret', 'confirm': True},
                              expected=501, headers=temporary_headers)
            checks.append('P3 real STS selected token, own identity classification and zero secret mutation')

            # Pending jobs from both sessions are revoked when identity retires.
            second = browser(scoped.base)
            # The printed code stays in the owned log; do not reuse the first
            # session token to fabricate a second session.
            fields = dict(line.split(': ', 1) for line in (root / 'settings-writer-console.log').read_text().splitlines() if ': ' in line)
            second_session = json.loads(second('/api/login', 'POST', {'code': fields['Login code']})[0])
            second('/api/uploads', 'POST', {'bucket': bucket, 'key': 'pending-after-rotation', 'size': 0},
                   expected=201, headers={'X-CSRF-Token': second_session['csrfToken']})
            new_secret = secrets.token_hex(24)
            credentials.append(new_secret)
            started = time.monotonic()
            result = json.loads(scoped('/api/self-secret', 'POST', {'newSecret': new_secret, 'confirm': True}, headers=scoped_headers)[0])
            assert result == {'restartRequired': True, 'outcome': 'confirmed'}
            proc.wait(timeout=5)
            shutdown_seconds = time.monotonic() - started
            assert proc.returncode == 0 and shutdown_seconds < 5
            assert cfg_path.read_text() == json.dumps(cfg)
            # Selected old key fails; new key remains IAM enabled with group policy.
            uri = '/' + bucket + '/'
            old_status, _, _ = signed_request(endpoint, 'GET', uri, b'', user, user_secret, context, query='policy=')
            assert old_status == 403
            status, payload, response_headers = signed_request(endpoint, 'GET', uri, b'', user, new_secret, context, query='versioning=')
            assert status == 200 and ET.fromstring(payload).findtext('{*}Status') == 'Enabled'
            assert response_headers['X-Otterio-Bucket-Config'] == 'v1'
            cli('admin', 'group', 'disable', 'store', 'console-settings-group')
            status, _, _ = signed_request(endpoint, 'GET', uri, b'', user, new_secret, context, query='versioning=')
            assert status == 403
            cli('admin', 'group', 'enable', 'store', 'console-settings-group')
            checks.append('P3 own secret rotation acknowledged before process exit, old key rejected, new group policy intact, local alias unchanged')
        finally:
            cfg_path.write_bytes(original_cfg)
        return {'profile': 'v1-protected-settings', 'rotationShutdownSeconds': shutdown_seconds}
    finally:
        for process in reversed(processes):
            stop(process)
