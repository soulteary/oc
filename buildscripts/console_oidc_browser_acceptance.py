"""Isolated real Chromium hook for console_oidc_keycloak_acceptance.py.
No persistent profile, credential file, screenshots, or browser URL logs are produced.
Run harness with --browser-hook THIS_FILE --cross-site-idp and 150s session TTL.
"""
import json
import os
from pathlib import Path
import subprocess
import signal


def run_browser(config):
    events = config['refresh_events']
    before = events()
    payload = {key: config[key] for key in ('public', 'issuer', 'passwords', 'session_ttl_seconds', 'refresh_wait_seconds')}
    if payload['session_ttl_seconds'] <= payload['refresh_wait_seconds']:
        raise RuntimeError('browser acceptance requires refresh before fixed deadline')
    node = os.environ.get('OC_BROWSER_NODE', '/Users/soulteary/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/bin/node')
    for env, key in [('OC_BROWSER_EXECUTABLE','browserExecutable'), ('OC_BROWSER_PLAYWRIGHT_MODULE','playwrightModule')]:
        if os.environ.get(env):
            payload[key] = os.environ[env]
    process = subprocess.Popen([node, str(Path(__file__).with_suffix('.cjs'))], stdin=subprocess.PIPE,
                               stdout=subprocess.PIPE, stderr=subprocess.PIPE, text=True, start_new_session=True)
    try:
        stdout, _ = process.communicate(json.dumps(payload), timeout=payload['session_ttl_seconds'] + 90)
    except subprocess.TimeoutExpired:
        os.killpg(process.pid, signal.SIGTERM)
        try:
            process.communicate(timeout=5)
        except subprocess.TimeoutExpired:
            os.killpg(process.pid, signal.SIGKILL)
            process.communicate()
        print(json.dumps({'browserStatus': 'failed', 'stage': 'runner-timeout-owned-process-group-terminated'}))
        raise RuntimeError('browser runner timeout; owned processes terminated') from None
    try:
        report = json.loads(stdout)
    except (ValueError, TypeError):
        raise RuntimeError('browser acceptance produced no valid report') from None
    if process.returncode or report.get('status') != 'passed':
        print(json.dumps({'browserStatus': 'failed', 'stage': report.get('stage', 'unknown'), 'diagnostics':report.get('diagnostics', {})}))
        raise RuntimeError('browser acceptance failed; sensitive details suppressed')
    after = events()
    if after <= before:
        print(json.dumps({'browserStatus':'failed','stage':'successful-refresh-events-not-increased','eventsBefore':before,'eventsAfter':after,'checksPassed':len(report.get('checks',[]))}))
        raise RuntimeError('browser acceptance did not observe successful IdP refresh')
    report['successfulRefreshEventsDelta'] = after-before
    return report
