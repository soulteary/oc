# OC Web console

[中文](zh_CN/console.md) · [Documentation](README.md) · [Migration plan](console-migration.md)

`oc-console` is a program distributed alongside OC in new timestamp releases, also buildable from source. Its default local mode serves one operator and one configured S3 alias on the local machine. Browsing, downloads, ZIP archives and configuration reads are available in the default read-only mode. Explicit write mode adds bucket creation/empty-bucket deletion, uploads, object deletion and IAM administration. Historical versions, protected settings and policy binding changes require matching server protocols; presigned sharing has a separate opt-in flag. The existing OtterIO Web console remains available.

## Native IAM login over HTTPS (source build)

The opt-in native mode serves multiple browser users against one administrator-configured storage target. Each browser supplies its own IAM access key and secret over HTTPS; OC verifies a signed `GET /otterio/admin/v3/self-credentials` reply with `X-Otterio-Self-Credentials: v1`, `kind: iam` and `status: enabled` before issuing a session. Login does not require listing buckets. Root, STS, service-account and directory identities are refused, as are legacy/unknown discovery replies. Sessions use separate storage clients and host-only Secure, HttpOnly, SameSite=Lax cookies. Secrets stay in process memory and are cleared from the form upon submission or page suspension; OC does not save them to browser storage, cookies, CLI configuration or preference files.

Build from the source containing this feature; the previously pinned release/Compose example does not gain native login automatically:

```sh
make build-console
./oc-console --auth-mode native \
  --s3-url https://s3.example.com --admin-url https://admin.example.com \
  --address 0.0.0.0:9090 --public-url https://console.example.com:9090 \
  --tls-cert /etc/oc/public.crt --tls-key /etc/oc/private.key \
  --data-dir /var/lib/oc-console
```

The browser connection uses TLS directly in OC. Storage and management endpoints must also use HTTPS; add `--s3-ca` and `--admin-ca` for private CAs. Redirects and browser-selected storage endpoints are refused. Forwarded headers do not establish a trusted HTTPS connection. Use a single OC instance for this first phase: sessions are in memory, expire after 30 minutes and are lost on restart. The global limit is 16 sessions, with at most four per native user and 30 login attempts per minute across the instance; existing global API/download/archive limits still apply. Without `--data-dir`, preferences last only while at least one session for that user remains active. Persistent preference filenames hash both configured endpoints, the authentication kind and the access key; different users do not share preferences, and changing a secret does not change that identity.

Native mode is read-only in this phase: `--allow-writes` is rejected. Bucket/IAM/configuration reads, downloads and ZIP archives continue to use each user's server-enforced permissions; optional sharing requires `--allow-sharing --share-url https://...`. Native mode does not read an alias/config directory or accept local login codes. Local mode and its explicit write support remain available. Shared writes/credential rotation, OIDC, session revocation on credential changes and multi-instance deployment need later migration changes. Keep the old OtterIO Web console and rollback path until those gates and release acceptance are complete.

[Recorded TLS acceptance](console-native-https-results.json) names the tested binaries, SDK pin and patched server profile; it is not proof for a different published server or OC release.

## Console navigation

After login, Overview shows the capacity reported for buckets accessible to the current identity, the listed bucket count, session upload/deletion task count, and recently visited buckets. Capacity comes from the account API; upstream controls its scope and freshness. Unavailable summaries display `—` rather than estimated totals. Monthly traffic, request trends, and alerts are not connected.

Use the sidebar to switch between Overview, Buckets, Tasks, and Account. The top navigation button collapses the sidebar. The bucket directory provides a searchable table with permission summaries, capacity, creation dates, refresh, and creation. Select a bucket to open its files; the back button returns to the directory. Configuration actions use the selected bucket. Object operations and bucket settings remain in the bucket browser; upload and deletion results appear in Tasks. ZIP preparation still uses the browser's ZIP task button. Switching pages preserves loaded locations and tasks. Recent visits are saved per storage identity and restored after login. Narrow screens use horizontal navigation and a single-column overview.

## Published Docker image

