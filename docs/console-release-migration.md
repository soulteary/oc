# OC Console release and migration gates

This is the second migration change after the browser-off Compose example.
It prepares publication; it does not publish a release or change OtterIO defaults.

## Distribution contract

The timestamp release builder places `oc` and `oc-console` together in each
of the eleven platform archives listed in `compatibility.json`. Windows uses
`.exe` names. Existing archive names, license files and checksums remain in place.
`oc-console --version` prints the exact release tag. The GoReleaser fallback
also builds both programs. The release image copies both verified executables
from the Linux amd64/arm64 archives; its default entry point remains `oc`.
Start the console with `--entrypoint oc-console`. The release workflow checks
version/help on packaged Linux amd64 and verifies image bytes against archives.
Cross-compilation does not prove native execution on every target.

Before using a published version, confirm its archive contains `oc-console`
or its immutable image digest passes the release verification workflow.
Older published archives and images are not retroactively updated.

## Compatibility and evidence

- Local source baseline: the first migration change `dea77905`, followed by
  CI fixes through `f62ccfbc`. The next release records its own source commit.
- SDK/admin build dependencies: use the exact independent identities recorded
  in `compatibility.json`; those pins are not a claim about a remote server.
- Compose server candidate: `RELEASE.2026-10-09T15-22-07Z`, fixed in
  `deploy/compose.console.yaml`. The browser-off migration result is recorded
  in `docs/console-browser-off-results.json`; use its recorded source/binary
  scope, not historical patch profiles as proof for a different release.
- Historical versions and third-party S3 require their own acceptance.
  Protected configuration, version access and IAM writes require their actual
  capability headers and identity permissions; absence must remain a refusal
  or documented read-only fallback.
- All local browser sessions share the startup alias identity. OIDC login,
  centralized independent-user authentication and Range downloads remain
  outside this migration scope. Existing historical console reports apply
  only to the binaries and server profiles they name.

## Migration notice and rollback

The Compose example opts out of the embedded Web while retaining S3 on 9000,
the internal Admin API on 9001 and OC on host loopback 9090. See the English
and Chinese console guides for installation and explicit write/sharing flags.
Pin both image versions/digests and retain the previous deployment configuration.
Validate object operations, IAM, bucket settings, negative permission cases,
restart persistence and the unavailable legacy routes on the actual deployment.

To restore the embedded Web, set `OTTERIO_BROWSER=on` and recreate OtterIO
without changing its image or data volumes. In the bridge example, publish
9001 to host loopback if browser access is required. Returning to the old UI
does not undo writes, rotated secrets or lifecycle transitions. Do not downgrade
storage without separate data/configuration compatibility verification.

## Gate before changing the server default

Keep OtterIO's browser default unchanged in this PR. Before changing it:

1. Publish installable OC artifacts and verify native execution on the supported
   platforms, both container architectures and a real upgrade/rollback.
2. Record a matrix for actual released OC/OtterIO pairs, single/dual HTTP/TLS,
   separate CAs, IAM identities and browser-off permission regressions.
3. Decide whether independent-user/OIDC deployments remain supported; provide
   their replacement or explicitly announce the change in supported scope.
4. Announce deprecation for at least two notified release cycles and resolve
   migration blockers before changing defaults; retain a bounded opt-in fallback.
5. Delete only Web-specific assets/routes after the fallback window. Preserve
   Admin, S3, STS, health, metrics, node communication and shared authentication.
   Terminal console printing and admin log streams are separate features.
