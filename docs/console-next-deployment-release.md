# Deployment acceptance, 2026-10-12

This run validates the existing pinned published pair, not the new OIDC source or a new release candidate. The unchanged Compose recipe starts a disposable project with random host ports, temporary credentials and directories. It never uses the operator’s existing alias, account, data or containers.

## Verified scope

- OC `RELEASE.2026-10-10T11-42-18Z` and OtterIO `RELEASE.2026-10-09T15-22-07Z`, immutable image digests recorded in [raw evidence](console-next-deployment-compose-results.json).
- Both running images are `linux/arm64`; actual image architecture is inspected rather than inferred from the host or manifest tag.
- Single instance, local alias login over host loopback HTTP, private Compose bridge for storage/Admin. This is one shared startup identity, not independent users.
- Real object bytes survive storage container recreation with the same image and data volume, embedded UI restoration, and return to browser-off. Console preferences survive its own restart.
- Embedded page, RPC, upload, download and ZIP routes fail with storage-protocol rejections; S3 and Console access remain available. The Admin listener is private.
- Temporary project cleanup passed. Evidence includes recipe and harness SHA-256, exact immutable references, and no credentials/login codes/object secrets.

Reproduce: `python3 buildscripts/console_released_compose_acceptance.py --output /tmp/oc-compose-results.json`. Requires Docker Compose and the pinned published images. Five focused harness unit tests pass: `python3 -m unittest discover -s buildscripts -p test_console_released_compose.py`.

## Decision gates

**Trial deployment:** verified for the above local/shared-identity arm64 scope.

**Default-off:** not approved. Independent native/OIDC identity, TLS/CA and policy context, actual amd64 execution, old-version upgrade, complete IAM protocol parity, resource stability and notification windows remain unverified in this run.

**Deletion:** not approved. The verified UI rollback still depends on embedded Web code. Keep that fallback until replacement releases and the notified fallback window have completed.

The recreation is not a server downgrade or old-version upgrade. No data/configuration compatibility claim is made across server versions. OIDC profile acceptance should use an isolated temporary IdP and fixed HTTPS issuer/client/callback; it awaits the authentication integration and profile contract. No existing user IdP or environment was accessed. Auditing and proactive external permission-change detection require lifecycle hooks from the authentication/identity tasks and remain separate work.
