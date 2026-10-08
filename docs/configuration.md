# Configure endpoints, credentials and TLS

[简体中文](zh_CN/configuration.md) · [Usage](usage.md) · [Migrate from mc](migration.md)

OC stores aliases and credentials in `config.json` inside its configuration
directory. It uses its own directory even if the executable is renamed, and
does not automatically read or modify mc configuration. Use
[explicit import](migration.md) to migrate.

## Select a configuration directory

The defaults are `$HOME/.oc` on Unix and `%USERPROFILE%\oc` on Windows. To keep
a job or test separate from your interactive configuration:

```sh
oc --config-dir /path/to/oc-config alias list
OC_CONFIG_DIR=/path/to/oc-config oc --json doctor
```

Directory selection is `--config-dir` / `-C`, then `OC_CONFIG_DIR`, then
`MC_CONFIG_DIR`, then the platform default. Subcommands may initialize the
selected directory, including when displaying help. A read-only installation
of the executable still needs a writable configuration directory for normal
client state. Containers use the same rules; see [containers](containers.md).

The directory also holds `certs/CAs/`, copy sessions under `session/`, saved
shares under `share/`, and profiling output under `profile/` when requested.
Keep it and backups private. `config.json` contains credentials; normal alias
status/list output redacts keys, but endpoint names
and CA paths may still identify your infrastructure.

## Add an alias

An alias names the S3 endpoint and credential pair. This example prompts for the
access and secret keys:

```sh
oc alias set store https://s3.example.com --api s3v4 --path on
oc alias list store
```

For unattended setup, the full syntax is
`oc alias set ALIAS URL ACCESSKEY SECRETKEY [FLAGS]`. Supply secrets using your
job's secret mechanism; shell arguments can appear in history or process
inspection. The configuration file is sensitive even when command output is
redacted. See the [security policy](../SECURITY.md).

Use the service root URL, not a bucket URL or reverse-proxy path prefix.
`--api s3v4` selects the signature explicitly and skips signature auto-probing
during alias creation. It does not validate connectivity or prove access.
`--path on` uses path-style bucket addressing; `off` uses DNS-style addressing;
the default `auto` lets the client decide. DNS-style addressing also requires
matching DNS routing and TLS certificates. Choose the mode supported by your
deployment. The help includes older S3 examples, but provider-specific support
still requires [acceptance checks](compatibility.md).

Remove an unneeded alias with `oc alias remove store`. This changes client
configuration and does not delete remote buckets or objects.

`alias set` replaces the entire saved entry for an existing alias. When rotating
credentials or changing its S3 URL, repeat the intended `--api`, `--path`,
`--admin-url` and `--admin-ca` settings. Omitted management settings are cleared;
an omitted path returns to `auto`. Management environment overrides are used at
request time and are not copied into the saved entry by `alias set`.

`alias list` displays saved configuration only; it does not show process-level
environment or management overrides. An environment-only alias is absent from
that list. Removing a saved alias does not unset `OC_HOST_<alias>` or
`MC_HOST_<alias>`; an environment alias remains usable until its variable is
unset. Use `doctor ALIAS` to inspect the resolved protocol and management
settings without printing its credentials or host.

## Override an alias for one process

An environment alias can supply a URL with credentials:

```sh
OC_HOST_store='https://ACCESS_KEY:SECRET_KEY@s3.example.com' oc ls store
```

The values above are placeholders. Inject the real value from a secret store.
The environment parser has its own credential-delimiter rules; use a saved alias
when credentials contain URL delimiters, and do not assume percent-encoded keys
will be decoded. Environment values are also sensitive; do not paste a full
environment dump into an issue.

For the same alias, `OC_HOST_<alias>` wins over `MC_HOST_<alias>`, and an
environment alias wins over the file's S3 address and credentials. It retains
the file alias's independent management URL and CA unless those are overridden
as described below. Alias suffixes preserve case: `store` and `Store` are not
interchangeable environment names.

