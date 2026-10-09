<div align="center">

[![OC — OtterIO & S3 Command-Line Client](./.github/oc-banner.png)](https://github.com/soulteary/oc)

# OC

**OtterIO & S3 Command-Line Client** — _Copy. Sync. Manage._

[![License](https://img.shields.io/badge/License-Apache%202.0-blue.svg)](./LICENSE)
[![Go](https://img.shields.io/badge/Go-1.27.2%2B-00ADD8.svg?logo=go&logoColor=white)](./go.mod)
[![Go checks](https://github.com/soulteary/oc/actions/workflows/go.yml/badge.svg)](https://github.com/soulteary/oc/actions/workflows/go.yml)
[![Release](https://github.com/soulteary/oc/actions/workflows/release.yml/badge.svg)](https://github.com/soulteary/oc/actions/workflows/release.yml)

English · [简体中文](./README_zh_CN.md)

[Documentation](./docs/README.md) · [Releases](https://github.com/soulteary/oc/releases) · [Contributing](./CONTRIBUTING.md)

</div>

OC is a command-line client for OtterIO, S3 object storage and local filesystems. Upload and download objects, inspect files, synchronize directories, and administer OtterIO servers from your terminal.

This README covers installation and your first file transfer. The [documentation index](./docs/README.md) links configuration, everyday usage, migration, administration and development guides.

> [!IMPORTANT]
> OC is an independent community project derived from the Apache-2.0 MinIO Client codebase. It is **not** affiliated with or endorsed by MinIO, Inc. Original copyright and attribution notices are retained; see [License and attribution](#license-and-attribution).

---

## What is OC

OC brings local files and S3 objects into one command-line workflow:

- **Copy** — upload, download and inspect files with familiar commands such as `cp`, `ls` and `stat`.
- **Sync** — mirror directories between local filesystems and S3 storage with `mirror`.
- **Manage** — inspect and administer OtterIO servers with `admin`, or diagnose connections with `doctor`.

[OtterIO](https://github.com/soulteary/otterio) runs the storage server; OC provides its command-line client. To integrate S3 operations into a Go application, use [OtterIO SDK](https://github.com/soulteary/otterio-sdk). The server, client and SDK publish their own releases. See [What OC provides](#what-oc-provides) for features and compatibility boundaries.

---

## Install

Download a platform archive from [GitHub Releases](https://github.com/soulteary/oc/releases), verify its SHA-256 checksum, and put `oc` (`oc.exe` on Windows) on your `PATH`. The [installation guide](docs/installation.md) includes Linux, macOS, Windows and source-build instructions.

The release workflow builds container images for Linux amd64 and arm64. This example uses a published release whose manifest records its image; check the `images` field before choosing another version:

```sh
TAG=RELEASE.2026-10-07T17-07-26Z
docker run --rm "ghcr.io/soulteary/oc:$TAG" --version
```

See [container usage](docs/containers.md) for persistent configuration, file mounts, networking and digest pinning. Docker Hub publication is enabled when the release's repository secrets are configured; the release manifest records the registries actually published.

## Connect and transfer a file

After installing OC, replace the example addresses with your deployment's S3 and management endpoints. If you need a test server, follow [Try OC with local OtterIO](docs/quickstart.md), which starts a server and reuses its startup credentials. This example uses local HTTP; use HTTPS for connections across an untrusted network. OC prompts for the access key and secret key when they are omitted. Enter the credentials configured on the server; the client does not create a server account:

```sh
oc --version
oc alias set store http://127.0.0.1:9000 \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/example
printf 'Hello from OC\n' > hello.txt
oc cp hello.txt store/example/hello.txt
oc cp store/example/hello.txt downloaded.txt
oc stat store/example/hello.txt
oc admin info store
```

With OtterIO's `--console-address ":9001"`, port 9000 serves S3 and port 9001 serves the Web console and management API; use the management root URL rather than a console page path. For a single-port OtterIO deployment, omit `--admin-url`. For another S3 provider, use its endpoint, credentials and supported bucket addressing; OtterIO administration is a separate API. Creating a bucket requires the corresponding permission. [Configuration](docs/configuration.md) covers private CAs and endpoint precedence; [everyday usage](docs/usage.md) covers copying, mirroring and object features.

## What OC provides

- Local and S3 file operations: `ls`, `cp`, `mv`, `cat`, `find`, `stat`, `du` and `mirror`.
- Object features: versioning, lifecycle, tags, retention, legal hold, notifications and replication, where the server supports them.
- OtterIO administration: server information, users, groups, policies, service accounts, metrics configuration and diagnostics.
- Separate S3 and management endpoints, including independently configured management CA trust.
- Explicit configuration import from mc, JSON output and credential-free `doctor` diagnostics.
- An optional local Web console with bucket management, IAM administration, historical-version selection, ZIP downloads and opt-in presigned sharing; protected settings, version authorization and conditional policy bindings require matching server protocols.

[Compatibility and validation scope](docs/compatibility.md) distinguish tested operations from features that need provider or deployment acceptance. The pinned OtterIO source includes the core CLI compatibility fixes, so the core fixture needs no additional server patches. Protected console settings, own IAM rotation and the new ILM target/transition protocol require the separate [optional server patch](docs/lifecycle-transition.md); these protocols are absent from the current server pin. Distributed deployments, external KMS/notification targets and arbitrary third-party S3 services are not covered by the recorded acceptance matrix.

Historical-version authorization and safe console IAM creation, secret rotation and policy binding replacement use the additional [feature server patches](docs/console-features.md). Unsupported servers retain available reads and refuse these protected operations.

The [current authorization review and report links](docs/console-features.md#本轮授权复核) distinguish this revision from historical console evidence.

## Migrate from mc

OC uses its own configuration directory: `~/.oc` on Unix, and `oc` under the user profile on Windows. It does not automatically load `~/.mc`.

```sh
oc config import ~/.mc/config.json
oc --json doctor store
```

Import accepts configuration version 10, replaces destination aliases after validation, and backs up the previous destination configuration. Certificates and saved sessions are not copied automatically. Read [the migration guide](docs/migration.md) before importing an existing setup.

`OC_*` environment variables take precedence over supported legacy `MC_*` variables. Self-update and MinIO SUBNET uploads are disabled; install reviewed OC release artifacts manually.

## Find the right guide

- [Install and upgrade](docs/installation.md), or [run in a container](docs/containers.md).
- [Try OC with a local OtterIO server](docs/quickstart.md): start the server, configure both endpoints, and verify an upload and download.
- [Configure endpoints and TLS](docs/configuration.md), [transfer and synchronize](docs/usage.md), or [look up commands](docs/commands.md).
- [Administer OtterIO](docs/administration.md) and [check compatibility](docs/compatibility.md).
- [Build the experimental local Web console](docs/console.md): one alias, local sessions, bucket/object/IAM management, ZIP and sharing. It is source-built separately and is not included in CLI archives or containers.
- [Configure lifecycle destinations and review transition limits](docs/lifecycle-transition.md).
- [Troubleshoot problems](docs/troubleshooting.md) and [report vulnerabilities privately](SECURITY.md).
- [Build and test](docs/development.md), [contribute](CONTRIBUTING.md), or [prepare a release](docs/releasing.md).

The [documentation index](docs/README.md) also links the dated implementation and validation records. The [2026-10-08 project review (中文)](docs/reviews/2026-10-08-project-status.md) records source, release, validation and remaining-work boundaries.

## Build from source

Use the Go version declared in `go.mod` (currently `1.27.2`), Git and Make:

```sh
git clone https://github.com/soulteary/oc.git
cd oc
make build
./oc --help
```

The Go module path remains `github.com/soulteary/mc` for source compatibility. Clone the `oc` repository rather than relying on that historical module name as an installation channel. [Development](docs/development.md) explains test tools, platform coverage and the pinned integration fixture.

## License and attribution

OC is distributed under [Apache-2.0](LICENSE). Preserve [NOTICE](NOTICE), [CREDITS](CREDITS) and the [MIT license for the internal notification fork](internal/notify/LICENSE) when redistributing it. The [notification fork notes](internal/notify/README.md) explain that component's maintenance and packaging requirements.
