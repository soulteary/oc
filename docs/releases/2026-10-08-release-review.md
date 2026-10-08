# OC release preparation for 2026-10-08

This record prepares the next OC timestamp release. It does not reserve a tag,
publish artifacts or complete release acceptance. Preserve the
[2026-10-07 record](2026-10-07-release-review.md) as history; use the current
[release procedure](../releasing.md) for publication.

## Published baseline and reviewed scope

The GitHub latest-release API returned the published stable
[`RELEASE.2026-10-07T17-07-26Z`](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-07T17-07-26Z),
published at `2026-10-07T17:14:26Z`, with thirteen assets. Its source is
`eeb95dbeeca8d80ae3e86e133228615ba84e02ce`, matching the peeled Git tag.
The reviewed main cutoff before this preparation is
`f7b784affbf3e3d4d7c54663932d9d1925dca2b3`.
[Source comparison](https://github.com/soulteary/oc/compare/eeb95dbeeca8d80ae3e86e133228615ba84e02ce...f7b784affbf3e3d4d7c54663932d9d1925dca2b3).
The earlier SDK/kits preparation reviewed main through
`6d6d4483124ecf1065313b640c65eac2a15d1c58`; its local validation is retained
below with that source identity.

- The optional local `oc-console`, its read-only browsing/download path, opt-in
  upload/deletion/cancellation, permission boundaries and Windows socket fixture
  follow-up landed after the published baseline. It remains source-built and is
  not included in the current CLI release archives or container images.
- [OC #7](https://github.com/soulteary/oc/pull/7) migrated to native
  urfave/cli v3.14.0, retaining frozen old-binary contracts and separately approved
  health usage-renderer corrections. Public Go consumers must rebuild against
  the v3 types; ordinary CLI command behavior remains covered by the contracts.
- [OC #8](https://github.com/soulteary/oc/pull/8) adopted the independently
  released SDK v7.3.1 and published kits, including the fixed server/gateway
  storage-readiness fixture. This preparation records their identities and
  distinguishes the S3 SDK from the server/admin module.
- [OC #9](https://github.com/soulteary/oc/pull/9) recorded the reviewed SDK
  identity and the accepted server/admin main pin. [OC #10](https://github.com/soulteary/oc/pull/10)
  subsequently retired Windows notification handles before closing them, so
  intentional shutdown completions do not report false event loss.
- [OC #11](https://github.com/soulteary/oc/pull/11) parallelized cross-builds
  and refreshed Go caches. [OC #12](https://github.com/soulteary/oc/pull/12)
  introduced fixed alternating throughput samples with fresh destinations and
  hash checks. [OC #14](https://github.com/soulteary/oc/pull/14) combines sampled
  RSS with per-process exit peaks, retaining the existing performance/memory
  budgets, and records the two intended ILM help deltas on Unix and Windows.
- The console now reads complete bucket policy, versioning and lifecycle
  configuration. With the separate P3 server protocol it can apply protected
  settings and rotate the selected native IAM user's secret. The CLI adds
  labeled ILM target management, gated by the lifecycle-transition capability.
  These protocols are provided by the optional server patch; the unchanged
  `6f6d083` integration-server pin does not include them. This preparation
  does not claim that a newly published OtterIO main is the pinned server.
- [OC #13](https://github.com/soulteary/oc/pull/13) connects onboarding to a
  real OtterIO server; [OC #15](https://github.com/soulteary/oc/pull/15) updates
  current console/lifecycle guides and known limitations. In particular,
  `admin policy update` can submit an empty policy replacement for duplicate
  or empty input; use the documented complete-list `policy set` workflow.

Earlier Windows notification, retention and publication work already present in
the published baseline is not presented as a new change in the next release.

## Dependency identity

- Go toolchain: `1.27.1`; OC module: `github.com/soulteary/mc`.
- S3 client: `github.com/soulteary/otterio-sdk/v7 v7.3.1`, published from
  `c11549d350d8d1f7474bc26037616e912f344c15`.
- OC kits: `github.com/soulteary/otterio-kits/crc64nvme v1.1.2` and
  `github.com/soulteary/otterio-kits/md5-simd v1.1.3`.
- Server/admin module: `github.com/soulteary/otterio
  v0.0.0-20261008035209-6f6d0835ddff`, resolved from the actual remote merged
  commit `6f6d0835ddff68020f1491c403b958fade22841f`, with Go module checksums and
  no local replacement. The exact module source builds the integration server.

The `fed9cc3f491c9851e2fcb695c5b8455c4cca032e` and `6f6d0835ddff68020f1491c403b958fade22841f`
server trees are identical. Updating that pin records the accepted main source;
it does not introduce another runtime or readiness fix.

[compatibility.json](../compatibility.json) retains `otterioSDK` as the legacy
server/admin version field. The additive `storageSDK` and `otterioKits` fields
record the independent client dependencies. Release manifests preserve the same
distinction through `otterio_sdk`, `storage_sdk` and `otterio_kits`, without
invalidating earlier manifest readers. SDK extraction and independent publication
are complete; stream delivery fixes remain deferred.

## Earlier preparation validation

The following results belong to the earlier `6d6d448` preparation, not the
current cutoff or final release commit. Validation ran on macOS arm64 with
Go 1.27.1. OC and `oc-console` were built with
`CGO_ENABLED=1`; the clean, exact-source OtterIO server used `CGO_ENABLED=0`.
The two client binaries describe the reviewed preparation working tree based on
`6d6d4483124ecf1065313b640c65eac2a15d1c58` (`vcs.modified=true`), not a finalized
release commit. The server records `6f6d0835ddff68020f1491c403b958fade22841f`
with `vcs.modified=false`.

- OC and console builds passed, as did full `go test -mod=readonly -race
  --timeout 20m ./...`, `go vet ./...` and golangci-lint v2.14.0 (zero issues).
- The candidate matched all 339 OC CLI cases against the fixed old binary;
  no case catalogs or approved deltas were refreshed.
- Release-boundary checks and 65 release helper tests passed, including rejected
  SDK/server version confusion, source truncation and tag/source mismatch, kit
  drift, retired imports, archive identity/licenses/checksums, publication
  preflight and image promotion.
  Seven maintenance and eleven console identity/measurement tests passed.
- CLI runner regressions passed. Four native Windows process/filesystem cases
  were skipped on macOS; the optional environment-driven candidate test was
  covered by the separate real 339-case contract run. These skips are not Windows
  runtime acceptance.
- Joint core acceptance passed 1,026 checks: five basic listener/CA scenarios
  (83/84/86/87/83), migration (22), dual-TLS stability with 30-second soak (367),
  and four-disk erasure extended acceptance (214). The scheduler used normal
  asynchronous preemption; no `asyncpreemptoff` workaround was enabled.
- Console acceptance with writes passed 21 groups each on `single-http`,
  `dual-http` and `dual-tls`, including permission checks, transfer confirmation,
  cancellation and cleanup. The server VCS identity and client server-module pin
  were verified from binary build information.

The joint reports identify these SHA-256 values:

```text
oc          2841e1a8e37025ee509e0c462cba3eab46f737afd98e5b8575ebda184f7da89b
oc-console  4b3c68904414cef580d92e2364efc8eeda9e78bc00a3becd3b8fcb48443f1cc7
otterio     75c0810207253585a577235bcbcd3aaf89e47a6a7eae185847cf23d5ef1ec2dd
```

These local results do not complete Linux/Windows runtime checks, both CI cgo
modes, all eleven release archive builds or published-image verification. Native
PR CI and exact-source main CI remain separate. No distributed, historical-server,
external KMS/notification/replication or blanket third-party S3 acceptance is added.
See [compatibility](../compatibility.md) for the existing limits.

## Current source and preparation validation

The exact pre-PR main source `f7b784affbf3e3d4d7c54663932d9d1925dca2b3`
passed its push runs for [Go](https://github.com/soulteary/oc/actions/runs/37790106980),
[Code scanning - action](https://github.com/soulteary/oc/actions/runs/37790107130)
and [CLI compatibility](https://github.com/soulteary/oc/actions/runs/37790106995).
The latter matched all 339 cases on Linux, macOS and Windows, including the two
approved ILM help differences. These results establish the reviewed main
source's CI status; the final merged preparation needs its own exact-source runs.

This preparation changes release documentation only. The dependencies,
compatibility manifest, historical CLI baseline, optional server patch,
publication workflow and runtime code remain at the reviewed source.

Current local checks passed on macOS arm64 with Go 1.27.1 and Python 3.14.6:

- Full `go test -mod=readonly -race --timeout 20m ./...`, `go vet ./...`,
  `go build -mod=readonly ./...` and `go mod verify`.
- All 65 release-helper regressions, 17 maintenance regressions and the
  release-boundary check, including the maintenance loopback fixture.
- The 34-test CLI runner suite passed with five skips: four Windows-only cases
  and the optional real-binary test. The separate three-platform main run above
  verifies the real CLI contract; this local runner suite does not replace it.
- All 25 local links in the two changed release documents and
  `git diff --check`.

This documentation refresh does not rerun the optional patched-server acceptance,
build the eleven formal archives or publish/verify registry assets. The earlier
acceptance evidence and exact-source main CI retain their own scope.

## Publication gates still outstanding

- [ ] Merge the preparation and record the final main SHA.
- [ ] Confirm **Go** and **Code scanning - action** on that exact clean main source.
- [ ] Review **CLI compatibility** on that final source, including the recorded
  ILM help approvals; the release workflow does not enforce this separate run.
- [ ] Create a fresh annotated or signed UTC `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tag;
  never reuse or move a published or partially used tag.
- [ ] Verify all thirteen downloaded assets, checksums, source identity and
  licenses; verify published Linux amd64/arm64 image digests and runtime tags.
- [ ] Verify stable promotion only after publication and only for the newest
  stable release, then perform deployment-specific TLS, transfer, mirror and
  retention acceptance before rollout.

---

## 中文说明

本次比较基线为已经正式发布的 `RELEASE.2026-10-07T17-07-26Z`，源码
`eeb95dbeeca8d80ae3e86e133228615ba84e02ce`；准备前审查到 main
`f7b784affbf3e3d4d7c54663932d9d1925dca2b3`。新增范围包含实验性本机控制台、
CLI v3 迁移、独立 SDK/kits 依赖、Windows 通知关闭时序、吞吐与 RSS 测量修复、
交叉编译和文档更新。控制台仍需源码构建，不加入当前 CLI 归档或镜像。

S3 客户端固定为 SDK v7.3.1，服务端 / 管理模块固定为已合并的 `6f6d083`；
这两个模块不可混称。新旧服务端 pin 的源码树相同，换 pin 只规范 main 来源。
旧清单字段保留兼容性，新增字段准确记录 SDK 与 kits；已经完成的提取及独立发布
不再列为延期，通知流投递修复仍保留在延期范围内。

早期 `6d6d448` 准备的本机 macOS arm64 验证通过构建、完整竞态测试、lint/vet、339 项 CLI 合同、
发布与身份回归、1,026 项核心联调，以及三个场景各 21 组含写操作控制台验收。
客户端使用 cgo1、服务端使用 cgo0；客户程序记录准备工作区的 dirty 身份，
不能作为最终发布源码的证明。Windows 专属跳过项、跨平台 CI、全部发行归档和镜像
仍需分别确认，不能把本机结果扩展为全平台或全部 S3 服务均兼容。

新增 ILM 帮助差异已经按 Unix / Windows 的准确前后输出批准，原始基线保持不变。
准备 PR 前的 `f7b784af` main 已通过 Go、Code scanning 及独立 CLI compatibility，
三平台各匹配全部 339 项合同；这不能代替合并后最终 main 的检查。P3 设置、自身改密和
生命周期协议仍需可选服务端补丁，不能把 OtterIO 新 main 发布直接当作当前兼容 pin。
`admin policy update` 的重复 / 空策略问题仍未修复，发布说明保留完整策略列表替代操作。

本次 macOS arm64 复查通过完整 Go 竞态测试、vet、全部包构建和模块校验，65 项发布回归、
17 项维护回归、发布边界、34 项 CLI 检查器测试（5 项按既有条件跳过），以及两份发布文档
的 25 个本地链接和差异格式检查。没有重跑可选补丁服务端联调、构建正式 11 平台归档，
也没有发布或核验远程仓库附件和镜像。

准备 PR 不创建标签、发布附件或镜像。合并后等待同一 main 提交的 Go 与 Code scanning
通过，并复核同一提交的 CLI compatibility，再生成新的 UTC 时间戳标签，
并按[发行流程](../zh_CN/releasing.md)完成校验。
