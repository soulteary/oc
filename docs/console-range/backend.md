# Single-range backend and integration contract

This package adds `consoleapi.RangeBackend` and the `downloadRange` console
helper. It does not register an unauthenticated or separate download endpoint.
The route owner must call the helper only after existing session authentication,
native credential revalidation, request lease, joined session cancellation and
download slot acquisition. Call it for GET with Range/conditional headers and
HEAD; leave unconditional GET on its existing path for backend compatibility.

`OpenRange` takes metadata authorized by the same session and a closed interval.
The SDK GET carries both explicit version (including `null`) and If-Match. Before
streaming, the adapter verifies the exact Content-Range including total length,
Content-Length through SDK object size, ETag, actual version, deletion-marker
state and protected-version capability. Ignored or inconsistent Range is 502,
never a false 206. No full-object buffering or multipart synthesis is used.

The helper supports one closed/open-ended/suffix range. Malformed, multiple,
empty-object and unsatisfiable ranges receive 416 with `bytes */length`. HEAD
ignores Range and reports the full metadata length. If-Match and valid
If-Unmodified-Since precede If-None-Match/If-Modified-Since. If-Range uses strong
ETag equality or exact last-modified date equality; mismatch falls back to a
full pinned GET. Invalid conditional dates are ignored. Request cancellation,
upstream idle bounds, deadlines, final flush and truncation handling reuse the
existing download implementation through an unwrap-capable response adapter.

Validation performed on this branch: console and storageclient package tests
with the race detector; protocol fixtures cover range rejection, selected
version protection, suffix and overflow, conditional statuses, HEAD and
truncated output. Route integration is included and tested through the existing authenticated
endpoint. `buildscripts/test-console-range.py` is a dedicated real-service
probe for integration: caller supplies a disposable native account via
OC_RANGE_ACCESS_KEY/OC_RANGE_SECRET_KEY, exact object fixture and browser TLS CA.
Run separately for each real server/HTTP/TLS/CA/version fixture. Empty objects,
historical/null versions and denied historical versions require additional
integration fixtures; local protocol tests alone do not certify those pairs.

## Formal-server evidence

The disposable matrix passed against the published OtterIO
`RELEASE.2026-10-10T16-38-05Z` binary (SHA256
`48b673ad0e03723eeeedb2ab5773569093b426d8fa04e132d17183ac7afb6f9c`).
Its embedded base revision is `8cc518b31ac8b69c8f855e19b4bd027f44275c47`,
with `vcs.modified=true`; the base does not identify the complete release source.
The tested profiles were single HTTP, split HTTP, and
split TLS with independent S3/Admin CAs. Each profile checked nine ordinary
Range/conditional/HEAD cases on a 1 MiB Unicode/percent-key fixture, two empty
object cases, explicit null version 206, historical-version old bytes 206, and
a restricted account allowed current bytes 206 but denied historical bytes 403.
The browser session used local login codes; native IAM browser login and a
browser-facing TLS matrix were not part of this run. Upstream CA verification
remained enabled and all local requests bypassed proxies.

Reproduce with `buildscripts/test-console-range-real.py --cli ... --console ...
--server ... --server-source ... --output ...` from the repository root. All
configuration, aliases, identities, storage and processes are disposable.
The source argument is a declared source identifier, not binary authentication.
