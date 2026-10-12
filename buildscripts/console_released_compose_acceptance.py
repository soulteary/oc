#!/usr/bin/env python3
"""Test the pinned Compose example on disposable ports, credentials and volumes.

Requires Docker Compose and the published images. Never uses an existing project
or the user's OC config. Cleans containers and temporary host files in finally.
"""
import argparse,datetime,hashlib,http.cookiejar,json,os,platform,secrets,socket,subprocess,tempfile,time,urllib.request,urllib.error
from pathlib import Path
from console_acceptance_protocol import legacy_route_disabled
ROOT=Path(__file__).resolve().parents[1]
def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1',0));return s.getsockname()[1]
def container_platform(container):
    # Inspect the running container's actual image, not a multi-arch tag or host.
    image = subprocess.run(['docker', 'inspect', '--format', '{{.Image}}', container],
                           capture_output=True, text=True, timeout=15, check=True).stdout.strip()
    reply = subprocess.run(['docker', 'image', 'inspect', '--format',
                            '{{.Os}}/{{.Architecture}}{{if .Variant}}/{{.Variant}}{{end}}', image],
                           capture_output=True, text=True, timeout=15, check=True).stdout.strip()
    if not image or len(reply.split('/')) not in (2, 3) or any(not item for item in reply.split('/')):
        raise RuntimeError('cannot determine the running container platform')
    return reply


def probe_legacy_route(container, method, path):
    result = subprocess.run(['docker', 'exec', container, 'curl', '-sS', '-w', '\n%{http_code}',
                             '-A', 'Mozilla/5.0', '-X', method, 'http://127.0.0.1:9001' + path],
                            capture_output=True, text=True, timeout=15, check=True)
    body, separator, code = result.stdout.rpartition('\n')
    if not separator or not code.isdigit() or not legacy_route_disabled(int(code), body):
        raise RuntimeError(f'legacy route is not a confirmed storage-protocol rejection: {method} {path} {code}')


def images_from_recipe(recipe):
    """Read the exact pinned service refs without a second handwritten matrix."""
    import re
    images = {}
    service = None
    for line in recipe.splitlines():
        match = re.fullmatch(r'  ([a-z-]+):', line)
        if match:
            service = match.group(1)
        match = re.fullmatch(r'    image: (\S+)', line)
        if match:
            ref = match.group(1)
            if not re.fullmatch(r'[^\s@]+@sha256:[0-9a-f]{64}', ref):
                raise ValueError('Compose image must have an immutable sha256 digest')
            images[service] = ref
    if set(images) != {'oc', 'oc-init', 'otterio'} or images['oc'] != images['oc-init']:
        raise ValueError('Compose must pair the console and initializer image')
    return images


def finalize_report(report, output, cleanup):
    """Always save evidence, preserving the primary failure if cleanup fails."""
    primary_failed = report['status'] != 'passed'
    try:
        cleanup()
        report['cleanup'] = 'passed'
    except Exception:
        report['cleanup'] = 'failed'
        report['cleanupError'] = 'temporary Compose project cleanup failed'
        if report['status'] == 'passed':
            report['status'] = 'failed'
    output.parent.mkdir(parents=True, exist_ok=True)
    output.write_text(json.dumps(report, indent=2) + '\n', encoding="utf-8")
    print(json.dumps(report))
    if report['cleanup'] == 'failed' and not primary_failed:
        raise RuntimeError('temporary Compose project cleanup failed; report saved') from None


