# Migrate an mc configuration to OC

[简体中文](zh_CN/migration.md) · [Configuration](configuration.md) · [Compatibility](compatibility.md)

Install OC alongside the old client first. OC defaults to `.oc` on Unix and
`oc` in the Windows user directory; it does not automatically load `.mc` or
change mc's files. Renaming the executable does not change the OC default.

## Check the source and back it up

Import accepts a JSON configuration with `"version": "10"` and an `aliases`
object. Upgrade older configuration formats with the original client before
importing. Keep a private copy of the old configuration and certificate
directory. Both the source and every backup contain secrets.

Use an independent destination directory for your first migration:

```sh
oc --config-dir /path/to/oc-migration config import /path/to/mc/config.json
oc --config-dir /path/to/oc-migration alias list
oc --config-dir /path/to/oc-migration --json doctor store
```

On Unix, the usual source is `$HOME/.mc/config.json`; choose the actual path
on your host. The alias `store` in the diagnostic command must exist in the
imported file. Inspect alias output locally; keys are redacted, but endpoint
names and CA paths may still be private.

## Understand what import replaces

`oc config import SOURCE` validates the JSON, version, alias names, S3 and
management endpoints, and bucket addressing settings before writing.
The imported aliases **replace the destination alias map**; they are not merged
with existing aliases. Plan any manual reconciliation before importing into
your normal OC directory.

The source stays unchanged. If a destination `config.json` already exists, OC
backs up its exact previous contents beside it as
`config.json.backup-<UTC timestamp>`. On Unix, new configuration files and backups
use mode `0600`; on Windows, keep the directory's access controls private to the
client account. The backup is synchronized before OC writes,
synchronizes and replaces the destination using a temporary file in the same
directory. Import does not delete older backups.

Import rejects source and destination paths that refer to the same file,
malformed JSON, unsupported versions, missing alias data and invalid endpoints.
Source S3 URLs must be HTTP/HTTPS roots without embedded credentials, queries,
fragments or path prefixes. Credentials belong in the alias fields. A failed
validation does not authorize replacing the destination with partially imported
aliases.

## Move certificates and check management settings

Import copies alias data, including optional `adminURL` and `adminCAFile`.
A relative management CA path becomes absolute relative to the **source
configuration file's directory**. Import does not copy that CA file or prove
that it contains valid certificates; it is read when a management client is
initialized. Check the resulting path before moving or deleting the source
directory.

Extra S3 CAs under the old `certs/CAs/` directory, copy sessions, saved shares
and profiling files are not imported. Copy only the S3 CA files you need into
the destination's `certs/CAs/`. For a two-port service, verify the independent
management URL and CA using the [configuration guide](configuration.md).

## Test before switching jobs

Check the imported alias with a read-only management diagnostic, then verify
your actual data path and permissions in a disposable bucket:

```sh
oc --config-dir /path/to/oc-migration --json doctor --online store
oc --config-dir /path/to/oc-migration ls store
```

Online doctor only queries management ServerInfo. It does not prove upload,
download, mirror, object-lock or provider compatibility. Follow the
[copy examples](usage.md) to upload and download a small file, compare its
contents, and preview mirror filters. Test only the features your jobs use
against the deployment in [compatibility](compatibility.md).

Update executable names, configuration mounts and environment variables in
scripts and containers. Prefer `OC_*` names. Existing `MC_*` names remain
supported during OC 0.x, but a set `OC_*` value takes precedence over its legacy
counterpart; environment aliases can also override the newly imported file.
Check both sets before diagnosing an apparent import failure.

The Go module path remains `github.com/soulteary/mc`; this does not cause the
client to use mc configuration or MinIO's update channel. Self-update and
MinIO SUBNET upload are disabled. Upgrade using [verified OC releases](installation.md).

## Restore a previous configuration

Stop OC jobs that use the destination directory before restoring it. Keep the
failed configuration for local inspection, then replace `config.json` with the
specific `config.json.backup-<UTC timestamp>` you verified, preserving private
permissions. Check environment overrides again and rerun offline doctor.
Do not restore a backup through `config import` using the current
`config.json` as both source and destination.

The original mc files remain available, so you can keep the original client
while testing OC. Feature behavior and support scope are listed in
[compatibility](compatibility.md); connection and import failures are covered
in [troubleshooting](troubleshooting.md).