The bridge deployment in `deploy/compose.console.yaml` pins OC
`RELEASE.2026-10-10T11-42-18Z` by its published multi-platform image digest.
Both initialization and Console use that same image. No local build is required.
Run `docker compose pull`, start OtterIO and wait for readiness, then run
`docker compose run --rm oc-init` and `docker compose up -d --no-deps oc`.
Preserve existing data/configuration directories; initialization changes the
`store` alias. The S3/Admin ports shown in the example do not override your
existing configured endpoint addresses.

The pinned OtterIO release does not expose the conditional IAM creation
protocol required for Console user/group creation. Full IAM feature acceptance
therefore remains blocked. See [released-pair evidence](console-released-pair.md).

## Optional local Docker image

The root `Dockerfile` builds and includes both `oc` and `oc-console`, retaining `oc` as its default entrypoint. `Dockerfile.release` also includes both programs; `Dockerfile.dev` remains CLI-only.

```sh
docker build -t soulteary/oc:local-console .
docker run --rm --entrypoint oc-console soulteary/oc:local-console --help
docker run --rm -it --network host \
  -v "$HOME/.oc:/config:ro" \
  -v "$PWD/oc-console-data:/app-data" \
  --entrypoint oc-console soulteary/oc:local-console \
  --config-dir /config --data-dir /app-data --alias store --address 127.0.0.1:9090
```

Configure the `store` alias first. Add the write/sharing flags when needed. By default the console binds to loopback; ordinary port publishing cannot reach that listener. For bridge networking use `deploy/compose.console.yaml` with `--container-listen`, `--public-url` and a writable data volume. Use host networking on Linux or enable host networking in Docker Desktop on macOS. Mount any private CA files and use paths that are valid inside the container.

## Build and start

Use the Go version in `go.mod`. No Node runtime or frontend dependencies are required.

```sh
make build-console
./oc-console --alias store
./oc-console --alias store --allow-writes
./oc-console --alias store --allow-writes --max-upload-size 268435456
./oc-console --alias store --allow-writes --allow-sharing \
  --share-url http://127.0.0.1:9000 --max-archive-size 5368709120
```

Configure `store` using `oc alias set`. Open the exact URL printed at startup and enter the printed login code. Sessions last 30 minutes. Logout cancels the session's requests and tasks; the process login code remains valid until it stops. Protect that code as access to the selected alias.

The default listener is `127.0.0.1:9090`; literal loopback IPv6 is also supported. This is a single-identity local service. Do not expose it using port forwarding, a tunnel or a reverse proxy as a shared console.

```sh
./oc-console --config-dir /path/to/oc-config --alias store \
  --address 127.0.0.1:9091 \
  --s3-ca /path/to/s3-ca.pem \
  --admin-url https://admin.example:9001 \
  --admin-ca /path/to/admin-ca.pem
```

## Configuration and trust

The program reads an existing version-10 `config.json` without modifying it. Config directory precedence is `--config-dir`, `OC_CONFIG_DIR`, `MC_CONFIG_DIR`, then `~/.oc` on Unix or `oc` in the Windows user profile. No local filesystem browser or alias editor is provided.

Management URL precedence is `--admin-url`, `OC_ADMIN_URL_<alias>`, `OC_ADMIN_URL`, stored `adminURL`, then the S3 URL. Management endpoints must be root URLs; redirects are rejected. Management CA precedence is `--admin-ca`, `OC_ADMIN_CA_<alias>`, `OC_ADMIN_CA`, stored `adminCAFile`, then the selected S3 trust pool. An explicit management CA builds system trust plus that file independently from S3 trust.

S3 trust normally includes system roots and `<config-dir>/certs/CAs`. `--s3-ca` replaces that custom directory with system roots plus the supplied file. Certificate verification stays enabled. Relative file paths use the working directory; prefer absolute paths. `OC_HOST_<alias>` and `MC_HOST_<alias>` overrides are rejected. Stored temporary session tokens are supported without automatic refresh.

The startup alias credentials remain in OC. Browsers receive an HttpOnly/SameSite=Strict cookie and a separate CSRF token. Newly generated IAM credentials are displayed once; existing credentials are never returned. Credentials and login codes are not persisted in localStorage. Restart after changing an alias or CA.

## Upload semantics

