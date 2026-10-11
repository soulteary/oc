#!/usr/bin/env python3
"""Disposable HTTPS Keycloak/OtterIO STS and optional Console acceptance.

Never reads existing OC configuration; credentials, keys and logs stay in a
private temporary directory and are deleted after containers/processes stop.
Only structural results, hashes and fixed image identity enter the report.
"""
import argparse
import base64
import datetime
import hashlib
import html.parser
import http.cookiejar
import importlib.util
import json
import os
from pathlib import Path
import secrets
import socket
import ssl
import subprocess
import tempfile
import time
import urllib.error
import urllib.parse
import urllib.request
import xml.etree.ElementTree as ET

ROOT = Path(__file__).resolve().parents[1]
KEYCLOAK_IMAGE = 'quay.io/keycloak/keycloak:26.8.0@sha256:27b3230fbabb8c9ff3c66c864cf09dcd8a41aca82e5100679d109ef0eeb05741'


def free_port():
    with socket.socket() as sock:
        sock.bind(('127.0.0.1', 0))
        return sock.getsockname()[1]


def run(command, **kwargs):
    try:
        result = subprocess.run(command, capture_output=True, timeout=kwargs.pop('timeout', 60), **kwargs)
    except subprocess.TimeoutExpired:
        raise RuntimeError('fixture subprocess deadline exceeded') from None
    if result.returncode:
        raise RuntimeError('fixture subprocess failed: '+Path(command[0]).name+' '+command[1])
    return result.stdout


def keycloak_refresh_events(container):
    import re
    try:
        result = subprocess.run(['docker', 'logs', container], capture_output=True, timeout=15)
    except subprocess.TimeoutExpired:
        raise RuntimeError('Keycloak event observation deadline exceeded') from None
    if result.returncode:
        raise RuntimeError('Keycloak event observation failed')
    protected_log = (result.stdout+result.stderr).decode(errors='replace')
    return len(re.findall(r'type="?REFRESH_TOKEN"?[, ]', protected_log))


def generate_certificate(root, directory, name):
    directory.mkdir()
    ca = root/(name+'-ca.pem')
    ca_key = root/(name+'-ca.key')
    run(['openssl', 'req', '-x509', '-newkey', 'rsa:2048', '-nodes', '-days', '1',
         '-keyout', str(ca_key), '-out', str(ca), '-subj', '/CN=OC temporary '+name+' CA',
         '-addext', 'basicConstraints=critical,CA:TRUE', '-addext', 'keyUsage=critical,keyCertSign,cRLSign'])
    run(['openssl', 'req', '-new', '-newkey', 'rsa:2048', '-nodes', '-keyout', str(directory/'private.key'),
         '-out', str(root/(name+'.csr')), '-subj', '/CN=localhost'])
    extensions = root/(name+'-extensions')
    extensions.write_text('subjectAltName=IP:127.0.0.1,DNS:localhost\nbasicConstraints=critical,CA:FALSE\nextendedKeyUsage=serverAuth\n')
    run(['openssl', 'x509', '-req', '-in', str(root/(name+'.csr')), '-CA', str(ca),
         '-CAkey', str(ca_key), '-CAcreateserial', '-days', '1', '-extfile', str(extensions),
         '-out', str(directory/'public.crt')])
    return ca


def program_identity(path, declared_source):
    import re
    if not re.fullmatch(r'[0-9a-f]{40}', declared_source):
        raise RuntimeError('program source must be a full commit hash')
    metadata = run(['go', 'version', '-m', path]).decode()
    settings = {}
    for line in metadata.splitlines():
        line = line.strip()
        if line.startswith('build\t') and '=' in line:
            key, value = line[6:].split('=', 1)
            settings[key] = value
    if settings.get('vcs.revision') != declared_source or settings.get('vcs.modified') != 'false':
        raise RuntimeError('program build metadata does not match clean declared source')
    return {'sha256': hashlib.sha256(Path(path).read_bytes()).hexdigest(),
            'declaredSource': declared_source, 'binaryVCSRevision': settings['vcs.revision'],
            'binaryVCSModified': False, 'platform': settings.get('GOOS', '')+'/'+settings.get('GOARCH', '')}


