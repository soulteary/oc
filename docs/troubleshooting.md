# Diagnose a failing command

[简体中文](zh_CN/troubleshooting.md) · [Configuration](configuration.md) · [Compatibility](compatibility.md)

Start with the version, selected configuration and the exact operation that
failed. Keep a failing transfer's output and exit status; the presence of a
local file or a remote object is not proof that the transfer completed.

## Collect a small diagnostic report

```sh
oc --version
oc --json doctor
oc --json doctor store
```

Doctor defaults to offline mode and sends no server request. Successful reports
include client/Go versions, platform and whether certificate verification is
enabled. `adminSDK` identifies the embedded OtterIO server/admin module; it
does not identify the independent S3 SDK. Endpoint schemes, separate-management
and custom-CA settings describe a supplied alias; running doctor without one
does not check an endpoint. Reports omit endpoint hosts, private paths,
credentials and the configuration directory.
Review failure diagnostics too: general error records can contain local paths,
host information and underlying error messages.

For a configured alias, online mode queries management ServerInfo with a
15-second deadline:

```sh
oc --json doctor --online store
```

It requires management permission and does not validate every object operation.
Online failure reports an error category/code and returns nonzero; offline
success does not establish DNS, network or certificate validity. If you use a
non-default configuration, include the same `--config-dir` on every command.

## OC sees the wrong alias or configuration

Run `oc alias list store` locally and check directory selection:
`--config-dir` > `OC_CONFIG_DIR` > `MC_CONFIG_DIR` > platform default.
The list only shows the saved entry, including its saved management settings;
it does not show command or environment overrides. An environment-only alias
can work even when `alias list` reports no saved alias. Removing a saved alias
does not disable its environment override.
An `OC_HOST_store` or `MC_HOST_store` value overrides saved S3 settings. Check
whether an override is set without printing its secret value. A set OC value
can suppress its MC counterpart even when empty. Unset unintended overrides,
then run offline doctor again.

If management stopped working after an `alias set`, check whether the new
command repeated `--admin-url` and `--admin-ca`. `alias set` replaces the whole
entry; omitted management settings are cleared, and an omitted `--path`
returns to `auto`. Reapply the intended complete configuration.

OC does not discover `.mc` automatically. Use [migration](migration.md) to
import version 10 data explicitly. If initialization fails with permission
errors, choose a writable configuration directory owned by the account running
OC; do not weaken permissions on every user's configuration to fix one job.

## TLS or endpoint errors

Check the two endpoints separately. Objects use the S3 URL; management uses
the resolved management URL and can have a different host, port and certificate.
Verify DNS, port reachability, certificate hostname, expiry and complete issuer
chain. Put private S3 CAs in the selected `certs/CAs/` directory and configure
the management CA with `--admin-ca` or the saved alias setting.

`AdminRedirectDisabled` means the management endpoint redirected. Configure its
actual root URL; proxy path prefixes and redirects are not supported for
management requests. If S3 operations work but `admin info` or online doctor
fails, check `--admin-url`, `OC_ADMIN_URL_<alias>`, `OC_ADMIN_URL` and `adminURL`
in that order. See [configuration](configuration.md) for CA precedence too.

`--insecure` disables TLS certificate verification. Repair the trust chain or
hostname instead of retaining it in a scheduled job.

## Authentication, permissions and unsupported operations

Use the JSON error category to narrow the investigation:

- `authentication`: check the credential pair, session expiry, disabled keys,
  system time and whether a proxy changes signed requests.
- `permission`: check the account's policy for the exact bucket, prefix,
  object version or management operation. A successful list does not prove
  write or administrative permission.
- `not_found`: verify the alias, bucket, object spelling and requested version.
- `unsupported`: check the deployment's feature prerequisites and the
  [compatibility guide](compatibility.md). Client command presence does not add
  a missing server feature.
- `endpoint`, `network` or `timeout`: check the resolved address, proxy, DNS,
  TLS and server logs around the same time.
- `watch`, `trace` or `stream`: inspect the subscription failure and the
  event-history limits below.
- `other`: retain the underlying message; this category does not imply that
  the credentials were accepted.

`error.code` preserves a known SDK/server code when available; it can be absent.
Ordinary command failures return `1`. SIGINT and SIGTERM normally return `130`
and `143`; cleanup failure or forced termination can produce another nonzero
status. Treat all nonzero statuses as incomplete work unless your job explicitly
handles cancellation.

## Lifecycle target support is not confirmed

ILM remote target mutations can fail with `server does not confirm lifecycle
transition v1 support`. The pinned server lacks the new runtime; check the
matching [server implementation and protocol scope](lifecycle-transition.md).
The signed capability request uses the resolved management endpoint and requires
`admin:GetBucketTarget`; modifying the target requires `admin:SetBucketTarget`.
Check those permissions, the management route and certificate trust. Missing or
unexpected capability headers, redirects and incomplete responses are rejected.
Changing `--allow-writes` on the local console does not enable CLI access or
upgrade the server. Ordinary `oc ilm` rule operations do not perform this target
capability check and do not prove transition or restore support.

## An IAM policy assignment disappeared

The current `admin policy update` implementation can clear assigned policies
when given an already assigned policy or an empty argument. Check `admin user
info` or `admin group info`, then use `admin policy set` to restore the complete
intended comma-separated policy list and verify access. Avoid repeating
`policy update` as an initialization step. See the
[administration guide](administration.md#manage-policies-users-and-groups).

## Transfers, mirrors and notifications

Retry failed copies deliberately and verify downloaded bytes. Do not infer
success from an old destination file. `cp --continue` creates or resumes a copy
session; it is not a guarantee that every interrupted provider operation can
resume safely. Check the chosen server and copy mode.

When a mirror reports an overwrite conflict, preview with `--fake --overwrite`
and confirm that the source is authoritative. A fake operation can list removal
candidates, but actual deletion requires `--remove`. `--remove` deletes
destination-only objects. Exclusions affect both source and destination
comparison, while age filters do not protect destination-only objects from
deletion. Recheck the source/target direction and quoted filter patterns before
running a changed command. See [usage](usage.md#mirror-a-directory-or-prefix).

`WatchStreamClosed` reports an unexpectedly ended subscription.
`WatchQueueSaturated` and `WatchEventsLost` report incomplete local notification
history. Standalone `watch` and `find --watch` return nonzero; a new subscription
does not replay missing events. Local-source `mirror --watch` re-registers and
reconciles current state, with periodic rescans by default. Deep verification is
opt-in and reads both sides. Watch reconciliation does not provide durable
audit history, and its local-source recovery rules do not validate remote
active-active replication. Use a durable server audit system when event history
is the requirement.

## Report a reproducible problem

For a public issue, include:

- OC release tag, operating system and architecture.
- Server version and deployment type, such as single-port HTTP or two-port TLS.
- A small command with keys, signed URLs, private hostnames and sensitive object
  names replaced by placeholders; include relevant flags and exit status.
- Expected behavior, actual behavior and the successful doctor report if useful.
- Whether the problem reproduces with a disposable local file or test bucket.

Do not attach `config.json`, its backups, exports containing credentials, environment dumps,
encryption keys, session tokens, share URLs or unreviewed `--debug` traces.
JSON errors can still contain infrastructure details. Review and redact logs
before posting them. Use the private channel in the [security policy](../SECURITY.md)
for suspected vulnerabilities; do not publish credentials or an exploit in an
ordinary issue. Maintainers' reproduction tools are documented in
[development](development.md).
