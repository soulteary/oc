# OC command reference

[Documentation index](README.md) · [简体中文](zh_CN/commands.md)

This index covers the command families registered by the current client. Flags and server capabilities can differ by version; use the help of the executable you will run:

```sh
oc --help
oc cp --help
oc mirror --help
oc admin --help
oc admin user svcacct --help
```

OC accepts flags before a command, and many commands also expose common flags locally. Put global flags first in scripts for clarity. Use local paths for filesystem operations, or `ALIAS/BUCKET/OBJECT` for S3 objects. A configured alias contains an endpoint and credentials; it is not a mounted filesystem.

## Configuration and diagnostics

- `alias`: `set`, `list`, `remove`; manages saved endpoint aliases. `set` replaces the complete saved entry; `list` does not show environment overrides, and `remove` does not unset them. There is no separate `alias export` command.
- `config import`: imports a version 10 mc/OC configuration with a destination backup; see [migration](migration.md).
- `doctor`: reports client/platform facts without printing credentials; add an alias for its resolved protocol and management settings. `adminSDK` identifies the OtterIO server/admin module, not the independent S3 SDK. `--online` also queries the management endpoint and requires an alias.
- `update`: retained to explain that self-update is disabled; it does not download or replace OC.

[Configuration](configuration.md) explains alias settings, environment precedence and TLS. [Troubleshooting](troubleshooting.md) explains interpreting diagnostics.

## Inspect and transfer files

- `ls`, `tree`, `find`: list or search buckets, objects and local files.
- `stat`, `du`, `diff`: inspect metadata, summarize size or compare object names/sizes/dates. `diff` is not a content-hash proof.
- `cp`, `mv`, `mirror`: copy, move or synchronize data. `mv` removes the source after successful transfer; `mirror --remove` can delete destination-only entries.
- `cat`, `head`: read contents, or the first lines of an object.
- `pipe`: upload data read from standard input.
- `mb`, `rb`, `rm`: create buckets, remove buckets or remove objects/files. Read the specific command's flags before deleting data.
- `share`: generate temporary access links. The resulting signed URL is sensitive until it expires.
- `sql`: issue S3 Select queries where the server supports them.

Examples and mirror filter/deletion behavior are in [everyday usage](usage.md).

## Object and bucket features

- `version`, `undo`: manage bucket versioning or undo supported PUT/DELETE operations.
- `retention`, `legalhold`: control object-lock retention and legal holds where supported; locked/versioned buckets need appropriate setup and permissions.
- `ilm`: manage lifecycle rules with `add`, `edit`, `rm`, `ls`, `export` and `import`. CLI export/import uses JSON; the console's full lifecycle editor uses XML.
- `encrypt`: manage server-side bucket encryption configuration; external KMS support is deployment-dependent.
- `tag`: manage bucket/object tags.
- `policy`: manage anonymous object access. This is different from `admin policy`, which manages identity policies.
- `event`, `watch`: configure or listen for object notifications. A live stream is not a durable replay log.
- `replicate`: configure server-side bucket replication; external/multiple-instance acceptance is not recorded for the pinned fixture.

The presence of a command does not establish support for a provider or topology. Read [compatibility](compatibility.md) and the command's own help before using advanced features.

## OtterIO administration

`admin` requires OtterIO's management API and suitable permissions. It is not a generic S3 administration interface.

- `admin info`: read server information.
- `admin user`, `admin group`, `admin policy`: manage identities, groups and policies; service accounts are under `admin user svcacct`.
- `admin config`: read, set, export or import server configuration. Exported configuration can contain secrets.
- `admin service`: restart or stop servers. These operations affect the deployment.
- `admin bucket`: manage bucket administrative settings, including quotas and remote targets. ILM target mutations require the matching server's transition protocol and management permissions; see [administration](administration.md#lifecycle-transition-targets).
- `admin prometheus`: generate metrics scraping configuration.
- `admin trace`, `admin console`: inspect live request/log streams.
- `admin profile`, `admin report`: collect diagnostic artifacts. The historical `admin subnet health` command remains as a legacy compatibility entry point; uploads are disabled.
- `admin top`, `admin kms`: deployment-dependent lock/KMS operations.
- `admin heal`: a deprecated entry point; inspect its help and deployment requirements before use.

The current `admin` group does not include an `admin update` command. [Administration](administration.md) covers workflows, metric endpoints, privileges and diagnostic artifact handling.

Review the [current `admin policy update` limitation](administration.md#manage-policies-users-and-groups)
before changing IAM assignments: a repeated or empty policy can clear existing
bindings. Use a verified complete assignment with `admin policy set` instead.
There is no dedicated CLI object restore command; the lifecycle transition
record describes native S3 restore requests separately.

## Common flags

- `--config-dir`, `-C`: choose a client configuration directory.
- `--admin-url`, `--admin-ca`: override the management endpoint or its CA file.
- `--json`: use the command's JSON output mode. Error diagnostics are one-line records; success/export schemas and formatting depend on the command and output mode. Do not assume all output has one universal JSONL schema.
- `--quiet`, `-q`, `--no-color`: control progress/color output.
- `--debug`: include debug diagnostics; review them for sensitive context before sharing.
- `--insecure`: disable certificate verification. Configure the appropriate CA instead of using this as a normal deployment setting.
- `--help`, `-h`: inspect command help. Use `oc --version` or `oc -v` at the root to print the client release tag.
- `--autocompletion`: install shell completion; this can change shell configuration. It is an explicit setup step, not a read-only diagnostic.

Command-specific flags such as `mirror --fake`, `cp --recursive`, `watch --events` and retention options belong to their respective help pages. Read them in the installed version before scripting.