Create-only is the default. A missing-key HEAD must advertise `X-Otterio-Conditional-Writes: v1`, and the server must enforce `If-None-Match: *` under the destination write lock for PUT and multipart completion. The pinned server provides this guarantee for FS and single-pool erasure storage. Gateways, multiple pools and write-back cache reject conditional writes. A HEAD existence check alone is not atomic; servers without this capability reject default uploads.

Explicit replacement has a separate confirmation with the exact bucket and complete key. It can replace the current object at execution time or add a version; it is not an ETag compare-and-swap. Write-only identities may enter a bucket/key manually and explicitly allow replacement without listing or HEAD permission. OC never substitutes an administrator for these checks.

The browser sends the raw file through OC. Browser progress at 100% means sending finished; success requires upstream commit confirmation. The default file limit is 1 GiB, configurable in bytes from 1 through 5 GiB. Empty files are valid; unknown lengths are rejected. Two writes may run concurrently. Multipart transfers use two fixed 16 MiB buffers per upload: reading and hashing the next part overlaps sending the previous part, with only one upstream part request active. Only the ID they created is aborted.

Cancellation, page departure, logout and shutdown stop pending transfer work and attempt owned multipart cleanup. They cannot undo an already committed object. A lost commit response is reported as unconfirmed; failed cleanup is reported explicitly. Check the object and unfinished upload before retrying. Final commits and deletes are not automatically retried.

## Deletion and task semantics

Delete an exact entered key, selected loaded objects, or a nonempty slash-terminated prefix. Prefix planning lists a fixed snapshot using the selected identity, capped at 1000 objects. Listing failure, malformed pagination or overflow results in no deletion. Exact-key deletion does not require listing permission.

Confirmation includes the bucket, scope, count and exact keys. The token belongs to that session/plan and is single-use. New keys created after planning are not added. A listed key replaced before execution can still be deleted: this is a key snapshot, not a version snapshot.

No version ID or retention bypass is sent. Versioned buckets normally receive a delete marker while old versions remain. Unversioned deletion cannot be undone by this console. Each key reports success, failure, unknown outcome or pending/unexecuted work; cancellation does not roll back completed deletions.

Waiting uploads expire after one minute; ready plans after ten minutes. Running tasks are bounded by the session. Refresh restores status without restoring confirmation tokens or retransmitting files. Finished results are retained for up to ten minutes; capacity pressure evicts the oldest completed results first. Limits are 16 tasks per session and 64 per process; active tasks are never evicted to make room.

Leaving or reloading the page in the browser interrupts browser uploads; the console's Refresh button does not interrupt uploads. Confirmed background deletion can continue. Cancel the task or log out to stop deletion. Page navigation does not undo work.

## Bucket settings and own account

Configuration reads work in the default read-only mode. Changes and secret
rotation require restarting with `--allow-writes`, the selected identity's
permissions and the matching server protocol; enabling the flag alone does not
grant access or add server capabilities.

To change a setting:

1. Open **Bucket settings**, enter the exact bucket, choose policy, versioning or lifecycle, and select **Read setting**.
2. Edit the complete returned document. Select **Review change**, check the loaded bucket, setting and full contents, then **Apply this setting**. For an existing policy or lifecycle, **Review removal** offers a separate removal confirmation.
3. Check the confirmed document returned by storage. After a conflict or uncertain result, copy edits you need to keep, read the current configuration and review a fresh change before submitting again.

Enter an exact bucket in Settings, including when bucket enumeration is denied. Read bucket policy as complete JSON, and versioning/lifecycle as complete XML. Each setting has its own authorization and errors; OC submits the full document; storage validates supported fields and may normalize its representation. Protected writes reject unsupported fields rather than silently discarding them. This server implements versioning Status, without MFADelete or excluded prefixes. Replacements and policy/lifecycle removal require a separate review of the exact bucket, kind and contents. Policy changes affect access, lifecycle rules may expire data, and enabling versioning is not reversed by suspension. Version configuration removal is unavailable.

Protected saves require the corresponding read permission as well as the
mutation permission: OC reads before submission and again to verify the saved
document. Policy uses `s3:GetBucketPolicy` and `s3:PutBucketPolicy` or
`s3:DeleteBucketPolicy`; versioning uses `s3:GetBucketVersioning` and
`s3:PutBucketVersioning`; lifecycle uses `s3:GetLifecycleConfiguration` and
`s3:PutLifecycleConfiguration` for both replacement and removal. A policy that
revokes your own read access can be committed but fail read-back verification,
leaving the outcome unconfirmed. Check the actual setting with an identity that
can read it before making another change; do not replay the submitted write.

