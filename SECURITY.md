# Security

[中文](docs/zh_CN/security.md) · [Documentation](docs/README.md)

## Report a vulnerability privately

GitHub private vulnerability reporting is enabled for this repository. Use [Report a vulnerability](https://github.com/soulteary/oc/security/advisories/new) to send a confidential report. Do not put exploit details, credentials, private endpoints or affected customer data in a public issue or pull request.

Include the OC release/version or source commit, operating system and architecture, relevant endpoint topology, impact, and a minimal reproduction with synthetic data. Add sanitized diagnostics or logs when useful. Say whether the problem is in the client, its packaging or its interaction with an OtterIO server, and identify the server version and required patches when known.

If you cannot use GitHub's private form, open a public issue containing only a request for a confidential contact method, with no vulnerability details. Wait for the maintainer to provide a private channel before sending the report. This project does not publish a response-time guarantee here.

Security testing should use systems and accounts you are authorized to test. Keep incident evidence and rotate exposed credentials according to your organization's procedures.

## Protect credentials and configuration

An OC alias can contain an access key, secret key and endpoint settings. The local `config.json` is not an encrypted secret store. The default configuration directory is `$HOME/.oc` on Unix-like systems and the user's `oc` directory on Windows. `--config-dir`, `OC_CONFIG_DIR` and legacy `MC_CONFIG_DIR` can override it.

Restrict access to that directory, configuration backups, sessions and sharing records using OS permissions. Protect CA trust files from unauthorized modification. Import creates a backup of the previous configuration, so deleting or rotating the current credential does not remove it from older backups. In containers and CI, use a protected configuration volume or secret injection, and avoid baking credentials into an image or committing them to the repository.

`oc alias set ALIAS URL` and `oc admin user add ALIAS` can prompt for keys. Prefer prompts for interactive use; command-line credentials can appear in shell history and process listings. Environment variables avoid literal command arguments but are not a secret vault: a privileged process, runner configuration or diagnostic dump can expose them. Use a dedicated, least-privilege identity for each workflow and rotate its credentials when exposure is suspected.

Ordinary alias text/JSON output redacts access and secret keys. That does not mean every command output is sanitized:

- Service-account creation with `--json` includes the new secret key.
- `admin config get`/`export` and local health reports can include server secrets or sensitive environment details.
- Generated Prometheus configuration can include a bearer token.
- Share URLs intentionally carry signed access parameters and grant access for their validity period.
- Object data, user names, bucket/object names and server-side trace or console records can be sensitive even without a credential.

Keep these outputs out of public logs and review them before sharing. There is no `alias export` command in this version; copying `config.json` or using a server configuration export still requires secret handling.

## Keep TLS verification enabled

OC verifies HTTPS certificates by default. Its object and management transports require TLS 1.2 or later. `--insecure` disables certificate verification for the invocation and permits an attacker with network access to impersonate the endpoint. Fix the endpoint hostname, certificate chain or CA trust rather than saving an insecure command in production scripts.

Additional S3 CA certificates belong in the selected configuration directory's `certs/CAs/`. Without an explicit management CA, management also uses that shared trust pool. A management PEM supplied through `--admin-ca`, `OC_ADMIN_CA[_ALIAS]` or the saved alias replaces the management pool with system roots plus that PEM, excluding extra S3 CAs. It does not change object-endpoint trust. Prometheus's `--metrics-ca` refers to the S3 listener's CA file on the Prometheus host.

An administrative endpoint must be an HTTP/HTTPS root URL without credentials, query parameters, fragments or path prefixes. Management redirects are refused to avoid forwarding signed requests and encrypted IAM/configuration bodies. Prefer HTTPS for both listeners; plain HTTP provides no TLS confidentiality or server certificate authentication. See [administration](docs/administration.md).

## Review diagnostics before sharing

`oc --json doctor ALIAS` uses an output allowlist and is offline by default. It omits endpoint hosts and paths, credentials, signatures and the configuration directory. `--online` additionally makes a read-only server-information request and exposes server count/version information. Review platform and version details against your organization's disclosure rules. The `certificateVerification` field describes the client setting; it does not prove certificate validity or reachability.

Client HTTP debug output redacts known authentication headers, session tokens, cookies, SSE-C customer keys, signature query parameters and embedded URL credentials. Request and response bodies are omitted from those HTTP dumps, and sensitive redirect headers are redacted. This is a targeted safeguard, not a guarantee that arbitrary logs, error messages, object names or server output are safe to publish. `--debug`, `admin trace`, `admin console`, health reports and exported configuration still require inspection.

## Install from reviewed release sources

Use the [OC GitHub releases](https://github.com/soulteary/oc/releases) and the registries described in [installation](docs/installation.md). Check the timestamp tag, archive SHA-256 against `checksums.txt`, and the source commit and asset identities in `release-manifest.json`. Older releases can be archive-only: do not infer an image tag from a Git tag. For an image-enabled release, use the image reference and digest recorded under `images` in its manifest. An immutable registry digest selects the exact image content; a mutable `latest` tag can change.

Checksums and image digests detect content changes relative to the values you trust. They are not release signatures and do not independently authenticate a publisher. A checksum downloaded with the artifact still depends on the security of the publication source. The current release workflow does not supply a cryptographic signing verification procedure; do not describe a checksum or OCI label as a verified signature. See [releasing](docs/releasing.md) for publication checks.

`oc update` is disabled and exits with status 1 without checking a remote version, downloading a replacement or modifying the executable. The inherited automatic MinIO update check is disabled. SUBNET report uploads are removed; `admin subnet health` only produces local output and rejects legacy upload flags. Upgrade by installing a reviewed OC release and verifying it using the installation instructions.

## Dependency and deployment limits

The [compatibility baseline](docs/compatibility.md) records the SDK pin, required server fixes and validation scope. Server IAM, administrative authorization, external KMS, notification services and replication are deployment responsibilities; a command's presence does not verify those systems.

CI includes a Go reachable-vulnerability check and a compiled-module inventory. These checks are evidence for their actual run and input; an unavailable vulnerability database is not a clean scan, and a module inventory is not a vulnerability or license verdict. Keep client and server dependencies under review and consult the reports for the exact version you deploy.