def realm_config(callback, passwords, client_secret=None):
    return {'realm': 'oc-acceptance', 'enabled': True, 'sslRequired': 'all',
            'eventsEnabled': True, 'eventsListeners': ['jboss-logging'],
            'defaultSignatureAlgorithm': 'RS256', 'accessTokenLifespan': 90,
            'clients': [{'clientId': 'oc-console', 'enabled': True, 'publicClient': client_secret is None, 'secret': client_secret,
                         'standardFlowEnabled': True, 'directAccessGrantsEnabled': False,
                         'redirectUris': [callback], 'webOrigins': [],
                         'attributes': {'pkce.code.challenge.method': 'S256', 'id.token.signed.response.alg': 'RS256'},
                         'defaultClientScopes': ['profile', 'email'], 'optionalClientScopes': ['offline_access'],
                         'protocolMappers': [{'name': 'storage-policy', 'protocol': 'openid-connect',
                                              'protocolMapper': 'oidc-usermodel-attribute-mapper',
                                              'config': {'user.attribute': 'policy', 'claim.name': 'policy',
                                                         'jsonType.label': 'String', 'id.token.claim': 'true',
                                                         'access.token.claim': 'true', 'multivalued': 'false'}}]}],
            'users': [{'username': name, 'enabled': True, 'emailVerified': True,
                       'realmRoles': ['offline_access'], 'email': name+'@fixture.invalid', 'firstName': name, 'lastName': 'Fixture',
                       'attributes': {'policy': [name+'-read']},
                       'credentials': [{'type': 'password', 'value': password, 'temporary': False}]}
                      for name, password in passwords.items()]}


def check_discovery(document, issuer, auth_method="none"):
    if document.get('issuer') != issuer:
        raise RuntimeError('discovery issuer mismatch')
    origin = urllib.parse.urlsplit(issuer)
    for key in ('authorization_endpoint', 'token_endpoint', 'jwks_uri'):
        endpoint = urllib.parse.urlsplit(document.get(key, ''))
        if endpoint.scheme != 'https' or endpoint.netloc != origin.netloc:
            raise RuntimeError('discovery endpoint outside fixed HTTPS origin')
    for key, value in [('id_token_signing_alg_values_supported', 'RS256'),
                       ('response_types_supported', 'code'), ('code_challenge_methods_supported', 'S256'),
                       ('token_endpoint_auth_methods_supported', auth_method)]:
        if value not in document.get(key, []):
            raise RuntimeError('discovery lacks required strict profile field: '+key)


def check_refresh_claims(initial, refreshed, nonce):
    if refreshed.get('sub') != initial.get('sub') or not initial.get('sub'):
        raise RuntimeError('refresh subject changed')
    for key in ('iss', 'aud'):
        if refreshed.get(key) != initial.get(key):
            raise RuntimeError('refresh issuer or audience changed')
    if 'nonce' in refreshed and refreshed['nonce'] != nonce:
        raise RuntimeError('refresh nonce changed')
    if refreshed.get('iat', 0) < initial.get('iat', 0):
        raise RuntimeError('refresh issuance time moved backwards')
    return {'nonceAbsentAllowed': 'nonce' not in refreshed,
            'nonceMatchesWhenPresent': 'nonce' not in refreshed or refreshed['nonce'] == nonce,
            'subjectPreserved': True, 'issuerAudiencePreserved': True}


class LoginForm(html.parser.HTMLParser):
    def __init__(self):
        super().__init__()
        self.action = None
        self.fields = {}
        self.active = False

    def handle_starttag(self, tag, attrs):
        attrs = dict(attrs)
        if tag == 'form' and attrs.get('id') == 'kc-form-login':
            self.action = attrs.get('action')
            self.active = True
        if tag == 'input' and self.active and attrs.get('name') and attrs.get('type') == 'hidden':
            self.fields[attrs['name']] = attrs.get('value', '')

    def handle_endtag(self, tag):
        if tag == 'form':
            self.active = False


class CaptureCallback(urllib.request.HTTPRedirectHandler):
    def __init__(self, callback):
        self.callback = callback

    def redirect_request(self, request, fp, code, msg, headers, newurl):
        if newurl.split('?', 1)[0] == self.callback:
            return None
        return super().redirect_request(request, fp, code, msg, headers, newurl)


def browser(context, callback):
    jar = http.cookiejar.CookieJar()
    opener = urllib.request.build_opener(urllib.request.ProxyHandler({}),
                                      urllib.request.HTTPSHandler(context=context),
                                      urllib.request.HTTPCookieProcessor(jar),
                                      CaptureCallback(callback))
    opener.fixture_cookie_jar = jar
    return opener


def login(opener, authorization, callback, username, password, issuer):
    with opener.open(authorization, timeout=20) as reply:
        form = LoginForm()
        form.feed(reply.read().decode())
    if not form.action:
        raise RuntimeError('Keycloak password form unavailable')
    target = urllib.parse.urlsplit(form.action)
    expected = urllib.parse.urlsplit(issuer)
    if target.scheme != 'https' or target.netloc != expected.netloc:
        raise RuntimeError('Keycloak credential form outside HTTPS issuer origin')
    form.fields.update(username=username, password=password)
    request = urllib.request.Request(form.action, data=urllib.parse.urlencode(form.fields).encode())
    try:
        opener.open(request, timeout=20)
    except urllib.error.HTTPError as response:
        location = response.headers.get('Location', '')
        response.close()
        if response.code == 302 and location.split('?', 1)[0] == callback:
            params = urllib.parse.parse_qs(urllib.parse.urlsplit(location).query)
            if 'code' in params:
                return location
    raise RuntimeError('authorization code callback unavailable')