Reading over an unsaved draft requires confirmation. Canceling or a failed read keeps the previous document and its exact save scope. The lifecycle profile requires `lifecycle-storage.patch` followed by `lifecycle-storage-hardening.patch`, applied after `console-server-base.patch`; it supports current and noncurrent version transitions, persistent destination references, and version-specific restore on erasure storage; see [runtime scope and verification](lifecycle-transition.md). The base-only conditional lifecycle profile accepts prefix expiration and noncurrent expiration; tag filters, transitions and expired-delete-marker rules require the storage profile or are rejected. Existing documents remain fully readable, and ordinary unconditional S3 writes preserve the pin behavior. The pinned, unpatched server still refuses protected writes. `NewerNoncurrentVersions` remains unsupported in the combined profile. A tag filter combined with `ExpiredObjectDeleteMarker=true` is also rejected. Invalid UTF-8 and unpaired UTF-16 escapes are refused before a secret or configuration can silently change during decoding.

Protected writes require the server's `X-Otterio-Bucket-Config: v1`, a 64-hex revision and an exists flag. A signed `X-Otterio-Config-If-Match` is checked under the complete metadata transaction lock. Conflicts retain the draft but require a fresh read and review; uncertain outcomes require checking storage before another edit. Writes are never replayed automatically. Documents are limited to 1 MiB, with the server's existing tighter limits still applying. FS protects policy/lifecycle; single-pool erasure also protects versioning. Gateways, multiple pools, V2 signing and servers without the protocol cannot perform these protected writes.

ILM destination registration, credential updates and removal use
`oc admin bucket remote` in the terminal, as described in the
[transition guide](lifecycle-transition.md). The Web editor changes lifecycle
rules but does not manage remote targets or provide historical-version restore
buttons. The transition protocol is limited to single-node, single-pool erasure.

Own-account discovery returns only identity kind, status and rotation availability. It never returns an access key, secret or session token. Only enabled native IAM users can rotate, subject to explicit `admin:CreateUser` denial. Root, STS, service/directory identities and distributed, etcd or external OPA authorization deployments cannot rotate here. Secrets must contain 8–128 UTF-8 bytes without NUL, CR or LF. Secret input is cleared upon submission, cancellation, close and logout.

To rotate an eligible account, finish or cancel active writes, select **Account**
to open **Current account**, enter **New secret key**, select **Review secret rotation**, then
**Rotate secret and stop console**. Verify the resulting credentials in the
terminal, update the alias using `oc alias set`, restart `oc-console`, and open
the newly printed URL with its new login code. Preserve the alias's S3,
management, addressing and trust settings when updating it.

Finish or cancel active writes before rotation; the process rejects rotation concurrent with object/settings writes or multipart cleanup. A confirmed or uncertain submitted rotation retires every session and pending task, flushes a restart acknowledgement, and stops the console. Verify credentials and update the alias in your terminal, then restart and reload the page; OC never rewrites the alias automatically. A definite permission rejection does not itself retire the connection. Retirement covers this OC process, without claiming revocation of previously issued STS or service-account credentials; manage those identities separately.

