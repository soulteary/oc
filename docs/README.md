# OC documentation

[简体中文](zh_CN/README.md) · [Project overview](../README.md)

Start with installation, then configure an alias and transfer a file. Guides describe the current OC client; the implementation records at the end describe the code and test results at the time they were written.

## Getting started

1. [Install or upgrade OC](installation.md): release archives, checksums, Windows and source builds.
2. [Run OC in a container](containers.md): configuration and data mounts, endpoint reachability and version pinning.
3. [Configure OC](configuration.md): aliases, environment variables, S3/management endpoints and TLS.
4. [Transfer and synchronize data](usage.md): local files, S3 objects, mirror behavior and automation.
5. [Migrate from mc](migration.md): version 10 import, backups and certificate migration.

## Operation and support

- [Command reference](commands.md): the available command families and how to inspect flags.
- [OtterIO administration](administration.md): server information, identities, policies and diagnostics.
- [Local Web console](console.md): opt-in source build, one alias, loopback sessions and current limits.
- [Compatibility and validation](compatibility.md): SDK pin, deployment coverage and known limits.
- [Troubleshooting](troubleshooting.md): authentication, TLS, endpoints, streaming and import failures.
- [Security](../SECURITY.md): credentials, trust and private vulnerability reporting.
- [Release notes](../RELEASE_NOTES.md) and [published releases](https://github.com/soulteary/oc/releases).

## Development and maintenance

- [Contributing](../CONTRIBUTING.md), [development and testing](development.md), and the [code of conduct](../code_of_conduct.md).
- [Maintainer workflow](MAINTAINERS.md) and [release publication](releasing.md).
- [Documentation maintenance](documentation.md): content structure, examples and the open-source references used for this documentation refresh.
- [Naming and module compatibility](../CONFLICT.md).
- [Internal notification fork](../internal/notify/README.md).

## Implementation and validation records

These records retain their original source ranges, limitations and test results. Consult the current guides above for installation and operation; an earlier stage's “not yet implemented” or “not verified” statement is not a current project status.

- [Phase one: build and release boundaries](oc-phase-one.md).
- [Web console migration plan](console-migration.md): staged implementation, permission differences and retirement gates.
- [Console phase one](console-phase-one.md): implementation, server fix, exact validation and remaining limits.
- [Console phase two](console-phase-two.md): object writes, independent review and validation.
- [Phase two: core connectivity](oc-phase-two.md).
- [Phase three: operations and migration](oc-phase-three.md), with [recorded results](oc-phase-three-results.json).
- [Phase four: stability and support policy](oc-phase-four.md), with [recorded results](oc-phase-four-results.json) and [development-build inventory](oc-phase-four.cdx.json).
- [Release preparation dated 2026-10-07](releases/2026-10-07-release-review.md).
- [Release preparation dated 2026-10-08](releases/2026-10-08-release-review.md): console, CLI and independently released SDK/kits.

The machine-readable [compatibility manifest](compatibility.json) supplies the toolchain, SDK pin, target list and test budgets. Recorded counts and inventories describe their individual runs; they are not release signatures or a guarantee for every deployment.

Older `minio-*-guide` paths are retained as navigation pages to avoid losing existing links. OC installation and support channels are documented here rather than inherited from MinIO.