def request(opener, url, data=None, headers=None, method=None):
    req = urllib.request.Request(url, data=data, headers=headers or {}, method=method)
    try:
        response = opener.open(req, timeout=20)
    except urllib.error.HTTPError as error:
        response = error
    with response:
        return response.status, response.read(), response.headers


def wait_ready(opener, url, deadline=90):
    end = time.monotonic()+deadline
    while time.monotonic() < end:
        try:
            status, payload, _ = request(opener, url)
            if status == 200:
                return payload
        except urllib.error.URLError as error:
            if isinstance(error.reason, ssl.SSLCertVerificationError):
                raise RuntimeError('fixture HTTPS certificate verification failed') from None
        except OSError:
            pass
        time.sleep(.2)
    raise RuntimeError('fixture HTTPS readiness deadline exceeded')


def token_request(opener, endpoint, fields, client_secret=None):
    headers = {'Content-Type': 'application/x-www-form-urlencoded'}
    if client_secret:
        headers['Authorization'] = 'Basic '+base64.b64encode(('oc-console:'+client_secret).encode()).decode()
    status, body, _ = request(opener, endpoint, urllib.parse.urlencode(fields).encode(), headers)
    if status != 200:
        try:
            error = json.loads(body)
        except ValueError:
            error = {}
        category = error.get('error')
        if category not in ('invalid_grant', 'invalid_client', 'invalid_request', 'invalid_scope', 'unauthorized_client'):
            category = 'unclassified'
        description = str(error.get('error_description', '')).lower()
        reasons = [reason for reason in ('code verifier', 'offline', 'session', 'code not valid', 'invalid client', 'redirect_uri', 'client authentication', 'code is expired') if reason in description]
        raise RuntimeError('Keycloak token exchange rejected: HTTP '+str(status)+' '+category+' '+','.join(reasons))
    return json.loads(body)


