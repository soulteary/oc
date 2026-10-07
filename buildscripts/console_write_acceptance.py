"""Write acceptance helpers; invoked only inside the disposable console fixture."""
import http.client
import datetime
import json
import shutil
import statistics
import subprocess
import threading
import time
import urllib.parse
import xml.etree.ElementTree as ET


def verify_writes(root, bucket, cli, base, request, session, viewer, viewer_session,
                  endpoint, access, secret, context, signed_request, put_object, checks,
                  start_console, browser, stop, credentials, console_process, metrics=None):
    metrics = {} if metrics is None else metrics
    assert session['readOnly'] is False and session['maxUploadSize'] == 1 << 30
    csrf = {'X-CSRF-Token': session['csrfToken']}
    viewer_csrf = {'X-CSRF-Token': viewer_session['csrfToken']}

    def job(path, method='GET', body=None, user=request, headers=csrf, expected=200):
        return json.loads(user(path, method, body, headers=headers, expected=expected)[0])

    def await_terminal(identifier, user=request):
        deadline = time.monotonic() + 15
        while time.monotonic() < deadline:
            result = job('/api/jobs/' + identifier, user=user)
            if result['status'] in ('succeeded', 'failed', 'partial', 'canceled'):
                return result
            time.sleep(0.05)
        raise RuntimeError('write job did not reach a terminal state')

    def upload(key, payload, overwrite=False, user=request, headers=csrf, target=bucket):
        created = job('/api/uploads', 'POST', {'bucket': target, 'key': key, 'size': len(payload), 'overwrite': overwrite},
                      user=user, headers=headers, expected=201)
        assert created['kind'] == 'upload' and created['status'] == 'waiting'
        return job('/api/uploads/' + created['id'], 'PUT', payload, user=user, headers=headers)

    def plan(keys=None, prefix=None, user=request, headers=csrf, target=bucket):
        body = {'bucket': target}
        body['keys' if keys is not None else 'prefix'] = keys if keys is not None else prefix
        return job('/api/deletions/plan', 'POST', body, user=user, headers=headers)

    def execute(planned, user=request, headers=csrf):
        assert planned['status'] == 'ready' and planned['confirmToken']
        job('/api/deletions/' + planned['id'] + '/execute', 'POST', {'confirmToken': planned['confirmToken']},
            user=user, headers=headers, expected=202)
        return await_terminal(planned['id'], user=user)

    def download(key, expected=200, target=bucket):
        return request('/api/download?' + urllib.parse.urlencode({'bucket': target, 'key': key}), expected=expected)[0]

    request('/api/uploads', 'POST', {'bucket': bucket, 'key': 'csrf-denied', 'size': 1}, expected=403)
    request('/api/deletions/plan', 'POST', {'bucket': bucket, 'keys': ['plain.txt']}, expected=403)
    request('/api/uploads', 'POST', {'bucket': bucket, 'key': 'too-large', 'size': (1 << 30) + 1},
            expected=400, headers=csrf)
    created = job('/api/uploads', 'POST', {'bucket': bucket, 'key': 'not-started', 'size': 1}, expected=201)
    viewer('/api/jobs/' + created['id'], expected=404)
    result = job('/api/jobs/' + created['id'] + '/cancel', 'POST', {})
    assert result['status'] == 'canceled'
    request('/api/uploads/' + created['id'], 'PUT', b'x', expected=409, headers=csrf)
    checks.append('default readonly, CSRF, upload limits, job ownership and canceled-job replay')

    samples = {'write/空 文件+%.txt': '写入摘要验证'.encode(), 'write/empty': b'',
               'write/multipart.bin': b'abcdefghijklmnop' * (1024 * 1024 + 1)}
    for key, payload in samples.items():
        result = upload(key, payload)
        assert result['status'] == 'succeeded', result
        assert result['transferred'] == len(payload) and result['size'] == len(payload) and result['etag']
        assert download(key) == payload
    collision = upload('write/空 文件+%.txt', b'replacement')
    assert collision['status'] == 'failed' and collision['error']['code'] == 'object_exists', collision
    assert download('write/空 文件+%.txt') == samples['write/空 文件+%.txt']
    assert upload('write/空 文件+%.txt', b'explicit replacement', overwrite=True)['status'] == 'succeeded'
    assert download('write/空 文件+%.txt') == b'explicit replacement'
    checks.append('zero/special/multipart uploads, byte fidelity and explicit overwrite')

    # The HTTP service must enforce the condition at commit, not merely HEAD.
    uri = '/' + bucket + '/write/direct-condition'
    status, _, headers = signed_request(endpoint, 'PUT', uri, b'first', access, secret, context,
                                         extra_headers={'If-None-Match': '*'})
    assert status == 200 and headers['X-Otterio-Conditional-Writes'] == 'v1'
    status, _, _ = signed_request(endpoint, 'PUT', uri, b'second', access, secret, context,
                                  extra_headers={'If-None-Match': '*'})
    assert status == 412 and download('write/direct-condition') == b'first'
    checks.append('real server conditional PUT preserves current data with HTTP 412')

    for key in ('write/delete/a', 'write/delete/sub/b'):
        assert upload(key, b'delete me')['status'] == 'succeeded'
    planned = plan(prefix='write/delete/')
    assert planned['count'] == 2 and {item['key'] for item in planned['items']} == {'write/delete/a', 'write/delete/sub/b'}
    status_copy = job('/api/jobs/' + planned['id'])
    assert 'confirmToken' not in status_copy
    assert upload('write/delete/created-after-plan', b'keep new key')['status'] == 'succeeded'
    request('/api/deletions/' + planned['id'] + '/execute', 'POST', {'confirmToken': 'wrong'}, expected=403, headers=csrf)
    result = execute(planned)
    assert result['status'] == 'succeeded' and result['completed'] == 2
    assert all(item['status'] == 'succeeded' for item in result['items'])
    request('/api/deletions/' + planned['id'] + '/execute', 'POST', {'confirmToken': planned['confirmToken']},
            expected=409, headers=csrf)
    assert download('write/delete/created-after-plan') == b'keep new key'
    download('write/delete/a', expected=404)
    request('/api/deletions/plan', 'POST', {'bucket': bucket, 'prefix': ''}, expected=400, headers=csrf)
    request('/api/deletions/plan', 'POST', {'bucket': bucket, 'keys': ['plain.txt', 'plain.txt']}, expected=400, headers=csrf)
    checks.append('flat prefix snapshot, exact confirmation, token secrecy/replay and new-key preservation')

    # Policy changes happen upstream; the console must never infer per-key writes
    # from the informational bucket-root AccountInfo booleans.
    policy_path = root / 'viewer-write-policy.json'
    policy_path.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
        {'Effect': 'Allow', 'Action': ['s3:GetBucketLocation'], 'Resource': ['arn:aws:s3:::' + bucket]},
        {'Effect': 'Allow', 'Action': ['s3:ListBucket'], 'Resource': ['arn:aws:s3:::' + bucket],
         'Condition': {'StringLike': {'s3:prefix': ['allowed/', 'allowed/*']}}},
        {'Effect': 'Allow', 'Action': ['s3:GetObject', 's3:PutObject', 's3:DeleteObject'],
         'Resource': ['arn:aws:s3:::' + bucket + '/allowed/*']},
        {'Effect': 'Deny', 'Action': ['s3:PutObject', 's3:DeleteObject'],
         'Resource': ['arn:aws:s3:::' + bucket + '/allowed/denied*']},
    ]}))
    cli('admin', 'policy', 'add', 'store', 'console-viewer-write', str(policy_path))
    cli('admin', 'policy', 'set', 'store', 'console-viewer-write', 'user=' + session_alias_access(root))
    allowed = upload('allowed/uploaded', b'scoped write', user=viewer, headers=viewer_csrf)
    assert allowed['status'] == 'succeeded', allowed
    permitted_plan = plan(prefix='allowed/', user=viewer, headers=viewer_csrf)
    assert permitted_plan['status'] == 'ready' and permitted_plan['count'] == 1 and permitted_plan['items'][0]['key'] == 'allowed/uploaded'
    assert job('/api/jobs/' + permitted_plan['id'] + '/cancel', 'POST', {}, user=viewer, headers=viewer_csrf)['status'] == 'canceled'
    denied = upload('allowed/denied-put', b'denied', overwrite=True, user=viewer, headers=viewer_csrf)
    assert denied['status'] == 'failed' and denied['error']['code'] in ('AccessDenied', 'access_denied'), denied
    denied_plan = plan(prefix='write/', user=viewer, headers=viewer_csrf)
    assert denied_plan['status'] == 'failed' and not denied_plan.get('confirmToken'), denied_plan
    put_object(endpoint, bucket, 'allowed/denied-delete', b'keep denied', access, secret, context)
    mixed = execute(plan(keys=['allowed/uploaded', 'allowed/denied-delete'], user=viewer, headers=viewer_csrf),
                    user=viewer, headers=viewer_csrf)
    assert mixed['status'] == 'partial' and [item['status'] for item in mixed['items']] == ['succeeded', 'failed'], mixed
    assert download('allowed/denied-delete') == b'keep denied'
    checks.append('prefix-scoped identity, explicit deny, failed-list zero deletion and batch partial failures')

    # Use an existing user's group policy and then a write-only policy, covering
    # inheritance and operations that legitimately do not require ListBucket.
    cli('admin', 'group', 'add', 'store', 'console-writers', session_alias_access(root))
    cli('admin', 'policy', 'set', 'store', 'console-viewer-write', 'group=console-writers')
    cli('admin', 'policy', 'unset', 'store', 'console-viewer-write', 'user=' + session_alias_access(root))
    assert upload('allowed/group-upload', b'group inherited', user=viewer, headers=viewer_csrf)['status'] == 'succeeded'
    cli('admin', 'group', 'disable', 'store', 'console-writers')
    assert upload('allowed/group-disabled', b'denied', overwrite=True, user=viewer, headers=viewer_csrf)['status'] == 'failed'
    cli('admin', 'group', 'enable', 'store', 'console-writers')
    # Temporary credentials are minted as a regular group user, then narrowed
    # by a session policy. Keep their config separate from the CLI fixture.
    config = json.loads((root / 'config' / 'config.json').read_text())
    parent = config['aliases']['viewer']
    session_policy = {'Version': '2012-10-17', 'Statement': [
        {'Effect': 'Allow', 'Action': ['s3:GetBucketLocation'], 'Resource': ['arn:aws:s3:::' + bucket]},
        {'Effect': 'Allow', 'Action': ['s3:GetObject', 's3:PutObject', 's3:DeleteObject'],
         'Resource': ['arn:aws:s3:::' + bucket + '/allowed/sts']},
    ]}
    form = urllib.parse.urlencode({'Action': 'AssumeRole', 'Version': '2011-06-15', 'DurationSeconds': 900,
                                  'Policy': json.dumps(session_policy, separators=(',', ':'))}).encode()
    status, payload, _ = signed_request(endpoint, 'POST', '/', form, parent['accessKey'], parent['secretKey'], context,
                                        extra_headers={'Content-Type': 'application/x-www-form-urlencoded'}, service='sts')
    assert status == 200, ('STS fixture did not issue temporary credentials', status)
    parsed = ET.fromstring(payload)
    temp_creds = parsed.find('.//{*}Credentials')
    temporary = {**parent, 'accessKey': temp_creds.findtext('{*}AccessKeyId'),
                 'secretKey': temp_creds.findtext('{*}SecretAccessKey'), 'sessionToken': temp_creds.findtext('{*}SessionToken')}
    assert all(temporary[field] for field in ('accessKey', 'secretKey', 'sessionToken'))
    credentials.extend(temporary[field] for field in ('accessKey', 'secretKey', 'sessionToken'))
    sts_dir = root / 'temporary-config'
    sts_dir.mkdir()
    if (root / 'config' / 'certs').exists():
        shutil.copytree(root / 'config' / 'certs', sts_dir / 'certs')
    invalid = {**temporary, 'sessionToken': temporary['sessionToken'] + 'x'}
    (sts_dir / 'config.json').write_text(json.dumps({'version': '10', 'aliases': {'temporary': temporary, 'invalid': invalid}}))
    sts_process = invalid_process = None
    try:
        sts_process, sts_base, sts_code = start_console('temporary', writes=True, config_dir=sts_dir)
        sts_user = browser(sts_base)
        sts_session = json.loads(sts_user('/api/login', 'POST', {'code': sts_code})[0])
        sts_csrf = {'X-CSRF-Token': sts_session['csrfToken']}
        allowed = upload('allowed/sts', b'session allowed', user=sts_user, headers=sts_csrf)
        assert allowed['status'] == 'succeeded', allowed
        denied = upload('allowed/outside-session', b'session denied', overwrite=True, user=sts_user, headers=sts_csrf)
        assert denied['status'] == 'failed', denied
        assert execute(plan(keys=['allowed/sts'], user=sts_user, headers=sts_csrf), user=sts_user, headers=sts_csrf)['status'] == 'succeeded'
        invalid_process, invalid_base, invalid_code = start_console('invalid', writes=True, config_dir=sts_dir)
        invalid_user = browser(invalid_base)
        invalid_session = json.loads(invalid_user('/api/login', 'POST', {'code': invalid_code})[0])
        denied = upload('allowed/sts', b'bad token', overwrite=True, user=invalid_user,
                        headers={'X-CSRF-Token': invalid_session['csrfToken']})
        assert denied['status'] == 'failed', denied
    finally:
        stop(invalid_process)
        stop(sts_process)
    checks.append('STS session-policy intersection, selected session token and invalid-token denial')
    write_only_path = root / 'write-only-policy.json'
    write_only_path.write_text(json.dumps({'Version': '2012-10-17', 'Statement': [
        {'Effect': 'Allow', 'Action': ['s3:GetBucketLocation'], 'Resource': ['arn:aws:s3:::' + bucket]},
        {'Effect': 'Allow', 'Action': ['s3:PutObject', 's3:DeleteObject'],
         'Resource': ['arn:aws:s3:::' + bucket + '/write-only/*']},
    ]}))
    cli('admin', 'policy', 'add', 'store', 'console-write-only', str(write_only_path))
    cli('admin', 'policy', 'set', 'store', 'console-write-only', 'group=console-writers')
    assert upload('write-only/file', b'without listing', overwrite=True, user=viewer, headers=viewer_csrf)['status'] == 'succeeded'
    assert execute(plan(keys=['write-only/file'], user=viewer, headers=viewer_csrf), user=viewer,
                   headers=viewer_csrf)['status'] == 'succeeded'
    assert plan(prefix='write-only/', user=viewer, headers=viewer_csrf)['status'] == 'failed'
    checks.append('group policy inheritance/revocation and write-only exact operations without list permission')

    version_bucket = 'console-versioned'
    cli('mb', 'store/' + version_bucket)
    cli('version', 'enable', 'store/' + version_bucket)
    assert upload('retained', b'original version', target=version_bucket)['status'] == 'succeeded'
    assert execute(plan(keys=['retained'], target=version_bucket))['status'] == 'succeeded'
    download('retained', expected=404, target=version_bucket)
    uri = '/' + version_bucket + '/'
    status, payload, _ = signed_request(endpoint, 'GET', uri, b'', access, secret, context, query='versions=')
    assert status == 200
    parsed = ET.fromstring(payload)
    versions = parsed.findall('{*}Version')
    markers = parsed.findall('{*}DeleteMarker')
    assert len(versions) == 1 and len(markers) == 1
    assert versions[0].findtext('{*}Key') == 'retained'
    version = versions[0].findtext('{*}VersionId')
    status, original, _ = signed_request(endpoint, 'GET', '/' + version_bucket + '/retained', b'', access, secret, context,
                                          query='versionId=' + urllib.parse.quote(version, safe=''))
    assert status == 200 and original == b'original version'
    assert upload('retained', b'after marker', target=version_bucket)['status'] == 'succeeded'
    assert download('retained', target=version_bucket) == b'after marker'
    checks.append('versioned deletion creates a marker, preserves old data and permits create after marker')

    locked_bucket = 'console-locked'
    cli('mb', '--with-lock', 'store/' + locked_bucket)
    retain_until = (datetime.datetime.now(datetime.timezone.utc) + datetime.timedelta(days=1)).strftime('%Y-%m-%dT%H:%M:%SZ')
    status, _, _ = signed_request(endpoint, 'PUT', '/' + locked_bucket + '/locked', b'locked version', access, secret, context,
                                  extra_headers={'X-Amz-Object-Lock-Mode': 'COMPLIANCE', 'X-Amz-Object-Lock-Retain-Until-Date': retain_until})
    assert status == 200
    # Current-key deletion is a marker and is allowed even with retained versions;
    # the UI never requests permanent versions or bypass governance headers.
    assert execute(plan(keys=['locked'], target=locked_bucket))['status'] == 'succeeded'
    status, payload, _ = signed_request(endpoint, 'GET', '/' + locked_bucket + '/', b'', access, secret, context, query='versions=')
    locked_versions = ET.fromstring(payload).findall('{*}Version')
    assert status == 200 and len(locked_versions) == 1
    version = locked_versions[0].findtext('{*}VersionId')
    query = 'retention=&versionId=' + urllib.parse.quote(version, safe='')
    status, payload, _ = signed_request(endpoint, 'GET', '/' + locked_bucket + '/locked', b'', access, secret, context, query=query)
    retention = ET.fromstring(payload)
    assert status == 200 and retention.findtext('{*}Mode') == 'COMPLIANCE'
    assert datetime.datetime.fromisoformat(retention.findtext('{*}RetainUntilDate').replace('Z', '+00:00')) == datetime.datetime.fromisoformat(retain_until.replace('Z', '+00:00'))
    version_query = 'versionId=' + urllib.parse.quote(version, safe='')
    status, original, _ = signed_request(endpoint, 'GET', '/' + locked_bucket + '/locked', b'', access, secret, context, query=version_query)
    assert status == 200 and original == b'locked version'
    status, retention_error, _ = signed_request(endpoint, 'DELETE', '/' + locked_bucket + '/locked', b'', access, secret, context, query=version_query)
    # This pinned OtterIO API represents WORM rejection as 400 InvalidRequest.
    # Verify the protected bytes again rather than treating any 4xx as proof.
    assert status == 400 and ET.fromstring(retention_error).findtext('{*}Code') == 'InvalidRequest', {'permanentRetainedDeleteStatus': status}
    status, original, _ = signed_request(endpoint, 'GET', '/' + locked_bucket + '/locked', b'', access, secret, context, query=version_query)
    assert status == 200 and original == b'locked version'
    checks.append('object-lock bucket current-key deletion retains versions without bypass or version ID')

    # Cancellation must abort only the multipart ID created by this upload.
    foreign_key = 'write/cancel-owned'
    foreign_uri = '/' + bucket + '/' + foreign_key
    status, payload, _ = signed_request(endpoint, 'POST', foreign_uri, b'', access, secret, context, query='uploads=')
    assert status == 200
    foreign_id = ET.fromstring(payload).findtext('{*}UploadId')
    assert foreign_id
    # OtterIO's erasure implementation lists incomplete sessions for an exact
    # object prefix; an unprefixed bucket list is not evidence of cleanup.
    multipart_query = 'prefix=' + urllib.parse.quote(foreign_key, safe='') + '&uploads='
    status, payload, _ = signed_request(endpoint, 'GET', '/' + bucket + '/', b'', access, secret, context, query=multipart_query)
    assert status == 200 and [item.findtext('{*}UploadId') for item in ET.fromstring(payload).findall('{*}Upload')] == [foreign_id]
    created = job('/api/uploads', 'POST', {'bucket': bucket, 'key': foreign_key, 'size': 33 << 20}, expected=201)
    transfer_errors = []

    def slow_sender():
        url = urllib.parse.urlsplit(base)
        connection = http.client.HTTPConnection(url.hostname, url.port, timeout=10)
        try:
            connection.putrequest('PUT', '/api/uploads/' + created['id'])
            for header, value in {'Origin': base, 'X-CSRF-Token': session['csrfToken'],
                                  'Cookie': '; '.join(cookie.name + '=' + cookie.value for cookie in request.jar),
                                  'Content-Type': 'application/octet-stream', 'Content-Length': str(33 << 20)}.items():
                connection.putheader(header, value)
            connection.endheaders()
            for _ in range(17):
                connection.send(b'x' * (1 << 20))
            response = connection.getresponse()
            response.read()
        except (OSError, http.client.HTTPException) as error:
            transfer_errors.append(type(error).__name__)
        finally:
            connection.close()

    sender = threading.Thread(target=slow_sender, daemon=True)
    sender.start()
    try:
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            running = job('/api/jobs/' + created['id'])
            if running['transferred'] >= 16 << 20:
                break
            time.sleep(0.05)
        else:
            raise RuntimeError('slow upload never acknowledged its first multipart chunk')
        started = time.monotonic()
        job('/api/jobs/' + created['id'] + '/cancel', 'POST', {})
        canceled = await_terminal(created['id'])
        sender.join(timeout=5)
        cancellation_seconds = time.monotonic() - started
        assert not sender.is_alive() and cancellation_seconds < 5
        assert canceled['status'] == 'canceled'
        status, payload, _ = signed_request(endpoint, 'GET', '/' + bucket + '/', b'', access, secret, context, query=multipart_query)
        assert status == 200
        incomplete = ET.fromstring(payload).findall('{*}Upload')
        observed = [item.findtext('{*}UploadId') for item in incomplete]
        foreign_status, foreign_parts, _ = signed_request(endpoint, 'GET', foreign_uri, b'', access, secret, context,
                                                          query=urllib.parse.urlencode({'uploadId': foreign_id}))
        assert foreign_status == 200 and ET.fromstring(foreign_parts).findtext('{*}UploadId') == foreign_id
        assert observed == [foreign_id], {'unfinishedCount': len(observed), 'foreignPreserved': foreign_id in observed,
                                          'foreignPartsStatus': foreign_status, 'jobError': canceled.get('error'),
                                          'listRoot': ET.fromstring(payload).tag,
                                          'keys': [item.findtext('{*}Key') for item in incomplete]}
        download(foreign_key, expected=404)
    finally:
        signed_request(endpoint, 'DELETE', foreign_uri, b'', access, secret, context,
                       query=urllib.parse.urlencode({'uploadId': foreign_id}))
    checks.append('slow multipart cancellation within 5s, owned cleanup and unrelated-upload preservation')

    # Alternate three identical-size CLI/console trials: sub-second transfers
    # are noisy, so keep every observation and gate their medians, never best-of.
    # Include browser job registration and final acknowledgement in each time.
    large = b'0123456789abcdef' * (65 * 1024 * 1024 // 16)
    large_path = root / 'transfer-65m.bin'
    large_path.write_bytes(large)
    cli_seconds, console_seconds, download_seconds, single_windows = [], [], [], []
    samples_rss = []
    sampler_errors = []
    sample_stop = threading.Event()

    def sample_rss():
        while not sample_stop.is_set():
            try:
                measured = subprocess.run(['ps', '-o', 'rss=', '-p', str(console_process.pid)], capture_output=True, timeout=2)
                if measured.returncode != 0 or not measured.stdout.strip():
                    raise RuntimeError('RSS process sampling failed')
                samples_rss.append((time.monotonic(), int(measured.stdout.strip()) / 1024))
            except Exception as error:
                sampler_errors.append(type(error).__name__)
                return
            sample_stop.wait(0.1)

    sampler = threading.Thread(target=sample_rss, daemon=True)
    sampler.start()
    timings, outcomes = {}, []
    try:
        for trial in range(3):
            started = time.monotonic()
            cli('cp', str(large_path), f'store/{bucket}/write/cli-reference-{trial}.bin')
            cli_seconds.append(time.monotonic() - started)
            key = f'write/performance-single-{trial}.bin'
            started = time.monotonic()
            result = upload(key, large)
            console_seconds.append(time.monotonic() - started)
            single_windows.append((started, time.monotonic()))
            assert result['status'] == 'succeeded', result
            started = time.monotonic()
            assert download(key) == large
            download_seconds.append(time.monotonic() - started)
        baseline_seconds = statistics.median(cli_seconds)
        timings['singleUploadSeconds'] = statistics.median(console_seconds)
        timings['singleDownloadSeconds'] = statistics.median(download_seconds)

        def concurrent_upload(key):
            try:
                outcomes.append(upload(key, large))
            except BaseException as error:
                outcomes.append(error)

        transfers = [threading.Thread(target=concurrent_upload, args=(f'write/performance-parallel-{i}.bin',), daemon=True) for i in range(2)]
        started = time.monotonic()
        for transfer in transfers:
            transfer.start()
        for transfer in transfers:
            transfer.join(timeout=max(0, started + 120 - time.monotonic()))
        timings['twoConcurrentUploadSeconds'] = time.monotonic() - started
        concurrent_window = (started, time.monotonic())
        assert all(not transfer.is_alive() for transfer in transfers)
        assert len(outcomes) == 2 and all(isinstance(item, dict) and item['status'] == 'succeeded' for item in outcomes), outcomes
        for i in range(2):
            assert download(f'write/performance-parallel-{i}.bin') == large
    finally:
        sample_stop.set()
        sampler.join(timeout=3)
        # Preserve measured evidence even when a transfer or sampler fails.
        metrics.update({**timings, 'throughputAggregation': 'median-of-three-alternating-trials',
                        'cliUploadTrialsSeconds': cli_seconds, 'consoleUploadTrialsSeconds': console_seconds,
                        'downloadTrialsSeconds': download_seconds, 'samplerErrors': sampler_errors,
                        'rssSampleCount': len(samples_rss), 'rssSampleIntervalSeconds': 0.1,
                        'cancellationSeconds': cancellation_seconds,
                        'objectSizeMiB': 65, 'maximumWriteConcurrency': 2,
                        'minimumConsoleToCLIRatio': 0.5, 'minimumUploadMiBPerSecond': 5})
        if samples_rss:
            metrics['peakConsoleRSSMiB'] = max(value for _, value in samples_rss)
        if cli_seconds:
            metrics['cliUploadSeconds'] = statistics.median(cli_seconds)
            metrics['cliMiBPerSecond'] = 65 / metrics['cliUploadSeconds']
        if console_seconds:
            metrics['singleUploadSeconds'] = statistics.median(console_seconds)
            metrics['consoleMiBPerSecond'] = 65 / metrics['singleUploadSeconds']
    assert not sampler.is_alive() and not sampler_errors, sampler_errors
    assert samples_rss, 'no console RSS samples'
    assert all(any(begin <= stamp <= end for stamp, _ in samples_rss) for begin, end in [*single_windows, concurrent_window]), 'no samples during one transfer phase'
    peak_rss = max(value for _, value in samples_rss)
    rate = 65 / timings['singleUploadSeconds']
    reference_rate = 65 / baseline_seconds
    assert peak_rss <= 512 and all(value <= 120 for value in [*timings.values(), *cli_seconds, *console_seconds, *download_seconds])
    assert rate >= 5 and rate >= reference_rate * 0.5, {'consoleMiBPerSecond': rate, 'cliMiBPerSecond': reference_rate}
    checks.append('65MiB byte fidelity, two concurrent uploads, sampled RSS and three-trial median same-host CLI throughput budget')
    jobs = job('/api/jobs')['jobs']
    assert len(jobs) <= 16 and all('confirmToken' not in item for item in jobs)

    # Stop a real console while it is blocked receiving the second part, then
    # query the still-running S3 server for owned cleanup and foreign survival.
    foreign_key = 'write/shutdown-owned'
    foreign_uri = '/' + bucket + '/' + foreign_key
    status, payload, _ = signed_request(endpoint, 'POST', foreign_uri, b'', access, secret, context, query='uploads=')
    assert status == 200
    foreign_id = ET.fromstring(payload).findtext('{*}UploadId')
    created = job('/api/uploads', 'POST', {'bucket': bucket, 'key': foreign_key, 'size': 33 << 20}, expected=201)
    sender = threading.Thread(target=slow_sender, daemon=True)
    sender.start()
    try:
        deadline = time.monotonic() + 10
        while time.monotonic() < deadline:
            if job('/api/jobs/' + created['id'])['transferred'] >= 16 << 20:
                break
            time.sleep(0.05)
        else:
            raise RuntimeError('shutdown upload never acknowledged its first part')
        started = time.monotonic()
        stop(console_process)
        sender.join(timeout=5)
        write_shutdown_seconds = time.monotonic() - started
        assert console_process.returncode == 0 and not sender.is_alive() and write_shutdown_seconds < 5
        multipart_query = 'prefix=' + urllib.parse.quote(foreign_key, safe='') + '&uploads='
        status, payload, _ = signed_request(endpoint, 'GET', '/' + bucket + '/', b'', access, secret, context, query=multipart_query)
        assert status == 200 and [item.findtext('{*}UploadId') for item in ET.fromstring(payload).findall('{*}Upload')] == [foreign_id]
        status, _, _ = signed_request(endpoint, 'GET', foreign_uri, b'', access, secret, context)
        assert status == 404
    finally:
        signed_request(endpoint, 'DELETE', foreign_uri, b'', access, secret, context,
                       query=urllib.parse.urlencode({'uploadId': foreign_id}))
    checks.append('active-write process shutdown, interrupted browser socket and owned multipart cleanup before exit')
    metrics['activeWriteShutdownSeconds'] = write_shutdown_seconds
    return metrics


def session_alias_access(root):
    return json.loads((root / 'config' / 'config.json').read_text())['aliases']['viewer']['accessKey']
