"""Real-process S3 copy authorization, using only isolated fixture data."""
import json
import secrets
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

from local_http import local_urlopen


def verify_version_copy(root, bucket, cli, credentials, endpoint, access, secret, context,
                        signed_request, put_object, checks):
    suffix = secrets.token_hex(4)
    source_bucket, destination_bucket = 'copy-source-' + suffix, 'copy-target-' + suffix
    cli('mb', 'store/' + source_bucket)
    cli('mb', 'store/' + destination_bucket)
    # A destination version query must pass destination validation before
    # it can test whether that query is improperly used for source policy.
    cli('version', 'enable', 'store/' + destination_bucket)
    source_key = 'source+ 世界.txt'
    payloads = {'null': b'copy-null-source-bytes'}
    put_object(endpoint, source_bucket, source_key, payloads['null'], access, secret, context)
    cli('version', 'enable', 'store/' + source_bucket)

    def uri(selected_bucket, key=''):
        return '/' + urllib.parse.quote(selected_bucket + ('/' + key if key else ''), safe='/-_.~')

    def query(**values):
        return urllib.parse.urlencode(sorted(values.items()), quote_via=urllib.parse.quote)

    def root_request(method, selected_bucket, key='', payload=b'', parameters='', headers=None):
        return signed_request(endpoint, method, uri(selected_bucket, key), payload,
                              access, secret, context, parameters, headers)

    versions = []
    for data, environment in ((b'copy-old-source-bytes', 'dev'), (b'copy-current-source-bytes', 'prod')):
        status, _, headers = root_request('PUT', source_bucket, source_key, data,
                                         headers={'X-Amz-Tagging': 'environment=' + environment})
        assert status == 200 and headers.get('X-Amz-Version-Id')
        versions.append(headers['X-Amz-Version-Id'])
        payloads[versions[-1]] = data
    old_version, current_version = versions

    def source_header(version):
        return uri(source_bucket, source_key) + ('?' + query(versionId=version) if version is not None else '')

    identities = {}
    profiles = {
        'current': (['s3:GetObject'], None),
        'history': (['s3:GetObjectVersion'], None),
        'both': (['s3:GetObject', 's3:GetObjectVersion'], None),
        'deny-history': (['s3:GetObject', 's3:GetObjectVersion'], 's3:GetObjectVersion'),
        'condition': (['s3:GetObjectVersion'], None),
        'destination-denied': (['s3:GetObject', 's3:GetObjectVersion'], None),
        'tag-history': (['s3:GetObjectVersion'], None),
        'tag-current': (['s3:GetObject'], None),
    }
    for profile, (actions, denial) in profiles.items():
        user, user_secret = 'copy-' + profile + '-' + suffix, secrets.token_hex(24)
        credentials.extend((user, user_secret))
        statement = {'Effect': 'Allow', 'Action': actions,
                     'Resource': ['arn:aws:s3:::' + source_bucket + '/' + source_key]}
        if profile == 'condition':
            statement['Condition'] = {'StringEquals': {'s3:versionid': old_version}}
        if profile in ('tag-history', 'tag-current'):
            statement['Condition'] = {'StringEquals': {'s3:ExistingObjectTag/environment': 'prod'}}
        statements = [statement]
        if profile != 'destination-denied':
            statements.append({'Effect': 'Allow', 'Action': ['s3:PutObject'],
                               'Resource': ['arn:aws:s3:::' + destination_bucket + '/*']})
        if denial:
            statements.append({'Effect': 'Deny', 'Action': [denial],
                               'Resource': ['arn:aws:s3:::' + source_bucket + '/*']})
        path = root / ('copy-' + profile + '.json')
        path.write_text(json.dumps({'Version': '2012-10-17', 'Statement': statements}))
        cli('admin', 'user', 'add', 'store', user, user_secret)
        cli('admin', 'policy', 'add', 'store', 'copy-' + profile + '-' + suffix, str(path))
        cli('admin', 'policy', 'set', 'store', 'copy-' + profile + '-' + suffix, 'user=' + user)
        identities[profile] = (user, user_secret)

    requests, successful, denied = 0, 0, 0

    def copy_case(operation, profile, version, expected, expected_bytes=None, destination_query='',
                  extra_headers=None, anonymous=False):
        nonlocal requests, successful, denied
        target = f'copied-{requests:03d}.txt'
        requests += 1
        headers = {'X-Amz-Copy-Source': source_header(version), **(extra_headers or {})}
        upload_id = None
        if operation == 'part':
            status, document, _ = root_request('POST', destination_bucket, target, parameters='uploads=')
            assert status == 200
            upload_id = ET.fromstring(document).findtext('{*}UploadId')
            assert upload_id
        parameters = query(partNumber='1', uploadId=upload_id) if upload_id else ''
        if destination_query:
            parameters += ('&' if parameters else '') + destination_query
            parameters = '&'.join(sorted(parameters.split('&')))
        try:
            if anonymous:
                request = urllib.request.Request(endpoint + uri(destination_bucket, target) +
                    ('?' + parameters if parameters else ''), data=b'', method='PUT', headers=headers)
                try:
                    reply = local_urlopen(request, timeout=20, context=context)
                except urllib.error.HTTPError as error:
                    reply = error
                with reply:
                    status, document = reply.status, reply.read()
            else:
                user, user_secret = identities[profile]
                status, document, _ = signed_request(endpoint, 'PUT', uri(destination_bucket, target), b'',
                    user, user_secret, context, parameters, headers)
            error_code = ET.fromstring(document).findtext('{*}Code')
            assert status == expected, {'operation': operation, 'profile': profile, 'expected': expected,
                                        'actual': status, 'code': error_code}
            if expected == 403:
                assert ET.fromstring(document).findtext('{*}Code') == 'AccessDenied'
                if upload_id:
                    status, listed, _ = root_request('GET', destination_bucket, target,
                        parameters=query(uploadId=upload_id))
                    assert status == 200 and not ET.fromstring(listed).findall('{*}Part'), 'denied copy wrote a multipart part'
                status, _, _ = root_request('GET', destination_bucket, target)
                assert status == 404, 'denied copy published destination bytes'
                denied += 1
                return
            result = ET.fromstring(document)
            assert result.tag.rsplit('}', 1)[-1] == ('CopyPartResult' if upload_id else 'CopyObjectResult')
            etag = result.findtext('{*}ETag')
            assert etag
            if upload_id:
                complete = ET.Element('CompleteMultipartUpload')
                part = ET.SubElement(complete, 'Part')
                ET.SubElement(part, 'PartNumber').text = '1'
                ET.SubElement(part, 'ETag').text = etag
                status, completed, _ = root_request('POST', destination_bucket, target, ET.tostring(complete),
                    query(uploadId=upload_id), {'Content-Type': 'application/xml'})
                assert status == 200 and ET.fromstring(completed).tag.rsplit('}', 1)[-1] == 'CompleteMultipartUploadResult'
                upload_id = None
            status, copied, _ = root_request('GET', destination_bucket, target)
            assert status == 200 and copied == expected_bytes, 'copied bytes differ from selected source version'
            successful += 1
        finally:
            if upload_id:
                status, _, _ = root_request('DELETE', destination_bucket, target, parameters=query(uploadId=upload_id))
                assert status == 204

    references = [(None, False, payloads[current_version]), ('', False, payloads[current_version]),
                  (' \t ', False, payloads[current_version]), (old_version, True, payloads[old_version]),
                  ('null', True, payloads['null']), (' ' + old_version + ' ', True, payloads[old_version])]
    for operation in ('object', 'part'):
        for profile in profiles:
            for version, historical, data in references:
                allowed = profile == 'both' or (profile == 'current' and not historical) or (
                    profile == 'history' and historical) or (profile == 'deny-history' and not historical) or (
                    profile == 'condition' and historical and version.strip() == old_version) or (
                    profile == 'tag-current' and not historical)
                copy_case(operation, profile, version, 200 if allowed else 403, data)
        # The source condition uses the source reference, even if the target
        # request has a different version query or an injected Versionid header.
        copy_case(operation, 'condition', current_version, 403,
                  destination_query=query(versionId=old_version), extra_headers={'Versionid': old_version})
        copy_case(operation, 'condition', current_version, 403,
                  extra_headers={'Versionid': old_version})
        copy_case(operation, 'history', None, 403, destination_query=query(versionId=old_version))
        copy_case(operation, 'tag-history', old_version, 403, extra_headers={'X-Amz-Tagging': 'environment=prod'})
        copy_case(operation, 'tag-history', old_version, 403,
                  destination_query=query(**{'ExistingObjectTag/environment': 'prod'}))
        copy_case(operation, 'tag-history', 'null', 403,
                  destination_query=query(**{'ExistingObjectTag/environment': 'prod'}),
                  extra_headers={'X-Amz-Tagging': 'environment=prod'})
        copy_case(operation, 'tag-history', old_version, 403,
                  extra_headers={'X-Amz-Copy-Source-If-Match': '"wrong-source-etag"'})
        copy_case(operation, 'tag-history', current_version, 200, payloads[current_version])
        copy_case(operation, 'tag-history', current_version, 200, payloads[current_version],
                  extra_headers={'X-Amz-Tagging': 'environment=dev'})
        copy_case(operation, 'tag-current', None, 200, payloads[current_version],
                  extra_headers={'X-Amz-Tagging': 'environment=dev'})
        checks.append('real ' + ('CopyObject' if operation == 'object' else 'UploadPartCopy') +
                      ' source-only current/history/deny/version-and-stored-tag conditions, null and whitespace normalization, forged request tags/query conditions, authorization before source preconditions, target isolation and exact copied bytes')

    def anonymous_policy(selected_bucket, actions, resource):
        document = json.dumps({'Version': '2012-10-17', 'Statement': [{'Effect': 'Allow',
            'Principal': {'AWS': ['*']}, 'Action': actions, 'Resource': [resource]}]}).encode()
        status, _, _ = root_request('PUT', selected_bucket, payload=document, parameters='policy=',
                                    headers={'Content-Type': 'application/json'})
        assert status == 204

    anonymous_policy(destination_bucket, ['s3:PutObject'], 'arn:aws:s3:::' + destination_bucket + '/*')
    for actions, current_status, historical_status in [(['s3:GetObjectVersion'], 403, 200),
                                                      (['s3:GetObject'], 200, 403)]:
        anonymous_policy(source_bucket, actions, 'arn:aws:s3:::' + source_bucket + '/' + source_key)
        for operation in ('object', 'part'):
            copy_case(operation, 'anonymous', None, current_status, payloads[current_version], anonymous=True)
            copy_case(operation, 'anonymous', old_version, historical_status, payloads[old_version], anonymous=True)
    checks.append('real anonymous bucket-policy copy authorization distinguishes current and historical source versions for both copy operations')
    # These access keys are private fixture inputs to the disclosure scanner.
    # Remove their users and equally named policies before other profiles list
    # IAM records; otherwise legitimate access-key labels look like a leak.
    for profile, (user, _) in identities.items():
        cli('admin', 'user', 'remove', 'store', user)
        cli('admin', 'policy', 'remove', 'store', 'copy-' + profile + '-' + suffix)
    return {'profile': 'copy-source-version-authorization', 'requests': requests,
            'confirmedCopies': successful, 'accessDeniedWithoutDestination': denied,
            'signatures': ['SigV4', 'anonymous'], 'storage': 'four-disk-erasure'}
