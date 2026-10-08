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
`6d6d4483124ecf1065313b640c65eac2a15d1c58`.
[Source comparison](https://github.com/soulteary/oc/compare/eeb95dbeeca8d80ae3e86e133228615ba84e02ce...6d6d4483124ecf1065313b640c65eac2a15d1c58).

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

## Local preparation validation

Validation ran on macOS arm64 with Go 1.27.1. OC and `oc-console` were built with
`CGO_ENABLED=1`; the clean, exact-source OtterIO server used `CGO_ENABLED=0`.
The two client binaries describe the reviewed preparation working tree based on
`6d6d4483124ecf1065313b640c65eac2a15d1c58` (`vcs.modified=true`), not a finalized
release commit. The server records `6f6d0835ddff68020f1491c403b958fade22841f`
with `vcs.modified=false`.

- OC and console builds passed, as did full `go test -mod=readonly -race
  --timeout 20m ./...`, `go vet ./...` and golangci-lint v2.14.0 (zero issues).
- The candidate matched all 339 OC CLI cases against the fixed old binary;
  no case catalogs or approved deltas were refreshed.
- Release-boundary checks and 64 release helper tests passed, including rejected
  SDK/server version confusion, source truncation, kit drift, retired imports,
  archive identity/licenses/checksums, publication preflight and image promotion.
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

## Publication gates still outstanding

- [ ] Merge the preparation and record the final main SHA.
- [ ] Confirm **Go** and **Code scanning - action** on that exact clean main source.
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
`6d6d4483124ecf1065313b640c65eac2a15d1c58`。新增范围包含实验性本机控制台、
CLI v3 迁移与独立 SDK/kits 依赖。控制台仍需源码构建，不加入当前 CLI 归档或镜像。

S3 客户端固定为 SDK v7.3.1，服务端 / 管理模块固定为已合并的 `6f6d083`；
这两个模块不可混称。新旧服务端 pin 的源码树相同，换 pin 只规范 main 来源。
旧清单字段保留兼容性，新增字段准确记录 SDK 与 kits；已经完成的提取及独立发布
不再列为延期，通知流投递修复仍保留在延期范围内。

本机 macOS arm64 验证通过构建、完整竞态测试、lint/vet、339 项 CLI 合同、
发布与身份回归、1,026 项核心联调，以及三个场景各 21 组含写操作控制台验收。
客户端使用 cgo1、服务端使用 cgo0；客户程序记录准备工作区的 dirty 身份，
不能作为最终发布源码的证明。Windows 专属跳过项、跨平台 CI、全部发行归档和镜像
仍需分别确认，不能把本机结果扩展为全平台或全部 S3 服务均兼容。

准备 PR 不创建标签、发布附件或镜像。合并后等待同一 main 提交的 Go 与 Code scanning
通过，再生成新的 UTC 时间戳标签，并按[发行流程](../zh_CN/releasing.md)完成校验。
