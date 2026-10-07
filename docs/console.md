# Local Web console

[中文](zh_CN/console.md) · [Documentation](README.md) · [Migration plan](console-migration.md)

`oc-console` is an optional source-built program for one operator and one configured S3 alias on the local machine. Browsing and downloads are read-only by default. Explicit write mode adds uploads, exact-key/batch/prefix deletion, cancellation and per-object results. The existing OtterIO Web console remains available.

## Build and start

Use the Go version in `go.mod`. No Node runtime or frontend dependencies are required.

```sh
make build-console
./oc-console --alias store
./oc-console --alias store --allow-writes
./oc-console --alias store --allow-writes --max-upload-size 268435456
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

Storage credentials remain in OC. Browsers receive an HttpOnly/SameSite=Strict cookie and a separate CSRF token. Credentials and login codes are not persisted in localStorage. Restart after changing an alias or CA.

## Upload semantics

Create-only is the default. A missing-key HEAD must advertise `X-Otterio-Conditional-Writes: v1`, and the server must enforce `If-None-Match: *` under the destination write lock for PUT and multipart completion. The [conditional-write patch](../buildscripts/otterio-conditional-writes-compat.patch) provides this guarantee for FS and single-pool erasure storage. Gateways, multiple pools and write-back cache reject conditional writes. A HEAD existence check alone is not atomic; servers without this capability reject default uploads.

Explicit replacement has a separate confirmation with the exact bucket and complete key. It can replace the current object at execution time or add a version; it is not an ETag compare-and-swap. Write-only identities may enter a bucket/key manually and explicitly allow replacement without listing or HEAD permission. OC never substitutes an administrator for these checks.

The browser sends the raw file through OC. Browser progress at 100% means sending finished; success requires upstream commit confirmation. The default file limit is 1 GiB, configurable in bytes from 1 through 5 GiB. Empty files are valid; unknown lengths are rejected. Two writes may run concurrently. Multipart transfers use two fixed 16 MiB buffers per upload: reading and hashing the next part overlaps sending the previous part, with only one upstream part request active. Only the ID they created is aborted.

Cancellation, page departure, logout and shutdown stop pending transfer work and attempt owned multipart cleanup. They cannot undo an already committed object. A lost commit response is reported as unconfirmed; failed cleanup is reported explicitly. Check the object and unfinished upload before retrying. Final commits and deletes are not automatically retried.

## Deletion and task semantics

Delete an exact entered key, selected loaded objects, or a nonempty slash-terminated prefix. Prefix planning lists a fixed snapshot using the selected identity, capped at 1000 objects. Listing failure, malformed pagination or overflow results in no deletion. Exact-key deletion does not require listing permission.

Confirmation includes the bucket, scope, count and exact keys. The token belongs to that session/plan and is single-use. New keys created after planning are not added. A listed key replaced before execution can still be deleted: this is a key snapshot, not a version snapshot.

No version ID or retention bypass is sent. Versioned buckets normally receive a delete marker while old versions remain. Unversioned deletion cannot be undone by this console. Each key reports success, failure, unknown outcome or pending/unexecuted work; cancellation does not roll back completed deletions.

Waiting uploads expire after one minute; ready plans after ten minutes. Running tasks are bounded by the session. Refresh restores status without restoring confirmation tokens or retransmitting files. Finished results are retained for up to ten minutes; capacity pressure evicts the oldest completed results first. Limits are 16 tasks per session and 64 per process; active tasks are never evicted to make room.

Leaving or reloading the page in the browser interrupts browser uploads; the console's Refresh button does not interrupt uploads. Confirmed background deletion can continue. Cancel the task or log out to stop deletion. Page navigation does not undo work.

## Browsing and operational limits

Every object operation uses the selected identity. Bucket-root Read/Write hints are informational and do not authorize specific keys or prefixes. Root AccountInfo needs the [account-info patch](../buildscripts/otterio-account-info-compat.patch); unavailable management information does not block S3 operations.

Downloads stream through OC into the browser's download manager. Errors open separately, preserving the console; the session is checked before starting. Version selection, Range, ZIP, presigned sharing, OIDC, management editing and centralized deployment remain outside this milestone.

Process limits are 16 sessions, 8 ordinary storage requests, 2 downloads, 2 prefix scans and 2 writes. S3 metadata/delete calls have a 15-second overall deadline; ordinary console metadata has an additional 30-second limit and prefix scanning a 60-second limit. JSON reads have a 10-second limit. Upload reads and download reads/writes use 30-second progress/idle limits, allowing slow uploads that keep making progress.

Shutdown cancels sessions, then allows up to five seconds each for HTTP shutdown and background cleanup, approximately ten seconds in the worst path. The normal five-second cancellation budget is measured in acceptance, not a throughput guarantee. Host/Origin checks require the printed URL. Object keys are preserved without filesystem path normalization.

## Verification and migration

```sh
make test-console
```

See [phase-two validation](console-phase-two.md) for exact evidence and remaining scope. `make build` still builds the CLI only; existing release archives and containers do not automatically include this experimental program. Account/management functionality, release/deployment validation and retirement of the old UI remain later migration gates.
