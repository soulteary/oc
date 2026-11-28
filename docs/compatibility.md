# Compatibility and validation scope

[中文](zh_CN/compatibility.md) · [Documentation](README.md)

OC's compatibility claim is limited to versions, deployments and operations with recorded passing tests. A compiled API or a command in `--help` is not enough to establish compatibility. The machine-readable baseline is [compatibility.json](compatibility.json).

## SDK pin and tested server baseline

The current Go toolchain, independent S3 SDK, OtterIO kits, server/admin module and full source SHAs are recorded in [compatibility.json](compatibility.json). The OC module remains `github.com/soulteary/mc`.

S3 operations use `github.com/soulteary/otterio-sdk/v7 v7.3.4`, released from `d3b12ac844759f41f5dc091dfdb141c3ce9f297e`, and published kits `crc64nvme v1.1.5` and `md5-simd v1.2.3`. The manifest's `storageSDK` and `otterioKits` fields identify these dependencies. Its legacy `otterioSDK` field still records the OtterIO server/admin module version so existing acceptance and release identity readers remain compatible; it is not the independent S3 SDK version.

The release-boundary check compares the SDK version and full source SHA with a reviewed tag-to-source mapping fixed independently in `buildscripts/verify-release-boundaries.py`. A full-length SHA alone does not establish release identity. An SDK upgrade must verify the published tag's source and update that mapping together with the dependency and compatibility manifest; the check runs without network access.

Server/admin packages and the core integration server use `github.com/soulteary/otterio v0.0.0-20261101162506-e9c0318fda00`, from the exact remote source `e9c0318fda0083caa3904573e44fad148e574517`, without local replacements or compatibility patches. The management query bridge, runtime/shutdown, HTTP API, account information and create-only conditional-write fixes are included in that source. The `otterio-*-compat.patch` files and earlier core reports are retained as historical evidence, not current setup instructions. The separate optional console protocol fixture is described below. Upgrading either dependency requires rerunning the recorded matrix. See [development](development.md) and [CLI migration](cli-migration.md).

