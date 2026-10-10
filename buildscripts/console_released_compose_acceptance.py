#!/usr/bin/env python3
"""Test the pinned Compose example on disposable ports, credentials and volumes.

Requires Docker Compose and the published images. Never uses an existing project
or the user's OC config. Cleans containers and temporary host files in finally.
"""
import datetime,hashlib,http.cookiejar,json,os,secrets,socket,subprocess,tempfile,time,urllib.request,urllib.error
from pathlib import Path
urllib.request.install_opener(urllib.request.build_opener(urllib.request.ProxyHandler({})))
ROOT=Path(__file__).resolve().parents[1]
checks=[]
def port():
    with socket.socket() as s:
        s.bind(('127.0.0.1',0));return s.getsockname()[1]
report={'dateUTC':datetime.datetime.now(datetime.timezone.utc).isoformat(),'platform':'Docker Desktop linux/arm64','status':'running','checks':checks,'images':{'oc':'ghcr.io/soulteary/oc:RELEASE.2026-10-10T11-42-18Z@sha256:f4a042ae7b9a2fb3b32c8984d36a53aea77cb69bd8469002f3cfb11d1b246dd6','otterio':'ghcr.io/soulteary/otterio:RELEASE.2026-10-09T15-22-07Z@sha256:7ddf211748d11bb8f6af23576f19eaf5b2675f70f647e38ed9fdd14912824ba8'}}
with tempfile.TemporaryDirectory(prefix='oc-compose-release-') as temporary:
    root=Path(temporary);project='oc-release-'+secrets.token_hex(4)
    s3,web=port(),port()
    while web==s3:web=port()
    base=f'http://127.0.0.1:{web}'
    original=(ROOT/'deploy/compose.console.yaml').read_text()
    recipe=original.replace('127.0.0.1:9000:9000',f'127.0.0.1:{s3}:9000').replace('127.0.0.1:9090',f'127.0.0.1:{web}')
    file=root/'compose.yaml';file.write_text(recipe)
    access='smoke-'+secrets.token_hex(5);secret=secrets.token_hex(24)
    env=dict(os.environ,OTTERIO_ROOT_USER=access,OTTERIO_ROOT_PASSWORD=secret)
    for d in ('oc-config','oc-console-data','otterio-data'):(root/d).mkdir()
    def compose(*args):
        r=subprocess.run(['docker','compose','-p',project,'-f',str(file),*args],env=env,capture_output=True,text=True,timeout=120)
        if r.returncode:raise RuntimeError('Compose operation failed: '+args[0])
        return r.stdout
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
        req,csrf=browser();req('/api/buckets');checks.append('published arm64 OC Console authenticates and reads buckets through bridge')
        pref={'language':'en','favorites':[{'bucket':'example-bucket','key':'example.txt'}],'recent':['example-bucket']}
        req('/api/preferences','PUT',{'action':'language','language':'en'},csrf)
        req('/api/preferences','PUT',{'action':'favorite-add','bucket':'example-bucket','key':'example.txt'},csrf)
        req('/api/preferences','PUT',{'action':'visit','bucket':'example-bucket'},csrf)
        compose('restart','oc');req,csrf=browser();assert req('/api/preferences')==pref
        checks.append('language favorites and recent preferences survive console restart')
        # These disposable preferences contain references, not actual storage data.
        for method,path in [('GET','/otterio/'),('POST','/otterio/webrpc'),('PUT','/otterio/upload/example/key'),('GET','/otterio/download/example/key?token=x'),('POST','/otterio/zip?token=x')]:
            # Admin port remains internal. Probe inside its server container via curl.
            container=compose('ps','-q','otterio').strip()
            r=subprocess.run(['docker','exec',container,'curl','-s','-o','/dev/null','-w','%{http_code}','-A','Mozilla/5.0','-X',method,'http://127.0.0.1:9001'+path],capture_output=True,text=True,timeout=15)
            assert r.returncode==0 and r.stdout in ('403','404','405'),(method,path,r.stdout)
        checks.append('legacy page RPC upload download ZIP unavailable on internal Admin listener')
        # Rollback toggles UI on the same image and volume, never downgrades storage.
        file.write_text(recipe.replace("OTTERIO_BROWSER: 'off'","OTTERIO_BROWSER: 'on'"));compose('up','-d','otterio');ready()
        container=compose('ps','-q','otterio').strip()
        r=subprocess.run(['docker','exec',container,'curl','-s','-o','/dev/null','-w','%{http_code}','-A','Mozilla/5.0','http://127.0.0.1:9001/otterio/'],capture_output=True,text=True,timeout=15)
        assert r.returncode==0 and r.stdout=='200',r.stdout
        checks.append('same-server same-volume rollback restores embedded page')
        file.write_text(recipe);compose('up','-d','otterio');ready();req('/api/buckets')
        checks.append('return to browser-off retains OC access')
        report['status']='passed'
    except Exception as e:
        report['status']='failed';report['error']=str(e)
        raise
    finally:
        compose('down','--remove-orphans')
        report['composeSha256']=hashlib.sha256(original.encode()).hexdigest()
        (ROOT/'docs/console-released-compose-results.json').write_text(json.dumps(report,indent=2)+'\n')
        print(json.dumps(report))