Environment aliases select S3v4 and automatic bucket lookup; they do not inherit
the file alias's `api` or `path` setting. Use the saved alias when you need an
explicit addressing mode.

`OC_REGION`, `OC_ENCRYPT`, `OC_ENCRYPT_KEY` and `OC_PROFILER` take precedence
over their matching `MC_*` names. Health diagnostics also accept
`OC_HEALTH_TEST` / `OC_OBD_TEST` and `OC_HEALTH_DEADLINE` / `OC_OBD_DEADLINE`;
explicit health command flags take precedence. For values read through the
OC/MC environment lookup, an explicitly empty `OC_*` value still suppresses the
legacy fallback. Unset an unwanted override instead of assuming an empty
variable will restore old credentials. Management settings use nonempty values
and their separate precedence below.

Legacy `MC_*` settings remain supported throughout OC 0.x, with at least one
minor release of notice before removal. Use `OC_*` in new jobs. Do not introduce
the deprecated `MC_HOSTS_*` format.

## Configure a separate OtterIO management endpoint

Object operations keep using the S3 endpoint. For a two-port deployment, persist
the management root URL and optional management CA in the alias:

```sh
oc alias set store https://s3.example.com --api s3v4 --path on \
  --admin-url https://admin.example.com --admin-ca /path/to/admin-ca.pem
```

The saved fields are `adminURL` and `adminCAFile`. Management URL and CA are
resolved independently, in this order:

1. Explicit `--admin-url` or `--admin-ca` on the command.
2. `OC_ADMIN_URL_<alias>` or `OC_ADMIN_CA_<alias>`.
3. Global `OC_ADMIN_URL` or `OC_ADMIN_CA`.
4. The corresponding saved alias field.

With no management URL, OC uses the S3 URL. With no management CA, the management
client uses the shared trust pool: system roots plus the selected configuration
directory's extra CA files. A one-off override does not change the saved alias:

```sh
oc --admin-url https://admin.example.com --admin-ca /path/to/admin-ca.pem \
  admin info store
OC_ADMIN_URL_store=https://admin.example.com oc admin info store
```

Management URLs must be HTTP/HTTPS root addresses with no embedded credentials,
query, fragment or path prefix. Management redirects are refused. Configure a
proxy to expose the management routes at the expected root instead of relying
on redirection. A relative CA path supplied to `alias set` is saved as an
absolute path; command and environment overrides resolve relative to the
command's working directory.

## Trust the right certificate

Publicly trusted HTTPS certificates usually require no extra CA configuration.
For a private S3 CA, put a PEM certificate file in the selected configuration
directory's `certs/CAs/` directory:

```sh
mkdir -p /path/to/oc-config/certs/CAs
cp /path/to/s3-ca.pem /path/to/oc-config/certs/CAs/s3-ca.pem
oc --config-dir /path/to/oc-config ls store
```

Without a separate management CA, management requests share the S3 trust pool,
including extra files from `certs/CAs/`. A nonempty `--admin-ca` or equivalent
management CA setting switches management requests to system roots plus the
specified PEM certificates, excluding those extra S3 CA files. It does not
change S3 trust. With distinct certificates on the two endpoints, configure
both trust paths.

Check certificate expiry, issuer chain and hostname when TLS fails. `--insecure`
disables certificate verification; repair trust rather than making it a
permanent setting. An offline doctor report's `certificateVerification: true`
only describes the setting, not whether the server's certificate is valid.

```sh
oc --json doctor store
oc --json doctor --online store
```

The online command reads management ServerInfo with a 15-second deadline; it
does not test every S3 operation or permission. Continue with
[troubleshooting](troubleshooting.md) or the [administration guide](administration.md).

Doctor's `adminSDK` field identifies the embedded OtterIO server/admin module,
not the independent S3 SDK. See the `storageSDK` entry in
[the compatibility manifest](compatibility.json) for the source baseline's S3
SDK version, and verify the installed release's own manifest when checking a
different executable.