This server/admin pin identifies compiled dependencies and the core test baseline. The deployed OtterIO server is selected separately and bound to OC by [packaged-pair acceptance](console-release-acceptance.md). Console rename needs the independent conditional-copy and conditional-delete capabilities from [OtterIO #55](https://github.com/soulteary/otterio/pull/55); the pinned server does not provide them. OC refuses servers without those capabilities before copying or deleting an object. The [rename contract](console.md#rename-semantics) describes partial outcomes and ETag limits. The [conditional mutation fixture](development.md#verify-conditional-uploads-and-rename) checks a server containing those capabilities while retaining S3 SDK `v7.3.4`; it does not change the historical core baseline or certify release artifacts.

## Deployments exercised by the test harness

The core deployment matrix contains:

- `single-http`: one HTTP object/management listener.
- `dual-http`: separate HTTP object and management listeners.
- `single-tls`: one TLS listener.
- `dual-tls`: separate TLS listeners with independent certificate trust.
- `dual-http-public`: separate HTTP listeners with public metrics.

Stability tests use `dual-tls`. The extended acceptance uses single-node, four-disk erasure storage. These configurations do not constitute a distributed multi-node matrix. Separate CLI fixtures cover NAS backed by local files and S3 gateway backed by a local OtterIO server, including CRUD and signal shutdown; they do not establish external-provider or other-backend compatibility.

Object acceptance covers bucket operations, empty and small objects, unusual and non-ASCII names, 65 MiB multipart transfers with download hash checks, server-side copy, stat, mirror, sharing and permission failures. The extended checks cover selected IAM and service-account operations, configuration round trips, quotas, object versions and tags, object lock and retention, lifecycle configuration, CSV Select, SSE-C, live event subscriptions, administrative streams, profile/health output, heal status and service control.

Those core checks have specific limits: lifecycle configuration is not timed-expiration acceptance; heal status is not a failed-disk recovery test; a KMS rejection is not successful KMS encryption; live subscriptions are not external-target delivery acceptance. [Administration](administration.md) explains endpoint and permission requirements.

## Optional console protocol and lifecycle fixture

The core integration job uses the unpatched `e9c0318fda0083caa3904573e44fad148e574517` pin above. The separate legacy-fallback and optional console protocol fixtures use the historical `6f6d0835ddff68020f1491c403b958fade22841f` source. Export patches apply only to writable copies of that historical source, in the documented combinations; do not reapply them to the current core pin, which already includes P3.

`console-server-base.patch` provides protected settings and own IAM rotation independently. Its conditional lifecycle profile supports prefix expiration and noncurrent expiration while rejecting transitions, tag filters and expired-delete-marker rules; existing configurations remain readable and ordinary unconditional S3 behavior is preserved. `lifecycle-storage.patch` and mandatory `lifecycle-storage-hardening.patch` are applied after base for transition execution, restore, target references, deletion protection and additional lifecycle filtering/scanning behavior. Hardening carries twelve source/test paths already merged in HEAD80 for storage-class snapshots, overwrite quorum and tier-metadata protection. Current source omits the independent IAM patch and is not byte-identical to the fixed-pin core composition.

Five-feature acceptance first uses base plus version authorization and IAM patches without the storage layer. A separately built base/storage/hardening server runs disposable-tier lifecycle acceptance; a fourth binary checks the full composition. GET/HEAD and CopyObject/UploadPartCopy version authorization are covered explicitly. `console-server-p3.patch` and the previous phase-three, lifecycle and five-feature reports retain their historical identity and are no longer current setup inputs.

Reproduction is in [development](development.md#reproduce-the-optional-console-protocol-fixture). Base settings/object, standalone five-feature/copy, hardened lifecycle, and full five-feature/copy plus settings/object reports have passed locally. Full composition lifecycle and the final aggregate have also passed. Acceptance is local macOS arm64; configured remote CI has not been run by this task. See [feature validation](console-features.md), [lifecycle scope](lifecycle-transition.md) and `consoleServerAcceptance` in [compatibility.json](compatibility.json). Final hardened Go/root-module source matches an independent current-plus-IAM projection; current itself omits IAM and nested Mint modules differ. These fixtures do not establish external-provider, distributed, gateway or filesystem-transition acceptance. The optional console is still source-built; current CLI release archives and images do not include it.

## Runtime CI and cross-compilation

[Go CI](../.github/workflows/go.yml) configures native unit/race tests and console frontend/helper regressions on Linux, macOS and Windows. Real OtterIO integration runs are configured on Linux and macOS with `CGO_ENABLED=0` and `1`: the exact unpatched pin, then independently built base, base/version/IAM, base/storage/hardening and full-composition servers. Each optional profile has separate acceptance and report artifacts. The workflow archives reports and diagnostic evidence even on failure. The separate [CLI compatibility workflow](../.github/workflows/cli-compat.yml) compares a fixed baseline and candidate on each native CI platform.

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

A successful cross-build proves that an executable can be produced for that target. It does not replace native execution, filesystem/ACL tests or real-service integration. In particular, Windows and FreeBSD do not have the same recorded service integration coverage as Linux/macOS. The current release workflow targets Linux amd64 and arm64 containers for subsequent timestamp releases; it validates both index entries but executes container smoke tests only on Linux amd64. Older releases can be archive-only; use a release whose manifest records `images` before expecting an image tag or digest. See [installation](installation.md).

A configured workflow is not a passing run. Check the results for the exact source commit in [GitHub Actions](https://github.com/soulteary/oc/actions). Local reports identify the tested binary hashes and platform; they are evidence for those runs rather than a promise for every later release.

## Stability budgets and their meaning

The current [budget values](compatibility.json) are:

- At most 120 seconds per measured transfer.
- At most 512 MiB OC process peak RSS during transfers.
- At most 5 seconds for measured cancellation.
- At most 64 MiB sampled memory growth during the short soak.
- At least 5 MiB/s measured transfer throughput.
- At most a 50% throughput drop relative to the reference transfer on the same host and run.

The harness transfers 65 MiB objects with concurrency 1 and 4, verifies downloaded bytes, checks cancellation and multipart cleanup, and exercises throttled and interrupted connections. CI requests 30 seconds of soak sampling; the recorded local acceptance also contains 60-second sampling.

Core throughput checks use two recorded warmups (one for each execution order) and six fixed measured/reference pairs (three for each order) for each upload/download operation and concurrency. Both roles create fresh remote objects or local files through the same warmed executor; the reference remains a single transfer even for the four-transfer group. The median of the six paired speed ratios must be at least 50%, and the median of the real measured group throughput at least 5 MiB/s. Timing spans CLI startup through observed process exit and excludes RSS sampler cleanup, which is recorded separately. Every warmup, measured transfer and reference still enforces the 120-second timeout and 512 MiB process RSS budget; every uploaded object is downloaded and SHA256-checked. All samples remain in the report, with no extra attempts after failure.

Console write acceptance uses five fixed CLI/console upload pairs with alternating order. Every object is downloaded and checked for size and SHA256. The median pair ratio must remain at least 50%, and median console throughput at least 5 MiB/s; all observations are retained without extra trials after a failure.

These are regression gates, not production sizing advice or a performance SLA. RSS is sampled every 100 ms for the main OC process on Linux/macOS. Transfer checks also retain the kernel peak RSS from that child's exit accounting and enforce the budget against the larger measurement, so even transfers shorter than the first sample remain checked. Reports record sampled, exit-accounted and combined process peaks separately. These measurements do not include the server or whole process tree; console and soak sampling can still miss short peaks. Throughput includes process startup and local filesystem overhead. A 30- or 60-second sample does not prove long-duration stability. Use your own representative workload before adopting a deployment.

## Features requiring separate acceptance

The compatibility manifest lists these unvalidated areas:

- Distributed topology, other gateway backends and external gateway upstreams.
- External KMS and external notification targets.
- Cross-instance replication.
- Third-party S3 services and historical OtterIO versions.
- Long-duration soak and disk-full recovery.

For another S3 provider, validate the object operations, authentication, addressing, encryption, multipart behavior and policy semantics that you use. OtterIO administrative commands require OtterIO's management API and do not follow from S3 compatibility.

Live notifications have no durable replay cursor. Reconnecting cannot guarantee delivery of events emitted during a disconnection. Use a persistent notification target and appropriate consumer acknowledgements when an event history is required. Periodic `mirror --watch` reconciliation concerns current state, not a complete event audit trail; see [usage](usage.md).

SDK extraction and independent publication are complete. SDK stream fixes remain deferred; notification delivery behavior remains outside this change. `MC_*` environment compatibility remains throughout OC 0.x; removal requires at least one minor-release notice. See [migration](migration.md).

## Read the recorded evidence

The current CLI migration and its exact-commit checks are tracked in
[OC PR #7](https://github.com/soulteary/oc/pull/7) and
[OtterIO PR #30](https://github.com/soulteary/otterio/pull/30).
The SDK/kits migration is recorded in [OC PR #8](https://github.com/soulteary/oc/pull/8) and [OtterIO PR #31](https://github.com/soulteary/otterio/pull/31); merged server storage-readiness fixture coverage is recorded in [OtterIO PR #32](https://github.com/soulteary/otterio/pull/32). The [2026-10-08 release preparation](releases/2026-10-08-release-review.md) records the current release baseline and outstanding publication gates.
CLI reports, compiled-module inventories and joint acceptance reports are
uploaded by the workflows linked from those checks.

The current catalogs cover 339 OC and 133 OtterIO CLI invocations. The reviewed
health usage-error renderer fix replaces the archived panic and exit code 2
with a specific argument error and exit code 1. Invalid selectors, durations,
booleans and unknown flags have separate exact approved deltas; see
[CLI migration](cli-migration.md) for their scope and retained raw evidence.

[Phase two](oc-phase-two.md) describes core endpoint and CA acceptance. [Phase three](oc-phase-three.md) and its [results](oc-phase-three-results.json) describe migration and extended operations. [Phase four](oc-phase-four.md) and its [results](oc-phase-four-results.json) record stability and subsequent joint review, including binary hashes and local platform details.

These are historical implementation and validation records. Their earlier statements about release work being disabled describe that stage; current publication procedures are in [releasing](releasing.md). A partially completed report or a skipped test must not be presented as a complete passing matrix.
