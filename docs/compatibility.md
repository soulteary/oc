# Compatibility and validation scope

[中文](zh_CN/compatibility.md) · [Documentation](README.md)

OC's compatibility claim is limited to versions, deployments and operations with recorded passing tests. A compiled API or a command in `--help` is not enough to establish compatibility. The machine-readable baseline is [compatibility.json](compatibility.json).

## SDK pin and tested server baseline

The current build baseline is Go `1.27.1` and OtterIO SDK module `v0.0.0-20261004215341-be8596f0d69d`, corresponding to commit `be8596f0d69d530586f35366fb2d5c79bdc54399`. The OC module still uses `github.com/soulteary/mc`; that name does not select an old update or publishing channel.

The client consumes the pinned remote module without a local `replace`. Real-service acceptance uses a server built from that same baseline with these patches applied in order:

1. [otterio-core-compat.patch](../buildscripts/otterio-core-compat.patch): management query-parameter bridging and regression tests.
2. [otterio-runtime-compat.patch](../buildscripts/otterio-runtime-compat.patch): bounded HTTP shutdown and Darwin restart supervision.
3. [otterio-http-api-compat.patch](../buildscripts/otterio-http-api-compat.patch): HTTP, object-path, management stream and resource-lifetime corrections.
4. [otterio-account-info-compat.patch](../buildscripts/otterio-account-info-compat.patch): authenticated root AccountInfo without an IAM-user lookup; ordinary and temporary identities retain their scoped permissions.
5. [otterio-conditional-writes-compat.patch](../buildscripts/otterio-conditional-writes-compat.patch): Atomic create-only PUT/multipart completion in FS and single-pool erasure storage. Write-back cache, gateways and multiple pools fail closed.

The SDK version and patched server fixture are different parts of the baseline. A deployment using the unpatched SDK/server commit does not inherit the fixture's passing results. Use a server that includes the required fixes; extending the baseline requires pinning the new version and rerunning the matrix. The [development guide](development.md) describes the reproducible checks.

## Deployments exercised by the test harness

The core deployment matrix contains:

- `single-http`: one HTTP object/management listener.
- `dual-http`: separate HTTP object and management listeners.
- `single-tls`: one TLS listener.
- `dual-tls`: separate TLS listeners with independent certificate trust.
- `dual-http-public`: separate HTTP listeners with public metrics.

Stability tests use `dual-tls`. The extended acceptance uses single-node, four-disk erasure storage. These configurations do not constitute a distributed multi-node or gateway matrix.

Object acceptance covers bucket operations, empty and small objects, unusual and non-ASCII names, 65 MiB multipart transfers with download hash checks, server-side copy, stat, mirror, sharing and permission failures. The extended checks cover selected IAM and service-account operations, configuration round trips, quotas, object versions and tags, object lock and retention, lifecycle configuration, CSV Select, SSE-C, live event subscriptions, administrative streams, profile/health output, heal status and service control.

Those checks have specific limits: lifecycle configuration is not timed-expiration acceptance; heal status is not a failed-disk recovery test; a KMS rejection is not successful KMS encryption; live subscriptions are not external-target delivery acceptance. [Administration](administration.md) explains endpoint and permission requirements.

## Runtime CI and cross-compilation

[Go CI](../.github/workflows/go.yml) configures native unit/race tests on Linux, macOS and Windows. Real OtterIO integration runs are configured on Linux and macOS with `CGO_ENABLED=0` and `1`, building the patched server fixture before testing. The workflow archives reports and diagnostic evidence even on failure.

Cross-compilation covers eleven targets:

```text
linux/amd64
linux/ppc64le
linux/arm64
linux/s390x
linux/arm
linux/386
darwin/amd64
darwin/arm64
freebsd/amd64
windows/amd64
windows/arm64
```

A successful cross-build proves that an executable can be produced for that target. It does not replace native execution, filesystem/ACL tests or real-service integration. In particular, Windows and FreeBSD do not have the same recorded service integration coverage as Linux/macOS. The current release workflow targets Linux amd64 and arm64 containers for subsequent timestamp releases. Older releases can be archive-only; use a release whose manifest records `images` before expecting an image tag or digest. See [installation](installation.md).

A configured workflow is not a passing run. Check the results for the exact source commit in [GitHub Actions](https://github.com/soulteary/oc/actions). Local reports identify the tested binary hashes and platform; they are evidence for those runs rather than a promise for every later release.

## Stability budgets and their meaning

The current [budget values](compatibility.json) are:

- At most 120 seconds per measured transfer.
- At most 512 MiB sampled OC process RSS.
- At most 5 seconds for measured cancellation.
- At most 64 MiB sampled memory growth during the short soak.
- At least 5 MiB/s measured transfer throughput.
- At most a 50% throughput drop relative to the reference transfer on the same host and run.

The harness transfers 65 MiB objects with concurrency 1 and 4, verifies downloaded bytes, checks cancellation and multipart cleanup, and exercises throttled and interrupted connections. CI requests 30 seconds of soak sampling; the recorded local acceptance also contains 60-second sampling.

These are regression gates, not production sizing advice or a performance SLA. RSS is sampled every 100 ms for the main OC process on Linux/macOS; it does not include the whole process tree or server, and sampling can miss short peaks. Throughput includes process startup and local filesystem overhead. A 30- or 60-second sample does not prove long-duration stability. Use your own representative workload before adopting a deployment.

## Features requiring separate acceptance

The compatibility manifest lists these unvalidated areas:

- Distributed topology and gateways.
- External KMS and external notification targets.
- Cross-instance replication.
- Third-party S3 services and historical OtterIO versions.
- Long-duration soak and disk-full recovery.

For another S3 provider, validate the object operations, authentication, addressing, encryption, multipart behavior and policy semantics that you use. OtterIO administrative commands require OtterIO's management API and do not follow from S3 compatibility.

Live notifications have no durable replay cursor. Reconnecting cannot guarantee delivery of events emitted during a disconnection. Use a persistent notification target and appropriate consumer acknowledgements when an event history is required. Periodic `mirror --watch` reconciliation concerns current state, not a complete event audit trail; see [usage](usage.md).

SDK stream fixes, SDK extraction and an independently released SDK remain deferred in the baseline. OC's adaptations do not change the pinned SDK version. `MC_*` environment compatibility remains throughout OC 0.x; removal requires at least one minor-release notice. See [migration](migration.md).

## Read the recorded evidence

[Phase two](oc-phase-two.md) describes core endpoint and CA acceptance. [Phase three](oc-phase-three.md) and its [results](oc-phase-three-results.json) describe migration and extended operations. [Phase four](oc-phase-four.md) and its [results](oc-phase-four-results.json) record stability and subsequent joint review, including binary hashes and local platform details.

These are historical implementation and validation records. Their earlier statements about release work being disabled describe that stage; current publication procedures are in [releasing](releasing.md). A partially completed report or a skipped test must not be presented as a complete passing matrix.
