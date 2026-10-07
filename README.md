# OC

[![Go checks](https://github.com/soulteary/oc/actions/workflows/go.yml/badge.svg)](https://github.com/soulteary/oc/actions/workflows/go.yml)
[![Release](https://github.com/soulteary/oc/actions/workflows/release.yml/badge.svg)](https://github.com/soulteary/oc/actions/workflows/release.yml)
[![License: Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

A command-line client for OtterIO, S3 object storage and local filesystems.

[简体中文](README_zh_CN.md) · [Documentation](docs/README.md) · [Releases](https://github.com/soulteary/oc/releases) · [Contributing](CONTRIBUTING.md)

OC uploads, downloads and inspects objects, synchronizes directories, and manages OtterIO servers. It derives from the Apache-2.0 MinIO Client codebase and retains the upstream notices. OC is an independent community project; it is not affiliated with or endorsed by MinIO, Inc.

## Install

Download a platform archive from [GitHub Releases](https://github.com/soulteary/oc/releases), verify its SHA-256 checksum, and put `oc` (`oc.exe` on Windows) on your `PATH`. The [installation guide](docs/installation.md) includes Linux, macOS, Windows and source-build instructions.

The release workflow builds container images for Linux amd64 and arm64. Select a version whose release manifest contains `images`:

```sh
# Select a published release whose manifest records container images.
TAG=RELEASE.YYYY-MM-DDTHH-MM-SSZ
docker run --rm "ghcr.io/soulteary/oc:$TAG" --version
```

See [container usage](docs/containers.md) for persistent configuration, file mounts, networking and digest pinning. Docker Hub publication is enabled when the release's repository secrets are configured; the release manifest records the registries actually published.

## Connect and transfer a file

After installing OC, replace the example addresses with your deployment's S3 and management endpoints. This example uses local HTTP; use HTTPS for connections across an untrusted network. OC prompts for the access key and secret key when they are omitted:

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

For a single-port OtterIO deployment, omit `--admin-url`. For another S3 provider, use its endpoint, credentials and supported bucket addressing; OtterIO administration is a separate API. Creating a bucket requires the corresponding permission. [Configuration](docs/configuration.md) covers private CAs and endpoint precedence; [everyday usage](docs/usage.md) covers copying, mirroring and object features.

## What OC provides

- Local and S3 file operations: `ls`, `cp`, `mv`, `cat`, `find`, `stat`, `du` and `mirror`.
- Object features: versioning, lifecycle, tags, retention, legal hold, notifications and replication, where the server supports them.
- OtterIO administration: server information, users, groups, policies, service accounts, metrics configuration and diagnostics.
- Separate S3 and management endpoints, including independently configured management CA trust.
- Explicit configuration import from mc, JSON output and credential-free `doctor` diagnostics.

[Compatibility and validation scope](docs/compatibility.md) distinguish tested operations from features that need provider or deployment acceptance. The pinned OtterIO fixture requires five recorded server patches. Distributed deployments, external KMS/notification targets and arbitrary third-party S3 services are not covered by the recorded acceptance matrix.

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
- [Configure endpoints and TLS](docs/configuration.md), [transfer and synchronize](docs/usage.md), or [look up commands](docs/commands.md).
- [Administer OtterIO](docs/administration.md) and [check compatibility](docs/compatibility.md).
- [Build the experimental local Web console](docs/console.md).
- [Troubleshoot problems](docs/troubleshooting.md) and [report vulnerabilities privately](SECURITY.md).
- [Build and test](docs/development.md), [contribute](CONTRIBUTING.md), or [prepare a release](docs/releasing.md).

The [documentation index](docs/README.md) also links the dated implementation and validation records.

## Build from source

Use the Go version declared in `go.mod` (currently `1.27.1`), Git and Make:

```sh
git clone https://github.com/soulteary/oc.git
cd oc
make build
./oc --help
```

The Go module path remains `github.com/soulteary/mc` for source compatibility. Clone the `oc` repository rather than relying on that historical module name as an installation channel. [Development](docs/development.md) explains test tools, platform coverage and the patched integration fixture.

## License and attribution

OC is distributed under [Apache-2.0](LICENSE). Preserve [NOTICE](NOTICE), [CREDITS](CREDITS) and the [MIT license for the internal notification fork](internal/notify/LICENSE) when redistributing it. The [notification fork notes](internal/notify/README.md) explain that component's maintenance and packaging requirements.