def main(output):
    urllib.request.install_opener(urllib.request.build_opener(urllib.request.ProxyHandler({})))
    checks=[]
    original=(ROOT/'deploy/compose.console.yaml').read_text(encoding="utf-8")
    report={'dateUTC':datetime.datetime.now(datetime.timezone.utc).isoformat(),'hostPlatform':platform.platform(),'status':'running','checks':checks,'images':images_from_recipe(original),'composeSha256':hashlib.sha256(original.encode()).hexdigest(),'harnessSha256':hashlib.sha256(Path(__file__).read_bytes()).hexdigest()}
    with tempfile.TemporaryDirectory(prefix='oc-compose-release-') as temporary:
        root=Path(temporary);project='oc-release-'+secrets.token_hex(4)
        s3,web=port(),port()
        while web==s3:web=port()
        base=f'http://127.0.0.1:{web}'
        recipe=original.replace('127.0.0.1:9000:9000',f'127.0.0.1:{s3}:9000').replace('127.0.0.1:9090',f'127.0.0.1:{web}')
        file=root/'compose.yaml';file.write_text(recipe, encoding="utf-8")
        access='smoke-'+secrets.token_hex(5);secret=secrets.token_hex(24)
        env={k:v for k,v in os.environ.items() if not k.startswith(('OC_', 'MC_', 'OTTERIO_', 'COMPOSE_'))}
        env.update(OTTERIO_ROOT_USER=access,OTTERIO_ROOT_PASSWORD=secret)
        for d in ('oc-config','oc-console-data','otterio-data'):(root/d).mkdir()
        def compose(*args):
            r=subprocess.run(['docker','compose','-p',project,'-f',str(file),*args],env=env,capture_output=True,text=True,timeout=120)
            if r.returncode:raise RuntimeError('Compose operation failed: '+args[0])
            return r.stdout
        def fixture_cli(*args):
            container=compose('ps','-q','oc').strip()
            try:
                result=subprocess.run(['docker','exec',container,'oc','--config-dir','/config',*args],env=env,capture_output=True,timeout=30)
            except subprocess.TimeoutExpired:
                raise RuntimeError('temporary object fixture CLI timeout') from None
            if result.returncode:
                raise RuntimeError('temporary object fixture CLI failed')
            return result.stdout
        def ready():
            for _ in range(300):
                try:
                    with urllib.request.urlopen(f'http://127.0.0.1:{s3}/otterio/health/ready',timeout=1) as r:
                        if r.status==200:return
                except (OSError,urllib.error.HTTPError):pass
                time.sleep(.1)
            raise RuntimeError('storage readiness timeout')
        def browser():
            for _ in range(200):
                logs=compose('logs','--no-color','oc')
                if 'Login code: ' in logs:
                    code=logs.split('Login code: ')[-1].splitlines()[0].strip();break
                time.sleep(.1)
            else:raise RuntimeError('console startup timeout')
            opener=urllib.request.build_opener(urllib.request.ProxyHandler({}),urllib.request.HTTPCookieProcessor(http.cookiejar.CookieJar()))
            def req(path,method='GET',body=None,csrf=None):
                headers={'Origin':base,'Content-Type':'application/json'}
                if csrf:headers['X-CSRF-Token']=csrf
                data=json.dumps(body).encode() if body is not None else None
                with opener.open(urllib.request.Request(base+path,data=data,method=method,headers=headers),timeout=20) as r:return json.loads(r.read())
            session=req('/api/login','POST',{'code':code});return req,session['csrfToken']
        try:
            compose('config','--quiet');checks.append('modified host ports and fixed-digest Compose configuration valid')
            compose('up','-d','otterio');ready()
            compose('run','--rm','oc-init');compose('up','-d','--no-deps','oc')
            report['containerPlatforms'] = {service: container_platform(compose('ps','-q',service).strip()) for service in ('oc', 'otterio')}
            report['platform'] = 'Docker containers: ' + ', '.join(sorted(set(report['containerPlatforms'].values())))
            runtime = subprocess.run(['docker','info','--format','{{json .OperatingSystem}}'],capture_output=True,text=True,timeout=15,check=True)
            report['dockerOperatingSystem'] = json.loads(runtime.stdout)
            req,csrf=browser();req('/api/buckets');checks.append('published OC Console authenticates and reads buckets through bridge')
            bucket='persistence-'+secrets.token_hex(5)
            payload=b'OC disposable release persistence fixture\n'
            fixture=root/'object.txt';fixture.write_bytes(payload)
            container=compose('ps','-q','oc').strip()
            subprocess.run(['docker','cp',str(fixture),container+':/tmp/oc-release-object.txt'],capture_output=True,timeout=15,check=True)
            fixture_cli('mb','store/'+bucket)
            fixture_cli('cp','/tmp/oc-release-object.txt','store/'+bucket+'/object.txt')
            def verify_object():
                if fixture_cli('cat','store/'+bucket+'/object.txt') != payload:
                    raise RuntimeError('persisted object bytes changed')
            verify_object()
            checks.append('published CLI creates and reads disposable object bytes')
            compose('up','-d','--force-recreate','otterio');ready();verify_object()
            checks.append('object bytes survive storage container recreation on unchanged image and volume')
            pref={'language' :'en','favorites':[{'bucket':'example-bucket','key':'example.txt'}],'recent':['example-bucket']}
            req('/api/preferences','PUT',{'action':'language','language':'en'},csrf)
            req('/api/preferences','PUT',{'action':'favorite-add','bucket':'example-bucket','key':'example.txt'},csrf)
            req('/api/preferences','PUT',{'action':'visit','bucket':'example-bucket'},csrf)
            compose('restart','oc');req,csrf=browser();assert req('/api/preferences')==pref
            checks.append('language favorites and recent preferences survive console restart')
            # These disposable preferences contain references, not actual storage data.
            for method,path in [('GET','/otterio/'),('POST','/otterio/webrpc'),('PUT','/otterio/upload/example/key'),('GET','/otterio/download/example/key?token=x'),('POST','/otterio/zip?token=x')]:
                # Admin port remains internal. Probe inside its server container via curl.
                container=compose('ps','-q','otterio').strip()
                probe_legacy_route(container,method,path)
            checks.append('legacy page RPC upload download ZIP unavailable on internal Admin listener')
            # Rollback toggles UI on the same image and volume, never downgrades storage.
            file.write_text(recipe.replace("OTTERIO_BROWSER: 'off'","OTTERIO_BROWSER: 'on'"), encoding="utf-8");compose('up','-d','otterio');ready()
            container=compose('ps','-q','otterio').strip()
            r=subprocess.run(['docker','exec',container,'curl','-s','-o','/dev/null','-w','%{http_code}','-A','Mozilla/5.0','http://127.0.0.1:9001/otterio/'],capture_output=True,text=True,timeout=15)
            assert r.returncode==0 and r.stdout=='200',r.stdout
            verify_object()
            checks.append('same-server same-volume rollback restores embedded page and object bytes')
            file.write_text(recipe, encoding="utf-8");compose('up','-d','otterio');ready();req('/api/buckets')
            verify_object()
            checks.append('return to browser-off retains OC access and object bytes')
            report['status']='passed'
        except Exception as e:
            report['status']='failed';report['error']='acceptance failed: '+type(e).__name__
            raise
        finally:
            finalize_report(report, output, lambda: compose('down','--remove-orphans'))


if __name__ == '__main__':
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument('--output', type=Path, default=ROOT/'docs/console-released-compose-results.json')
    main(parser.parse_args().output)