def main(args):
    report = {'userAgentScope': 'Python HTTPS cookie-jar protocol client; browser rendering/SameSite not evaluated',
              'storageTopology': 'dual HTTPS S3/Admin listeners' if args.dual_admin else 'single HTTPS S3/Admin listener',
              'certificateTopology': 'independent Admin CA; IdP/S3/Console share primary CA' if args.dual_admin else 'shared temporary CA and loopback certificate',
              'dateUTC': datetime.datetime.now(datetime.timezone.utc).isoformat(),
              'status': 'running', 'checks': [], 'scope': 'isolated HTTPS Keycloak + real OtterIO STS',
              'harnessSha256': hashlib.sha256(Path(__file__).read_bytes()).hexdigest(),
              'programs': {name: program_identity(path, getattr(args, name+'_source', ''))
                           for name, path in [('server', args.server), ('oc', args.oc)] +
                           ([('console', args.console)] if args.console else [])}}
    container = None
    storage_container = None
    processes = []
    logs = []
    succeeded = False
    with tempfile.TemporaryDirectory(prefix='oc-keycloak-') as temporary:
        root = Path(temporary)
        os.chmod(root, 0o700)
        old_umask = os.umask(0o077)
        try:
            ports = []
            while len(ports) < (4 if args.dual_admin else 3):
                candidate = free_port()
                if candidate not in ports:
                    ports.append(candidate)
            issuer_host = 'localhost' if args.cross_site_idp else '127.0.0.1'
            issuer = f'https://{issuer_host}:{ports[0]}/realms/oc-acceptance'
            report['browserSiteTopology'] = 'localhost IdP and 127.0.0.1 Console (different sites)' if args.cross_site_idp else '127.0.0.1 IdP/Console (same site, different origins)'
            endpoint = f'https://127.0.0.1:{ports[1]}'
            public = f'https://127.0.0.1:{ports[2]}'
            admin = f'https://127.0.0.1:{ports[3]}' if args.dual_admin else endpoint
            callback = public+'/api/oidc/callback'
            certs = root/'certs'
            primary_ca = generate_certificate(root, certs, 'primary')
            (root/'ca.pem').write_bytes(primary_ca.read_bytes())
            (certs/'CAs').mkdir()
            admin_certs = root/'admin-certs' if args.dual_admin else certs
            admin_ca = generate_certificate(root, admin_certs, 'admin') if args.dual_admin else primary_ca
            report['certificateIdentities'] = {'primaryCAsha256': hashlib.sha256(primary_ca.read_bytes()).hexdigest(),
                                               'adminCAsha256': hashlib.sha256(admin_ca.read_bytes()).hexdigest()}
            (certs/'CAs'/'fixture.pem').write_bytes((root/'ca.pem').read_bytes())
            context = ssl.create_default_context(cafile=str(root/'ca.pem'))
            opener = browser(context, callback)
            passwords = {name: secrets.token_urlsafe(24) for name in ('alice', 'bob')}
            client_secret = None if args.public_client else secrets.token_urlsafe(32)
            secret_file = root/'client-secret';secret_file.write_text(client_secret or '')
            realm = root/'oc-acceptance-realm.json';realm.write_text(json.dumps(realm_config(callback, passwords, client_secret)))
            try:
                image_info = json.loads(run(['docker', 'image', 'inspect', KEYCLOAK_IMAGE]))[0]
            except RuntimeError:
                run(['docker', 'pull', KEYCLOAK_IMAGE], timeout=240)
                image_info = json.loads(run(['docker', 'image', 'inspect', KEYCLOAK_IMAGE]))[0]
            digest = next(item for item in image_info['RepoDigests'] if item.startswith('quay.io/keycloak/keycloak@sha256:'))
            report['keycloak'] = {'version': '26.8.0', 'image': digest, 'platform': image_info['Os']+'/'+image_info['Architecture'],
                                   'profile': ('public/none' if args.public_client else 'confidential/client_secret_basic')+'; RS256; code; mandatory PKCE S256; HTTPS; offline_access'}
            container = 'oc-keycloak-'+secrets.token_hex(5)
            # Root only inside this disposable container so private host-mounted key files remain 0600.
            bindings = ['-p', f'127.0.0.1:{ports[0]}:{ports[0]}']
            if args.server_container_image:
                bindings += ['-p', f'127.0.0.1:{ports[1]}:{ports[1]}']
                if args.dual_admin:
                    bindings += ['-p', f'127.0.0.1:{ports[3]}:{ports[3]}']
            run(['docker', 'run', '-d', '--name', container, '--user', '0', *bindings,
                 '-v', str(certs)+':/fixture-certs:ro', '-v', str(realm)+':/opt/keycloak/data/import/oc-acceptance-realm.json:ro',
                 digest, 'start-dev', '--import-realm', '--http-enabled=false', '--https-port='+str(ports[0]), '--hostname', issuer.rsplit('/realms/', 1)[0],
                 '--https-certificate-file=/fixture-certs/public.crt', '--https-certificate-key-file=/fixture-certs/private.key',
                 '--log-level=INFO,org.keycloak.events:DEBUG'])
            discovery = json.loads(wait_ready(opener, issuer+'/.well-known/openid-configuration', 180))
            report['discoveryProfile'] = {key: discovery.get(key) for key in ('id_token_signing_alg_values_supported', 'response_types_supported', 'code_challenge_methods_supported', 'token_endpoint_auth_methods_supported')}
            check_discovery(discovery, issuer, 'none' if args.public_client else 'client_secret_basic')
            report['checks'].append('actual Keycloak discovery satisfies fixed HTTPS origin RS256/code/S256/'+('none' if args.public_client else 'client_secret_basic')+' profile')
            env = {k:v for k,v in os.environ.items() if not k.startswith(('OC_', 'MC_', 'OTTERIO_'))}
            access, secret = 'fixture-'+secrets.token_hex(8), secrets.token_urlsafe(32)
            env.update(OTTERIO_ROOT_USER=access, OTTERIO_ROOT_PASSWORD=secret, OTTERIO_BROWSER='off',
                       OTTERIO_IDENTITY_OPENID_CONFIG_URL=issuer+'/.well-known/openid-configuration',
                       OTTERIO_IDENTITY_OPENID_CLIENT_ID='oc-console', NO_PROXY='*', no_proxy='*')
            def start(command, name, process_env=None):
                log = (root/(name+'.log')).open('wb');logs.append(log)
                process = subprocess.Popen(command, env=process_env if process_env is not None else env, stdout=log, stderr=log)
                processes.append(process)
                return process
            if args.server_container_image:
                import re
                if not re.fullmatch(r'[^\s@]+@sha256:[0-9a-f]{64}', args.server_container_image):
                    raise RuntimeError('server container base image must have immutable sha256 digest')
                if report['programs']['server']['platform'] != 'linux/arm64':
                    raise RuntimeError('shared-clock fixture requires a clean linux/arm64 server program')
                server_env = root/'server.env'
                server_env.write_text('\n'.join(key+'='+value for key, value in env.items()
                                               if key.startswith('OTTERIO_') or key in ('NO_PROXY', 'no_proxy'))+'\n')
                binary = root/'otterio'
                binary.write_bytes(Path(args.server).read_bytes());binary.chmod(0o700)
                storage_container = 'oc-sts-'+secrets.token_hex(5)
                admin_args = ['--console-address', ':'+str(ports[3]), '--console-certs-dir', '/fixture/admin-certs'] if args.dual_admin else []
                run(['docker', 'run', '-d', '--name', storage_container, '--user', '0',
                     '--network', 'container:'+container, '--env-file', str(server_env),
                     '-v', str(root)+':/fixture', '--entrypoint', '/fixture/otterio',
                     args.server_container_image, 'server', '--address', ':'+str(ports[1]),
                     '--certs-dir', '/fixture/certs', *admin_args, '/fixture/data'])
                report['storageContainer'] = {'baseImage': args.server_container_image,
                                               'runtimeProgram': report['programs']['server'],
                                               'clockTopology': 'Keycloak and OtterIO share Docker VM clock and temporary network namespace'}
            else:
                admin_args = ['--console-address', '127.0.0.1:'+str(ports[3]), '--console-certs-dir', str(admin_certs)] if args.dual_admin else []
                start([args.server, 'server', '--address', '127.0.0.1:'+str(ports[1]),
                       '--certs-dir', str(certs), *admin_args, str(root/'data')], 'server')
            wait_ready(opener, endpoint+'/otterio/health/ready')
            if args.dual_admin:
                try:
                    request(opener, admin+'/otterio/health/ready')
                except urllib.error.URLError as error:
                    if not isinstance(error.reason, ssl.SSLCertVerificationError):
                        raise RuntimeError('independent Admin CA negative did not fail by certificate validation') from None
                else:
                    raise RuntimeError('S3 primary CA incorrectly trusted independent Admin listener')
                admin_opener = browser(ssl.create_default_context(cafile=str(admin_ca)), callback)
                wait_ready(admin_opener, admin+'/otterio/health/ready')
                report['checks'].append('independent Admin HTTPS CA accepted; S3 primary CA is rejected for Admin listener')
            config = root/'oc-config';(config/'certs'/'CAs').mkdir(parents=True)
            (config/'certs'/'CAs'/'fixture.pem').write_bytes((root/'ca.pem').read_bytes())
            def cli(*argv):
                try:
                    return run([args.oc, '--config-dir', str(config), *argv], env=env)
                except RuntimeError:
                    raise RuntimeError('isolated CLI action failed: '+argv[0]) from None
            # Bootstrap uses a private subprocess environment, never secret argv.
            env.update(OC_ADMIN_URL=admin, OC_ADMIN_CA=str(admin_ca))
            env['MC_HOST_fixture'] = endpoint.replace('https://', 'https://'+access+':'+urllib.parse.quote(secret, safe='')+'@', 1)
            cli('ls', 'fixture')
            spec = importlib.util.spec_from_file_location('console_existing_fixture', ROOT/'buildscripts/test-console-integration.py')
            module = importlib.util.module_from_spec(spec);spec.loader.exec_module(module)
            for user in passwords:
                bucket = user+'-bucket'
                cli('mb', 'fixture/'+bucket)
                policy = {'Version': '2012-10-17', 'Statement': [
                    {'Effect': 'Allow', 'Action': ['s3:ListAllMyBuckets'], 'Resource': ['arn:aws:s3:::*']},
                    {'Effect': 'Allow', 'Action': ['s3:ListBucket'], 'Resource': ['arn:aws:s3:::'+bucket]},
                    {'Effect': 'Allow', 'Action': ['s3:GetObject'], 'Resource': ['arn:aws:s3:::'+bucket+'/allowed/*']} ]}
                path = root/(user+'-policy.json');path.write_text(json.dumps(policy))
                cli('admin', 'policy', 'add', 'fixture', user+'-read', str(path))
                for key in ('allowed/object.txt', 'denied/object.txt'):
                    status, _, _ = module.signed_request(endpoint, 'PUT', '/'+bucket+'/'+key, b'fixture-object', access, secret, context)
                    if status != 200:
                        raise RuntimeError('fixture object creation failed')
            report['checks'].append('isolated two-user distinct bucket/prefix policies and real objects created')
            def sts_and_permissions(token, user):
                fields = {'Action': 'AssumeRoleWithWebIdentity', 'Version': '2011-06-15', 'WebIdentityToken': token}
                status, body, _ = request(opener, endpoint, urllib.parse.urlencode(fields).encode(),
                                          {'Content-Type': 'application/x-www-form-urlencoded'})
                if status != 200:
                    try:
                        failure = {node.tag.rsplit('}',1)[-1]: node.text for node in ET.fromstring(body).iter()}
                        code = failure.get('Code', '')
                        message = str(failure.get('Message', '')).lower()
                        reasons = [value for value in ('before issued', 'not valid yet', 'expired', 'signature', 'policy', 'claim missing', 'public key', 'connection refused') if value in message]
                    except ET.ParseError:
                        code = ''
                        reasons = []
                    import re
                    code = code if re.fullmatch(r'[A-Za-z]{1,80}', code or '') else 'unclassified'
                    raise RuntimeError('real STS exchange rejected: HTTP '+str(status)+' '+code+' user='+user+' reasons='+','.join(reasons))
                tree = ET.fromstring(body)
                values = {element.tag.rsplit('}',1)[-1]: element.text for element in tree.iter()}
                for bucket, key, expected in [(user+'-bucket', 'allowed/object.txt', 200),
                                              (user+'-bucket', 'denied/object.txt', 403),
                                              (('bob' if user == 'alice' else 'alice')+'-bucket', 'allowed/object.txt', 403)]:
                    status, body, _ = module.signed_request(endpoint, 'GET', '/'+bucket+'/'+key, b'',
                        values['AccessKeyId'], values['SecretAccessKey'], context,
                        extra_headers={'X-Amz-Security-Token': values['SessionToken']})
                    if status != expected or (expected == 200 and body != b'fixture-object'):
                        raise RuntimeError('STS bucket/prefix permission mismatch')
            for user, password in passwords.items():
                verifier = secrets.token_urlsafe(48)
                challenge = base64.urlsafe_b64encode(hashlib.sha256(verifier.encode()).digest()).rstrip(b'=').decode()
                state, nonce = secrets.token_urlsafe(24), secrets.token_urlsafe(24)
                authorization = discovery['authorization_endpoint']+'?'+urllib.parse.urlencode({
                    'client_id': 'oc-console', 'response_type': 'code', 'redirect_uri': callback,
                    'scope': 'openid offline_access', 'state': state, 'nonce': nonce,
                    'code_challenge': challenge, 'code_challenge_method': 'S256'})
                location = login(browser(context, callback), authorization, callback, user, password, issuer)
                params = urllib.parse.parse_qs(urllib.parse.urlsplit(location).query)
                if params.get('state') != [state]:
                    raise RuntimeError('authorization state mismatch')
                fields = {'grant_type': 'authorization_code', 'client_id': 'oc-console', 'redirect_uri': callback,
                          'code': params['code'][0], 'code_verifier': verifier}
                tokens = token_request(opener, discovery['token_endpoint'], fields, client_secret)
                # This is structural inspection only; real STS verifies the signed token.
                header, claims, _ = tokens['id_token'].split('.')
                decode = lambda value: json.loads(base64.urlsafe_b64decode(value+'='*((-len(value))%4)))
                if decode(claims).get('policy') != user+'-read':
                    raise RuntimeError('Keycloak ID token storage policy claim mismatch')
                if decode(header).get('alg') != 'RS256' or decode(claims).get('nonce') != nonce:
                    raise RuntimeError('ID token algorithm/nonce mismatch')
                report.setdefault('initialTokenClock', {})[user] = {
                    'issuedAtOffsetSeconds': round(decode(claims).get('iat', 0)-time.time(), 3),
                    'secondsUntilExpiry': round(decode(claims).get('exp', 0)-time.time(), 3)}
                sts_and_permissions(tokens['id_token'], user)
                report['checks'].append(user+': real code/PKCE login STS permits own prefix and denies other prefix/user bucket')
                refreshed = token_request(opener, discovery['token_endpoint'], {'grant_type': 'refresh_token',
                                         'client_id': 'oc-console', 'refresh_token': tokens['refresh_token']}, client_secret)
                refreshed_claims = decode(refreshed['id_token'].split('.')[1])
                report.setdefault('keycloakRefresh', {})[user] = check_refresh_claims(decode(claims), refreshed_claims, nonce)
                sts_and_permissions(refreshed['id_token'], user)
                report['checks'].append(user+': real code/PKCE login and refresh STS credentials allow own prefix and deny other prefix/user bucket')
            report['keycloakSuccessfulRefreshEvents'] = keycloak_refresh_events(container)
            if report['keycloakSuccessfulRefreshEvents'] < 2:
                raise RuntimeError('real Keycloak successful refresh events unavailable')
            if args.console:
                console_secret_args = ['--oidc-client-secret-file', str(secret_file)] if client_secret else []
                if args.console_session_ttl:
                    console_secret_args += ['--session-ttl', args.console_session_ttl]
                console_started = time.monotonic()
                start([args.console, *console_secret_args, '--auth-mode', 'oidc', '--oidc-issuer', issuer, '--oidc-client-id', 'oc-console',
                       '--oidc-ca', str(root/'ca.pem'), '--s3-url', endpoint, '--admin-url', admin,
                       '--s3-ca', str(root/'ca.pem'), '--admin-ca', str(admin_ca), '--public-url', public,
                       '--address', '127.0.0.1:'+str(ports[2]), '--tls-cert', str(certs/'public.crt'),
                       '--tls-key', str(certs/'private.key'), '--data-dir', str(root/'console-data')], 'console',
                      {key: value for key, value in env.items() if not key.startswith(('MC_', 'OC_', 'OTTERIO_'))})
                wait_ready(opener, public+'/')
                if args.browser_hook:
                    spec = importlib.util.spec_from_file_location('isolated_console_browser_hook', args.browser_hook)
                    hook = importlib.util.module_from_spec(spec)
                    spec.loader.exec_module(hook)
                    ttl = int(args.console_session_ttl[:-1]) if args.console_session_ttl.endswith('s') else 0
                    try:
                        result = hook.run_browser({'public': public, 'issuer': issuer, 'passwords': dict(passwords),
                                      'session_ttl_seconds': ttl, 'refresh_wait_seconds': args.console_refresh_wait,
                                      'refresh_events': lambda: keycloak_refresh_events(container),
                                      'ca_file': str(root/'ca.pem')})
                    except Exception:
                        raise RuntimeError('isolated browser acceptance hook failed') from None
                    encoded = json.dumps(result)
                    if any(value and value in encoded for value in [access, secret, client_secret, *passwords.values()]):
                        raise RuntimeError('browser acceptance report disclosed fixture credentials')
                    report['browserAcceptance'] = result
                    report['browserHook'] = {'name': Path(args.browser_hook).name,
                                            'sha256': hashlib.sha256(Path(args.browser_hook).read_bytes()).hexdigest()}
                users = {}
                for user, password in passwords.items():
                    user_browser = browser(context, callback)
                    location = login(user_browser, public+'/api/oidc/login', callback, user, password, issuer)
                    status, _, _ = request(browser(context, callback), location)
                    if status not in (400, 401, 403):
                        raise RuntimeError('callback accepted from wrong browser binding')
                    status, _, _ = request(user_browser, location)
                    if status not in (200, 302, 303):
                        raise RuntimeError('Console callback rejected')
                    for owner, prefix, expected in [(user, 'allowed', 200), (user, 'denied', 403),
                                                   ('bob' if user == 'alice' else 'alice', 'allowed', 403)]:
                        query = urllib.parse.urlencode({'bucket': owner+'-bucket', 'key': prefix+'/object.txt'})
                        status, body, _ = request(user_browser, public+'/api/download?'+query)
                        if status != expected or (expected == 200 and body != b'fixture-object'):
                            raise RuntimeError('Console independent user permission mismatch')
                    status, body, _ = request(user_browser, public+'/api/session')
                    if status != 200:
                        raise RuntimeError('Console session unavailable after callback')
                    session = json.loads(body)
                    status, account, _ = request(user_browser, public+'/api/account')
                    if status != 200:
                        raise RuntimeError('authenticated Console Admin account request failed')
                    buckets = json.loads(account)['buckets']
                    if [bucket['name'] for bucket in buckets] != [user+'-bucket']:
                        raise RuntimeError('Admin account response exposes other user bucket')
                    users[user] = (user_browser, session['csrfToken'])
                    status, _, _ = request(user_browser, location)
                    if status not in (400, 401, 403):
                        raise RuntimeError('callback replay accepted')
                    report['checks'].append(user+': production Console binding/replay rejection, isolated object permissions and scoped Admin account')
                def api(user, path, method='GET', value=None):
                    user_browser, csrf = users[user]
                    headers = {'Origin': public, 'X-CSRF-Token': csrf}
                    data = None
                    if value is not None:
                        headers['Content-Type'] = 'application/json'
                        data = json.dumps(value).encode()
                    return request(user_browser, public+path, data, headers, method)
                status, _, _ = api('alice', '/api/preferences', 'PUT', {'action': 'language', 'language': 'en'})
                if status != 200:
                    raise RuntimeError('Alice preference update failed')
                status, _, _ = api('bob', '/api/preferences', 'PUT', {'action': 'language', 'language': 'zh'})
                if status != 200:
                    raise RuntimeError('Bob preference update failed')
                for user, language in [('alice', 'en'), ('bob', 'zh')]:
                    status, body, _ = api(user, '/api/preferences')
                    if status != 200 or json.loads(body)['language'] != language:
                        raise RuntimeError('independent user preferences leaked')
                status, body, _ = api('alice', '/api/archives', 'POST',
                                     {'refs': [{'bucket': 'alice-bucket', 'key': 'allowed/object.txt'}]})
                if status != 202:
                    raise RuntimeError('Alice archive task creation failed')
                archive_id = json.loads(body)['id']
                status, _, _ = api('bob', '/api/archives/'+archive_id)
                if status != 404:
                    raise RuntimeError('Bob can read Alice archive task')
                status, body, _ = api('bob', '/api/archives')
                if status != 200 or any(task['id'] == archive_id for task in json.loads(body)['archives']):
                    raise RuntimeError('Alice archive task leaked in Bob list')
                report['checks'].append('simultaneously active Alice/Bob preferences and archive tasks remain isolated')
                old_cookie = '; '.join(cookie.name+'='+cookie.value for cookie in users['alice'][0].fixture_cookie_jar)
                bob_fixed_cookie = '; '.join(cookie.name+'='+cookie.value for cookie in users['bob'][0].fixture_cookie_jar)
                status, _, _ = api('alice', '/api/logout', 'POST')
                if status != 204:
                    raise RuntimeError('Alice logout failed')
                status, _, _ = request(browser(context, callback), public+'/api/session', headers={'Cookie': old_cookie})
                if status != 401:
                    raise RuntimeError('Alice old session cookie remained usable after logout')
                status, _, _ = api('bob', '/api/download?bucket=bob-bucket&key=allowed%2Fobject.txt')
                if status != 200:
                    raise RuntimeError('Alice logout disrupted Bob session')
                report['checks'].append('Alice logout rejects old cookie while Bob continues authorized reads')
                before_refresh = keycloak_refresh_events(container)
                deadline = time.monotonic()+args.console_refresh_wait
                while time.monotonic() < deadline:
                    status, _, _ = api('bob', '/api/download?bucket=bob-bucket&key=allowed%2Fobject.txt')
                    if status != 200:
                        raise RuntimeError('Bob session failed across initial ID token lifetime')
                    time.sleep(.5)
                after_refresh = keycloak_refresh_events(container)
                report['consoleRefreshEvidence'] = {'waitSeconds': args.console_refresh_wait,
                                                   'idTokenSeconds': 90, 'keycloakEventsBefore': before_refresh,
                                                   'keycloakEventsAfter': after_refresh}
                if after_refresh <= before_refresh:
                    raise RuntimeError('no actual Keycloak refresh event observed for Console')
                report['checks'].append('Bob survives initial ID token expiry; additional actual Keycloak refresh events observed')
                if args.console_session_ttl:
                    # Fixed TTL uses the same start-of-session bound, never a refreshed deadline.
                    import re
                    match = re.fullmatch(r'(\d+)s', args.console_session_ttl)
                    if not match:
                        raise RuntimeError('acceptance fixed session TTL must use integer seconds')
                    end = console_started+int(match.group(1))+5
                    while time.monotonic() < end:
                        time.sleep(.5)
                    status, _, _ = request(browser(context, callback), public+'/api/session',
                                           headers={'Cookie': bob_fixed_cookie})
                    if status != 401:
                        raise RuntimeError('refresh extended the fixed session lifetime')
                    report['checks'].append('Bob saved pre-expiry cookie is rejected after fixed TTL despite successful refresh')
                else:
                    report['consoleFixedExpiry'] = 'not run: supply --console-session-ttl with integrated --session-ttl support'
                protected_log = (root/'console.log').read_bytes()
                for value in [access, secret, client_secret, *passwords.values()]:
                    if value and value.encode() in protected_log:
                        raise RuntimeError('Console log disclosed fixture authentication material')
                report['checks'].append('Console protected log contains no fixture password, root credential or client secret')
                report['consoleAcceptance'] = 'passed: binding/replay, simultaneous users, permissions, preferences/tasks, local logout, observed refresh'


            else:
                report['consoleAcceptance'] = 'not run: supply integrated --console and --console-source'
            succeeded = True
            report['status'] = 'passed'
        except KeyboardInterrupt:
            report['status'] = 'failed'
            report['error'] = 'fixture interrupted; cleanup attempted'
        except Exception as error:
            report['status'] = 'failed'
            report['error'] = str(error) if isinstance(error, RuntimeError) else type(error).__name__
        finally:
            cleanup = True
            for process in reversed(processes):
                try:
                    process.terminate();process.wait(timeout=12)
                except subprocess.TimeoutExpired:
                    process.kill();process.wait(timeout=5)
                except Exception:
                    cleanup = False
            for log in logs:
                log.close()
            for name in (storage_container, container):
                if name:
                    try:
                        run(['docker', 'rm', '-f', name])
                    except Exception:
                        cleanup = False
            os.umask(old_umask)
            report['cleanup'] = 'passed' if cleanup else 'failed'
            if not cleanup:
                report['status'] = 'failed'
            args.output.parent.mkdir(parents=True, exist_ok=True)
            args.output.write_text(json.dumps(report, indent=2)+'\n')
            print(json.dumps(report))
    if not succeeded or report['cleanup'] != 'passed':
        raise SystemExit(1)


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--server', required=True, type=lambda x: str(Path(x).resolve()))
    parser.add_argument('--server-source', required=True)
    parser.add_argument('--browser-hook', default='', type=lambda x: str(Path(x).resolve()), help='isolated trusted local module exposing run_browser(config); credentials passed in memory only')
    parser.add_argument('--cross-site-idp', action='store_true', help='localhost issuer vs 127.0.0.1 Console for real browser SameSite checks')
    parser.add_argument('--dual-admin', action='store_true', help='separate Admin HTTPS listener with independent temporary CA')
    parser.add_argument('--server-container-image', default='', help='fixed-digest image base for clean Linux arm64 server; shares Keycloak Docker clock')
    parser.add_argument('--oc', required=True, type=lambda x: str(Path(x).resolve()))
    parser.add_argument('--oc-source', required=True)
    parser.add_argument('--console', type=lambda x: str(Path(x).resolve()))
    parser.add_argument('--console-source', default='')
    parser.add_argument('--console-session-ttl', default='', help='integer seconds, e.g. 150s; requires integrated --session-ttl')
    parser.add_argument('--console-refresh-wait', type=float, default=95, help='observe Console beyond initial 90-second ID token lifetime')
    parser.add_argument('--public-client', action='store_true', help='negative compatibility profile: Keycloak omits none discovery method')
    parser.add_argument('--output', required=True, type=Path)
    main(parser.parse_args())
