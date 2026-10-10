# Published OC/OtterIO browser-off acceptance — 2026-10-11

## Fixed release identities

OC `RELEASE.2026-10-10T11-42-18Z`, source
`ee1846d587a6548db4e595d653ca4f54baea2b1a`; OtterIO
`RELEASE.2026-10-09T15-22-07Z`, source
`4022b784cbb2e1d778411e8385f1e885d0c14216`.
Both macOS arm64 binaries were downloaded from the published releases and their
SHA-256 values verified against the published manifest/checksums. OC Console's
version/help were executed. Reports include actual program hashes and build info.
Build dependency identities are not substituted for server identities.

The Compose example now pins both published multi-platform image digests:

- OC: `sha256:f4a042ae7b9a2fb3b32c8984d36a53aea77cb69bd8469002f3cfb11d1b246dd6`
- OtterIO: `sha256:7ddf211748d11bb8f6af23576f19eaf5b2675f70f647e38ed9fdd14912824ba8`

Its initialization and Console use the same OC image. Source builds remain
optional. The server version is not upgraded by this deployment change.

## Passing acceptance

[Object writes and protected settings](console-released-pair-settings-results.json):
33 acceptance groups in each of single HTTP, dual HTTP, single TLS and dual TLS
(132 total). Includes the base browser-off/authentication/permission checks,
streaming downloads, unusual keys, pagination, upload/delete and protected bucket
settings/own-account rotation. Dual TLS uses independently generated CA/keypairs.
All fixtures use temporary identities and disposable four-disk erasure storage.

[Version copy authorization](console-released-pair-copy-results.json):
13 acceptance groups per entry configuration (52 total). These results separately
exercise version-aware copy behavior and do not certify all Console IAM features.

[Published-image Compose acceptance](console-released-compose-results.json):
Docker Desktop Linux arm64, isolated project, random host ports and disposable
credentials/volumes. Valid configuration, login/bucket reads through the bridge,
language/favorites/recent persistence across Console restart, unavailable legacy
page/RPC/upload/download/ZIP, same-image same-volume `OTTERIO_BROWSER=on` rollback,
and return to browser-off with retained OC access passed. Admin stays internal.
This is a UI configuration rollback, not storage downgrade or a comprehensive
upgrade from every previous deployment. Only the arm64 image was executed locally.

## Preserved failures and migration blockers

The [combined settings/features attempt](console-released-pair-combined-failed-results.json)
failed its null-version expectation: settings acceptance had already enabled
versioning on the shared bucket, violating the features fixture's assumption that
its initial object predates versioning. Independent fixtures avoid that interaction;
the failed record remains available rather than being overwritten with success.

The [independent features attempt](console-released-pair-features-results.json)
passed 16 groups in single HTTP, including history, sharing and ZIP checks, then
failed the `GET /otterio/admin/v3/iam-create` capability expectation with HTTP 400
`InvalidRequest`. The pinned server does not implement this conditional IAM creation
protocol. This is a genuine released-pair capability gap, not resolved by separating
fixtures. Full IAM creation/binding migration is not accepted; do not infer coverage
for later cases or other entry configurations from this partial report. Existing
admin reads/operations and protected settings retain their own recorded scope.

Independent-user/OIDC login, centralized deployment, Range, native execution on
all target platforms, amd64 container runtime, full previous-version upgrades and
the notified deprecation/fallback windows remain pending. Keep OtterIO's browser
default and opt-in fallback. Do not remove legacy Web based on these results.

## Reproduction

Use the actual downloaded binaries, verify checksums first, and invoke
`buildscripts/test-console-integration.py` with `--cli`, `--console`, `--server`,
`--server-source 4022b784cbb2e1d778411e8385f1e885d0c14216`, `--output` and
`--scenarios single-http,dual-http,single-tls,dual-tls`.
Run `--writes --settings` and `--version-copy` independently. The full
`--features` profile is expected to expose the recorded IAM gap on this server.
Do not combine settings and features on a shared null-version fixture.

To rerun the disposable Compose checks after pulling the pinned images:

```sh
python3 buildscripts/console_released_compose_acceptance.py
```

This writes its report and removes the temporary Compose project. It requires
Docker Desktop/Compose and host-loopback connectivity; it never reads the user's
OC configuration. Example ports are not requirements for existing deployments.

OtterIO's first deprecation-notice release was triggered separately with tag
`RELEASE.2026-10-10T16-38-05Z`, source
`8cc518b31ac8b69c8f855e19b4bd027f44275c47`. This is a distinct server identity;
these acceptance results do not certify that new release. A pushed tag is not a
published release; verify its final workflow/release status before counting a
notified release cycle.