The pinned server dependency, `6f6d0835ddff68020f1491c403b958fade22841f`, does not contain P3. On a writable copy, apply [console base](../buildscripts/console-server-base.patch) for protected settings and own IAM rotation. For transition execution and restore, apply [lifecycle storage](../buildscripts/lifecycle-storage.patch) and mandatory [lifecycle hardening](../buildscripts/lifecycle-storage-hardening.patch). Current HEAD80 already contains P3 and twelve additional storage-hardening source/test paths, while the independent conditional IAM patch is absent. Do not apply these export patches again there or claim equality with the fixed-pin composition. The frozen `console-server-p3.patch` and its reports remain historical evidence. See [development](development.md#reproduce-the-optional-console-protocol-fixture) for separate builds and acceptance. Building OC does not upgrade storage; older servers retain reads while protected mutations remain disabled.

Stopping OC or returning to the old UI keeps the current server and does not
undo configuration changes, rotated secrets or transitioned objects. New
transition references require a server that understands them; validate storage
compatibility before any server downgrade.

## Buckets, versions, archives, sharing and IAM

Write mode enables **Create bucket** and **Delete empty bucket**. Deletion requires typing the exact name and uses the server's atomic emptiness check. It never clears objects, historical versions, deletion markers or multipart uploads.

**Versions** lists one exact object's historical versions and deletion markers with pagination. Select a version for download, sharing or ZIP; deletion markers cannot be downloaded, and `null` is an explicit version ID. The server must advertise `X-Otterio-Version-Authorization: v1`, authorize `s3:ListBucketVersions` and `s3:GetObjectVersion` separately, and evaluate the actual version ID. Older servers are rejected without a fallback to the current object. Versioning requires an erasure deployment; FS cannot enable it. Version restoration and permanent historical deletion are outside this interface.

**ZIP selected** and **ZIP prefix** also work in read-only mode. A prefix is completely scanned before preparation, with at most 1000 objects and 5 GiB of source bytes by default; lower the byte limit with `--max-archive-size`. Current objects use ETag-conditional reads; explicit historical references retain their version ID. Changed or incomplete objects fail the whole archive. OC builds a complete ZIP before exposing a single download. Safe unique archive paths are mapped back to original buckets, keys, versions and ETags in `manifest.json`.

Only one archive can be preparing or awaiting download. It expires after ten minutes; download completion, cancellation, logout and normal shutdown delete the private 0600 temporary file. **Cancel** closes blocked source reads. Closing the dialog preserves access to the session-owned task; page reload restores an active archive through the session-owned list. Lost create/cancel replies trigger reads without replaying the request. `--archive-dir` selects its temporary directory; a process killed without cleanup can leave a file for the local temporary-file cleanup policy.

Enable **Share** with `--allow-sharing --share-url <recipient-accessible S3 root URL>`. URL precedence is the flag, `OC_SHARE_URL_<alias>`, then `OC_SHARE_URL`. A GET link targets one object or exact version, defaults to one hour and allows one second through seven days, with an optional download filename. OC signs the recipient host directly without contacting it; editing the host afterward invalidates the signature. Links are absent from task history and browser persistent storage. Possession grants access, logout does not revoke issued links, and temporary credentials or account changes can shorten their lifetime.

**IAM management** lists users, groups, service accounts and named policies. Write mode supports native user creation/status/secret/deletion, group creation/membership/status/deletion, and service-account creation/status/secret/restriction-policy/deletion, subject to the selected identity's server permissions. Root and directory identities retain the server's restrictions. User-deletion preflight requires no groups or service accounts; deletion cascades through STS, bindings and relationships, including concurrent associations created after preflight. Groups must be empty. Disabling a group does not remove direct or other-group permissions. Newly generated secrets appear once and are cleared on close, page departure or logout; existing secrets are never returned. Supplying a service-account secret also requires an access key; leave both empty to generate and show the complete pair once.

User/group policy bindings use a read revision followed by a confirmed conditional replacement. The server compares revisions, checks policy existence and validates the native target under the IAM store lock. Conflicts require a fresh read, and writes are never replayed. Older servers retain read-only policy inspection. Changes that invalidate the selected identity, and submitted IAM changes with an uncertain result, stop OC; verify credentials/permissions in the terminal before restarting.

The five-feature server uses [console base](../buildscripts/console-server-base.patch), then [version authorization](../buildscripts/console-features-server.patch) and [conditional IAM](../buildscripts/console-iam-bindings.patch). Its primary acceptance excludes lifecycle storage; a separately built server adds [lifecycle storage](../buildscripts/lifecycle-storage.patch) and mandatory [lifecycle hardening](../buildscripts/lifecycle-storage-hardening.patch) for composition regression. IAM reads, user status/deletion, group membership/status/deletion and service-account operations use existing admin APIs; user/group creation, native-user secret rotation and binding replacement require the advertised IAM protocols. Building OC does not upgrade your server. See [feature validation](console-features.md) for source identities, current report status and preserved historical evidence.

The version/object authorization patch derives `s3:ExistingObjectTag/<key>` only from stored tags, filters injected header/query/claim values, and rechecks GET/HEAD and copy-source access after actual metadata is loaded and before conditional responses or destination commits. Request tags keep their request origin. Existing tagging APIs keep their actions; PUT/DELETE authorization is checked again under the object write lock. Native route/storage and race regressions, standalone and full five-feature/copy acceptance have passed. Full composition lifecycle acceptance has also passed locally. The console base profile remains unchanged.

## Browsing and operational limits

Every object operation uses the selected identity. Bucket-root Read/Write hints are informational and do not authorize specific keys or prefixes. Root AccountInfo is supported by the pinned server; unavailable management information does not block S3 operations.

Downloads stream through OC into the browser's download manager. Errors open separately, preserving the console; the session is checked before starting. Range, OIDC and centralized deployment remain outside this milestone.

Process limits are 16 sessions, 8 ordinary storage requests, 2 downloads, 2 prefix scans and 2 writes. S3 metadata/delete calls have a 15-second overall deadline; ordinary console metadata has an additional 30-second limit and prefix scanning a 60-second limit. JSON reads have a 10-second limit. Upload reads and download reads/writes use 30-second progress/idle limits, allowing slow uploads that keep making progress.

Shutdown cancels sessions, then allows up to five seconds each for HTTP shutdown and background cleanup, approximately ten seconds in the worst path. The normal five-second cancellation budget is measured in acceptance, not a throughput guarantee. Host/Origin checks require the printed URL. Object keys are preserved without filesystem path normalization.

## Verification and migration

```sh
make test-console
```

See [feature validation](console-features.md), [phase-three validation](console-phase-three.md), the later [transition verification](lifecycle-transition-verification.json), and [phase-two validation](console-phase-two.md) for exact evidence and remaining scope. Each report applies to its recorded binaries and patches; historical reports are preserved. Frontend behavior tests require Node in the development environment; running the console does not. `make build` still builds the CLI only; the timestamp release builder now packages both executables and the release image includes `oc-console`. Previously published releases are unchanged. See [release and migration gates](console-release-migration.md). Diagnostics, release/deployment validation and retirement of the old UI remain later migration gates.

## Language and user preferences

The header switches between English and Chinese. After sign-in, the language, favorite object references and 20 most recently visited buckets are saved per storage identity (S3 endpoint plus access key). Files use SHA-256 identity names and mode 0600; they contain no credentials, sessions or login codes. Renaming an alias or rotating its secret does not change its preferences. This remains a single-identity process; all sessions opened with its login code use that identity.

`--data-dir` selects a writable application data directory, defaulting to `<config-dir>/console-data`. Preferences remain writable in storage read-only mode. Save failures are displayed. The bridge Compose example mounts `./oc-console-data:/app-data` and passes `--data-dir /app-data`, keeping the OC configuration mount read-only.

## Use OC as the primary UI with the legacy Web disabled

The Compose example sets `OTTERIO_BROWSER=off`, disabling OtterIO's embedded page and legacy Web RPC/upload/download/ZIP routes. S3 remains on 9000; the internal Admin API remains on `--console-address :9001` and OC uses `--admin-url http://otterio:9001`. Open OC at `http://127.0.0.1:9090`. Keep the management listener. This changes only the deployment example, not OtterIO's server defaults.

Back up the deployment configuration, retain the current OtterIO image version, and pull the pinned released OC image from the Compose example (or explicitly build a local image for development). Verify that the legacy page is unavailable, then check browsing, uploads/downloads, deletion, ZIP, bucket settings and IAM in OC. Protected writes and historical versions still require server capabilities and identity permissions. Restart OC and verify language, favorites and recent visits persist.

This is a single-identity local console: every session uses the startup alias identity. Deployments requiring independent user login, OIDC or centralized multi-user access should retain the legacy Web until those migration capabilities are available. The pinned release includes `oc-console`; older published images may not.

To roll back, set `OTTERIO_BROWSER` to `'on'` and recreate storage with `docker compose up -d --no-deps otterio`; do not downgrade the server. The example does not publish 9001. To access the legacy page, temporarily add `127.0.0.1:9001:9001` and open `http://127.0.0.1:9001/otterio/`. Rollback does not undo configuration writes, credential rotation or object operations. Disable the legacy Web and remove the temporary port mapping when returning to OC.
