#!/usr/bin/env python3
"""Exercise an authenticated console object route against a real server.

The caller owns temporary accounts, fixture upload, TLS trust and cleanup.
Credentials are supplied via environment, never command-line arguments.
Fixture object must contain the exact bytes supplied in --fixture.
"""
import argparse
import hashlib
import http.cookiejar
import json
import os
import ssl
import urllib.error
import urllib.request


def main():
    parser = argparse.ArgumentParser()
    parser.add_argument('--console-url', required=True)
    parser.add_argument('--object-path', required=True)
    parser.add_argument('--fixture', required=True)
    parser.add_argument('--ca')
    args = parser.parse_args()
    with open(args.fixture, 'rb') as fixture:
        data = fixture.read()
    if len(data) < 4:
        parser.error('fixture must contain at least four bytes')
    opener = urllib.request.build_opener(
        urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()),
        urllib.request.HTTPSHandler(context=ssl.create_default_context(cafile=args.ca)))
    def request(path, method='GET', headers=None, body=None):
        req = urllib.request.Request(args.console_url + path, data=body, method=method, headers={'Origin': args.console_url, **(headers or {})})
        try:
            response = opener.open(req, timeout=30)
        except urllib.error.HTTPError as error:
            response = error
        with response:
            return response.status, response.headers, response.read()
    login = {'accessKey': os.environ['OC_RANGE_ACCESS_KEY'], 'secretKey': os.environ['OC_RANGE_SECRET_KEY']}
    status, _, _ = request('/api/login', 'POST', {'Content-Type': 'application/json'}, json.dumps(login).encode())
    assert status == 200, ('login', status)
    path = args.object_path
    status, headers, body = request(path)
    assert status == 200 and hashlib.sha256(body).digest() == hashlib.sha256(data).digest()
    # Conditional route may be required to obtain a metadata-derived ETag.
    status, headers, body = request(path, headers={'Range': 'bytes=0-1'})
    assert status == 206 and body == data[:2]
    assert headers['Content-Range'] == f'bytes 0-1/{len(data)}'
    etag = headers['ETag']
    for value, expected, interval in [
        ('bytes=1-3', data[1:4], f'bytes 1-3/{len(data)}'),
        ('bytes=-3', data[-3:], f'bytes {len(data)-3}-{len(data)-1}/{len(data)}'),
        (f'bytes={len(data)-2}-', data[-2:], f'bytes {len(data)-2}-{len(data)-1}/{len(data)}')]:
        status, headers, body = request(path, headers={'Range': value})
        assert status == 206 and body == expected and headers['Content-Range'] == interval
        assert int(headers['Content-Length']) == len(expected)
    status, headers, body = request(path, headers={'Range': f'bytes={len(data)}-'})
    assert status == 416 and headers['Content-Range'] == f'bytes */{len(data)}'
    status, _, body = request(path, headers={'If-None-Match': etag})
    assert status == 304 and not body
    status, _, body = request(path, headers={'Range': 'bytes=1-3', 'If-Range': '"different"'})
    assert status == 200 and body == data
    status, headers, body = request(path, 'HEAD', {'Range': 'bytes=1-3'})
    assert status == 200 and not body and int(headers['Content-Length']) == len(data)
    print(json.dumps({'checks': 9, 'fixtureBytes': len(data), 'sha256': hashlib.sha256(data).hexdigest()}))


if __name__ == '__main__':
    main()
