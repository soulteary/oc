'use strict';
// Secrets arrive only through stdin; never log browser URLs, headers, or exceptions.
const fs = require('node:fs');
let stage = 'launch';
const diagnostics = {};
async function run(config) {
  const pw = require(config.playwrightModule || '/Users/soulteary/.cache/codex-runtimes/codex-primary-runtime/dependencies/node/node_modules/playwright');
  const browser = await pw.chromium.launch({headless:true, executablePath:config.browserExecutable || '/Users/soulteary/Library/Caches/ms-playwright/chromium_headless_shell-1194/chrome-mac/headless_shell'});
  const checks = [];
  const check = (name, value) => { stage = name; if (!value) throw new Error(name); checks.push({name,status:'passed'}); };
  try {
    check('different_sites', new URL(config.public).hostname !== new URL(config.issuer).hostname);
    const users = {};
    for (const [name,password] of Object.entries(config.passwords)) {
      const context = await browser.newContext({ignoreHTTPSErrors:true});
      const page = await context.newPage();
      let callbackBinding = false;
      page.on('request', request => {
        const url = new URL(request.url());
        if (url.origin === new URL(config.public).origin && url.pathname === '/api/oidc/callback') {
          // allHeaders is needed because regular headers may omit security headers.
          users[name].callbackRequest = request;
          request.allHeaders().then(h => {diagnostics.callbackBinding = /(?:^|;\s*)__Host-oc_oidc_binding=/.test(h.cookie || ''); diagnostics.fetchSite=h['sec-fetch-site']; diagnostics.originPresent=Boolean(h.origin);});
        }
      });
      page.on('response', response => { const u=new URL(response.url()); if(u.origin===new URL(config.public).origin && u.pathname==='/api/oidc/callback') diagnostics.callbackStatus=response.status(); if(u.origin===new URL(config.public).origin && u.pathname==='/') diagnostics.rootStatus=response.status(); });
      users[name] = {context,page,started:Date.now()};
      stage = name + '_console_page';
      await page.goto(config.public);
      stage = name + '_ui_login';
      await page.locator('#login-submit').click();
      stage = name + '_idp_form';
      await page.locator('#username').waitFor();
      const cookies = await context.cookies(config.public);
      check(name+'_binding_cookie_flags', cookies.some(c => c.name === '__Host-oc_oidc_binding' && c.httpOnly && c.secure && c.sameSite === 'Lax' && c.path === '/'));
      await page.locator('#username').fill(name);
      await page.locator('#password').fill(password);
      stage = name + '_callback';
      await page.locator('#kc-login').click();
      await page.waitForURL(url => url.origin === new URL(config.public).origin && url.pathname === '/');
      stage = name + '_console_landing';
      await page.locator('#logout').waitFor({state:'visible'});
      const request = users[name].callbackRequest;
      const headers = request && await request.allHeaders();
      callbackBinding = Boolean(headers && /(?:^|;\s*)__Host-oc_oidc_binding=/.test(headers.cookie || ''));
      check(name+'_cross_site_top_level_callback_binding', callbackBinding && request.method() === 'GET' && request.isNavigationRequest());
      check(name+'_ui_session', await page.evaluate(async()=> (await fetch('/api/session')).status) === 200);
      users[name].started = Date.now();
    }
    const alice = users.alice, bob = users.bob;
    await alice.page.locator('#logout').click();
    await alice.page.locator('#login-submit').waitFor({state:'visible'});
    check('alice_ui_logout', await alice.page.evaluate(async()=> (await fetch('/api/session')).status) === 401);
    check('bob_logout_isolation', await bob.page.evaluate(async()=> (await fetch('/api/session')).status) === 200);
    const until = bob.started + config.refresh_wait_seconds * 1000;
    await bob.page.waitForTimeout(Math.max(0,until-Date.now()));
    check('bob_session_after_initial_token_expiry', await bob.page.evaluate(async()=> (await fetch('/api/session')).status) === 200);
    await bob.page.waitForTimeout(Math.max(0,bob.started + config.session_ttl_seconds*1000 + 1500-Date.now()));
    check('fixed_deadline_401', await bob.page.evaluate(async()=> (await fetch('/api/session')).status) === 401);
    return {schemaVersion:1,status:'passed',browser:browser.version(),persistentProfile:false,certificateTrust:'temporary-context-only',checks};
  } finally { await browser.close(); }
}
(async()=> { try { const result = await run(JSON.parse(fs.readFileSync(0,'utf8'))); process.stdout.write(JSON.stringify(result)); } catch (_) { process.stdout.write(JSON.stringify({schemaVersion:1,status:'failed',stage,diagnostics,error:'browser acceptance failed; sensitive browser detail suppressed'}));process.exitCode=1; } })();
