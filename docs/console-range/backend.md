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
truncated output. No real server combination has been certified before route
integration. `buildscripts/test-console-range.py` is a dedicated real-service
probe for integration: caller supplies a disposable native account via
OC_RANGE_ACCESS_KEY/OC_RANGE_SECRET_KEY, exact object fixture and browser TLS CA.
Run separately for each real server/HTTP/TLS/CA/version fixture. Empty objects,
historical/null versions and denied historical versions require additional
integration fixtures; local protocol tests alone do not certify those pairs.
