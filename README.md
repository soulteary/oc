# OC client

OC provides filesystem and S3 object operations together with OtterIO administration. It derives from Apache-2.0 MinIO Client; upstream attribution remains in LICENSE, NOTICE and source headers.

[中文说明](README_zh_CN.md)

Release instructions and asset verification: [releasing OC](docs/releasing.md).

## Build and connect

Use the Go toolchain declared in `go.mod`:

```sh
make build
./oc --help
oc alias set store http://127.0.0.1:9000 ACCESS_KEY SECRET_KEY \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/example
oc cp ./file.txt store/example/file.txt
oc admin info store
```

Omit `--admin-url` for single-port deployments. Use `--admin-ca` for a private management CA. Put additional S3 CAs under the configuration directory's `certs/CAs/`. See [core connection and TLS](docs/oc-phase-two.md).

## Migrate from mc

The default directory is always `~/.oc` (user-directory `oc` on Windows), regardless of executable name. OC does not automatically read or modify mc configuration.

```sh
oc config import ~/.mc/config.json
OC_CONFIG_DIR=/path/to/config oc ls store
OC_HOST_store=http://ACCESS_KEY:SECRET_KEY@127.0.0.1:9000 oc ls store
```

Import accepts version 10 configuration and replaces the destination aliases after validation and a private backup. The source stays unchanged. Relative management CA paths resolve against the source configuration directory. Upgrade older formats with the original client first. S3 certificates, sessions and saved shares are not copied automatically.

Explicit `--config-dir` wins over environment settings. `OC_*` settings take precedence over corresponding `MC_*`; legacy names remain supported throughout OC 0.x and will receive at least one minor release of notice before removal. Environment aliases override file aliases. Region, encryption, profiling and health-check settings also accept the OC prefix. Management endpoint precedence is documented in phase two.

The Go module path remains `github.com/soulteary/mc`. Self-update and MinIO SUBNET upload are disabled; see [release boundaries](docs/oc-phase-one.md).

## Verify compatibility

See [phase three support and migration](docs/oc-phase-three.md) for deployment requirements and verified coverage. Advanced features depend on erasure/distributed storage and configured external services; interface presence alone is not a support guarantee.

```sh
python3 buildscripts/test-core-integration.py \
  --oc /path/to/oc --otterio /path/to/otterio --extended \
  --report /tmp/oc-compatibility.json
```

Tests use disposable local servers and credentials. The pinned server needs the three patches in the [compatibility manifest](docs/compatibility.json), covering query bridging, shutdown/restart, and HTTP/object path fixes. See the phase-three guide for the Darwin restart supervisor and platform validation. JSON errors keep existing fields and add `error.code` and `error.category`; each error is one JSON line. Error exit status remains 1, with existing cancellation/signal statuses preserved.

## Stability and diagnostics

Use `oc --json doctor [ALIAS]` for offline credential-free diagnostics and add `--online` for a read-only management connectivity check. See [phase four stability and support policy](docs/oc-phase-four.md) for fault injection, memory budgets, platform coverage and compiled-module inventories. SDK changes are deferred.
