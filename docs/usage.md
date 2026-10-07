# Copy, mirror and inspect data

[简体中文](zh_CN/usage.md) · [Install OC](installation.md) · [Configuration](configuration.md)

OC accepts local filesystem paths and `ALIAS/BUCKET/OBJECT` paths. This guide
uses the alias `store` and disposable buckets. Configure your endpoint and
credentials before running remote commands; see [configuration](configuration.md).
Object APIs require the server's S3 capabilities and permissions. OtterIO
administration has a separate [guide](administration.md).

## Start with local files

Local operations do not need a storage server or an alias:

```sh
mkdir -p ./oc-example/source ./oc-example/download
printf 'hello OC\n' > ./oc-example/source/hello.txt
oc ls ./oc-example/source/
oc cp ./oc-example/source/hello.txt ./oc-example/download/hello.txt
oc stat ./oc-example/download/hello.txt
oc cat ./oc-example/download/hello.txt
```

Use `./` or an absolute path when a local directory name could be mistaken for
an alias. Quote paths that contain spaces. Commands in these guides use POSIX
shell syntax; adapt paths and environment assignments for PowerShell.

## Connect and copy objects

For an OtterIO service on local ports 9000 and 9001, the following command prompts
for keys rather than putting them in shell history:

```sh
oc alias set store http://127.0.0.1:9000 \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/oc-example
oc cp ./oc-example/source/hello.txt store/oc-example/hello.txt
oc stat store/oc-example/hello.txt
oc cp store/oc-example/hello.txt ./oc-example/download/from-store.txt
```

These HTTP addresses are for a local test service. Use HTTPS for a deployed
service and configure its CA as described in [TLS configuration](configuration.md#trust-the-right-certificate).
For a single-port OtterIO deployment, omit `--admin-url`.

Copy a directory or object prefix recursively:

```sh
oc cp --recursive ./oc-example/source/ store/oc-example/upload/
oc cp --recursive store/oc-example/upload/ ./oc-example/download/
oc ls --recursive store/oc-example/upload/
```

`cp` copies selected objects; it does not delete destination-only objects.
An upload to an existing object name can replace its current contents or create
a new version when bucket versioning is enabled. Download into a separate
directory when you need to retain existing local files. Compare downloaded
contents or hashes before treating a transfer as verified; an ETag is not a
universal content checksum.

## Mirror a directory or prefix

`mirror SOURCE TARGET` walks directories or object prefixes recursively. It
compares available size, time and metadata; an ordinary mirror does not read
every unchanged object's contents. Missing destination objects are copied.
Different existing destination objects require `--overwrite`; without it OC
reports the conflict.

Preview a one-way mirror, then run the same operation:

```sh
oc mirror --fake --overwrite ./oc-example/source/ store/oc-example/mirror/
oc mirror --overwrite ./oc-example/source/ store/oc-example/mirror/
```

`--fake` lists candidate work without transferring or deleting data. It can list
destination-only removal candidates even without `--remove`; actual removal
still requires `--remove`. A preview does not reserve source or destination
state; review it again if either changes.

Destination-only objects remain unless you add `--remove`. **`--remove` deletes
data on the destination.** Use a dedicated destination prefix and preview with
the same options before running it:

```sh
oc mirror --fake --overwrite --remove \
  --exclude '*.tmp' ./oc-example/source/ store/oc-example/mirror/
oc mirror --overwrite --remove \
  --exclude '*.tmp' ./oc-example/source/ store/oc-example/mirror/
```

Quote each `--exclude` pattern so the shell does not expand it. Repeat the flag
for multiple patterns. Patterns match names relative to the mirrored root;
excluded source and destination entries are left out of the comparison.
An exclusion can therefore protect matching destination entries from removal.

Age filters use the source modification time:

```sh
oc mirror --overwrite --newer-than 7d ./oc-example/source/ store/oc-example/recent/
oc cp --recursive --older-than 30d store/oc-example/upload/ ./oc-example/older/
```

`--newer-than 7d` selects source objects younger than seven days;
`--older-than 30d` selects those at least thirty days old. Combined values such
as `7d10h30m` are accepted. **Age filters do not protect destination-only objects
from `mirror --remove`.** Use exclusions or a separate destination prefix when
deletion must stay within a defined scope.

## Follow changes

Watch a bucket or a local directory:

```sh
oc watch --events put,delete store/oc-example
oc watch --recursive ./oc-example/source/
```

Notifications have no durable replay cursor. A disconnect, restart or overloaded
watcher can lose events. A standalone `watch` or `find --watch` reports unexpected
stream closure or local event loss with a nonzero exit; restarting it does not
recover the missing history. Use notifications for live observation, not as a
complete audit log.

For a local source, a one-way watch mirror can reconcile current state:

```sh
oc mirror --watch --overwrite --watch-rescan-interval 1m \
  ./oc-example/source/ store/oc-example/live/
```

Local-source watch mirrors rescan every minute by default and re-register after
reported event loss or unexpected subscription closure. Recovery can recopy
existing source files, including equal-size/equal-time files. Routine rescans
compare metadata. Add `--watch-verify-interval 1h` to stream and compare both
sides with SHA-256 during a scheduled rescan; this needs destination read
permission and reads all selected data. Deep verification defaults to `0`
(disabled); enabled verification and rescan intervals must be at least `1s`.
Deletion still requires `--remove`. Filters remain in effect, with the deletion
boundary described above. Reconciliation restores current state, not files that
appeared and vanished between observations. These local recovery guarantees do
not establish durable delivery for remote S3 event streams or active-active
deployments.

## Work with versions and retention

Versioning and object lock require server support and suitable permissions;
they are not local filesystem features. Use a disposable supported deployment
before applying a policy to production data:

```sh
oc version enable store/oc-example
oc version info store/oc-example
oc ls --versions store/oc-example
```

For object retention, create a bucket with object lock enabled. Ordinary
versioning alone does not establish that object lock is available:

```sh
oc mb --with-lock store/oc-locked-example
oc cp --retention-mode governance --retention-duration 1d \
  ./oc-example/source/hello.txt store/oc-locked-example/hello.txt
oc retention info store/oc-locked-example/hello.txt
oc retention set --default governance 30d store/oc-locked-example
```

Retention validity uses positive whole days or years such as `1d` or `1y`;
invalid or unrepresentable dates are rejected. A bucket default applies to
future eligible writes, not retroactively to every existing version. Retention
protects eligible object versions against overwrite or deletion; uploading to
the same key can create a new current version. Compliance and governance have
different server-enforced rules. Check [compatibility](compatibility.md) for the tested
deployment and operations before relying on them. This guide does not use bypass
or destructive cleanup commands.

## Use output in scripts

```sh
oc --json ls store/oc-example > objects.jsonl
oc --json doctor store > doctor.json
```

`--json` is the machine-readable mode. Operational results generally use JSON
lines; command-specific formats, such as alias records and explicit exports, can
be formatted across several lines. Check the chosen command rather than assuming
one universal success schema. Unified errors carry `status`, `error.message`,
`error.cause`, `error.category` and, when available, `error.code`. A missing code
does not establish success. Do not mix `--debug` output into a JSON parser.

Always check the process exit status: normal success is `0`, ordinary errors
are `1`, interrupted commands normally return `130` (SIGINT) or `143` (SIGTERM).
Forced termination or a cleanup failure may return another nonzero status.
Capture the status before running another shell command. See
[troubleshooting](troubleshooting.md) for credential-free diagnostics and what
to include in a report.
