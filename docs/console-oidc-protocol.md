# OIDC / STS protocol client

This module is the first implementation of parallel work package 03. It supplies
an in-process protocol client, not console routes, a browser login page, or a
complete replacement for legacy Web OIDC. Package 04 must integrate owned storage
clients, request leases, cancellation, expiry, and session revocation.

## Supported profile

`internal/oidc.New(ctx, Config)` accepts administrator-fixed issuer, client ID,
callback URL, STS endpoint, optional client secret, scopes and session policy.
These values must never be selected by a browser request. Discovery must match
the exact issuer. Authorization, token and JWKS endpoints must use HTTPS and the
issuer's exact origin; cross-origin discovery endpoints are deliberately rejected.
All network redirects are rejected. Custom CA transport retains TLS verification;
a standard transport with `InsecureSkipVerify` is rejected. Discovery requires
advertised authorization-code, S256 and RS256 support, and the configured token
endpoint authentication method (`none` or `client_secret_basic`).

The flow is authorization code with PKCE S256 and nonce. `Begin(binding)` generates
authorization URL and a five-minute state; the independent binding comes from
package 04's random HttpOnly browser cookie. A length check cannot establish
entropy: 04 must generate at least 256 random bits and validate cookie scope,
SameSite, Secure, origin and the exact callback route. It must reject duplicate
callback parameters and OAuth error callbacks, and keep tokens out of HTTP/JSON.
At most 1024 pending states are retained, with expired entries reclaimed on Begin.
`Complete(ctx, binding, state, code)` consumes the matching state before any
exchange. A failed, canceled or uncertain exchange cannot be retried with it.

Only signed RS256 ID tokens are accepted. OC verifies signature, exact issuer,
nonempty subject, audience, `azp` for multiple audiences, nonce, expiry, `iat` and
`nbf`. Future `iat`/`nbf` allow at most 30 seconds; expiry has no grace period.
JWKS accepts RSA signing keys of 2048–8192 bits, caches for five minutes and
rate-limits unknown-key refresh to once per second. Concurrent expired-cache
validation shares one cancellable fetch and the same bounded failure result. An unknown key or expired
cache never causes acceptance without verification. An existing `kid` whose
key has changed is rejected until cache expiry; providers should rotate to a new
`kid`. Encrypted tokens, other signing algorithms, userinfo-derived identities,
implicit flow and dynamic endpoint selection are unsupported.

The unsigned `AssumeRoleWithWebIdentity` STS POST sends only the verified ID token,
fixed session policy and API version. It does not send root credentials or request
a longer `DurationSeconds`. The response must contain the same subject, access
key, secret, session token and future expiration. Optional issuer/audience response
fields must match when present. Pinned OtterIO omits these optional fields and
native `ParentUser`; verified `(issuer, subject)` is an OIDC principal, **not** a
native IAM parent access key. Group and native user management must not guess a
mapping from subject/email.

## Grant lifecycle and caller obligations

`Grant.Identity()` returns the verified issuer/subject pair.
`Grant.Credentials()` returns an in-process copy, including session token and
hard expiry, for constructing an owned S3 client. Both Grant and Credentials
reject JSON serialization and redact normal string formatting. Do not extract
and serialize secret fields, URLs or upstream token response bodies yourself.

Expiry is the earliest of ID token expiry, STS expiry and a fixed session deadline
measured from successful login. `MaxSessionLifetime` defaults to one hour and
accepts one minute through 24 hours. Refresh never extends this fixed deadline.
The caller must stop/cancel requests and jobs at expiry, including requests using
a copied credential; this package alone cannot enforce that outside itself.

`Grant.Refresh(ctx)` serializes concurrent exchanges, requires a newly verified
ID token for the same issuer/subject and nondecreasing `iat`, validates nonce if
present, and exchanges it for new STS credentials. Refresh must happen before the
current hard expiry. Its network deadline and final commit check also respect
that old expiry; an exchange cannot revive a grant after it expires. A rotated
refresh token replaces the old one. A confirmed
success without a replacement retains the prior refresh token, as OAuth permits.
An unsupported refresh, missing ID token, transport failure, rejection or
uncertain response closes the grant and requires a new login. It never silently
falls back to startup/root credentials. 04 must revoke the associated browser
session, prevent stale client use and release owned clients on this failure.
`Close()` marks closed and cancels any in-flight refresh immediately; no late
response may commit credentials afterward. It drops credential/refresh references; it does not securely erase Go
strings or remotely revoke STS credentials.

Network responses are capped at one MiB, individual calls default to ten seconds
(configurable up to 30), and New/Complete/Refresh have an overall 30-second bound
in addition to caller cancellation. Errors expose fixed categories, not upstream
bodies, endpoint URLs, codes or credentials.

## Actual server constraints and evidence

OtterIO's `cmd/sts-handlers.go` requires a configured JWT validator and a valid
policy claim (default `policy`) or configured OPA authorization. Its current
OpenID JWT validator verifies signature/time but does not explicitly verify
issuer or audience; OC therefore performs those checks before STS. Session policy
is an authorization intersection, not permission expansion. Server health may
be ready before asynchronous STS initialization; the integration test separately
waits for STS with an intentionally invalid, credential-free token.

On 2026-10-11, this worktree built unmodified OtterIO source
`8cc518b31ac8b69c8f855e19b4bd027f44275c47` with Go 1.27.2. Binary SHA256:
`b3afa6c80b854023b038310aaa7353fe9921f68a4f8ad6ecb2f541953840224a`.
A second run used the verified published OtterIO artifact from
[RELEASE.2026-10-10T16-38-05Z](https://github.com/soulteary/otterio/releases/tag/RELEASE.2026-10-10T16-38-05Z),
with the same source commit and binary SHA256
`48b673ad0e03723eeeedb2ab5773569093b426d8fa04e132d17183ac7afb6f9c`.
A controlled temporary TLS IdP with each actual temporary TLS server passed:

- Signed ID token → actual STS credentials with session token.
- Authorized object read; a session-policy excluded object is denied.
- A broad session policy cannot grant writes beyond upstream `readonly`.
- Same-subject refresh issues usable replacement credentials.

The race-enabled package tests also cover nonce/state/binding rejection and replay,
PKCE parameters, issuer/audience/azp/time rejection, canceled exchanges, key cache
rotation, refresh rotation/failure/missing new ID token/subject change, concurrent
refresh, timeout and output redaction. This evidence does not certify a released
server pair, external production IdP, browser callback integration or console
session expiry/revocation. Those remain work packages 04 and 08.

Run ordinary tests with `go test -race ./internal/oidc`. To repeat actual STS
acceptance, build a chosen clean server and explicitly supply its binary and
source metadata:

```sh
OC_OIDC_TEST_SERVER=/absolute/path/to/otterio \
OC_OIDC_TEST_SERVER_SOURCE=<40-character-source-commit> \
go test -race -v ./internal/oidc -run TestOtterIOSTSIntegration -count=1
```

The source label is operator supplied; the test records the binary hash as well.
All accounts, certificates, objects, policies and processes are disposable. No
production identity provider credentials are needed. Test logs record only STS
status/XML structure, never raw token or credential payloads. Production protocol
basis: [OpenID Connect Core](https://openid.net/specs/openid-connect-core-1_0.html)
and the inspected OtterIO source above.
