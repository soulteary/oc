# OC client guide

This historical filename remains as a navigation entry for existing links. OC's current guides use the `oc` executable, the OC repository, and OC release channels. The old MinIO download, demo-alias and self-update instructions have been replaced.

[Documentation index](README.md) · [简体中文](zh_CN/minio-client-complete-guide.md)

1. [Install or upgrade](installation.md), or [run in a container](containers.md).
2. [Configure aliases, endpoints and TLS](configuration.md).
3. [Copy, inspect and mirror objects](usage.md).
4. [Look up available commands](commands.md).
5. [Migrate version 10 mc configuration](migration.md).
6. [Administer OtterIO](administration.md).
7. [Troubleshoot failures](troubleshooting.md) and [check compatibility](compatibility.md).

For the exact command flags in your build, run `oc --help` and `oc COMMAND --help`. Do not use an upstream MinIO binary or `mc update` as an OC upgrade method.
