# OC release preparation — 2026-10-10

## Published baseline and source

The latest published stable release is
[RELEASE.2026-10-09T17-33-03Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-09T17-33-03Z),
published at `2026-10-09T17:38:24Z` (October 10 in Asia/Shanghai).
Its peeled source tag is `99c6728ee75d10aaa7b6932a0ec07764258aa4ca`.
The reviewed main cutoff is `c2e2137dcec9047fa92e93d45b5f51f6150476b4`.
[Compare sources](https://github.com/soulteary/oc/compare/RELEASE.2026-10-09T17-33-03Z...c2e2137dcec9047fa92e93d45b5f51f6150476b4).

The increment contains object-management/localization/preferences and Docker
changes (`7fa0b109`), browser-off deployment (`dea77905`), console lint and
Windows preference-permission fixes (`fc7d1e90`, `f62ccfbc`), and console
release distribution (`efdfabec`, merged in #19). Earlier CLI v3 and SDK
migration changes already shipped and are not new changes in this release.
This preparation updates documentation only and reserves no release tag.

## Distribution and dependency identity

All eleven timestamp archives now contain both OC executables, with `.exe`
on Windows. Linux amd64/arm64 images include both; the default entrypoint is
`oc`. The workflow verifies packaged Linux amd64 console version/help and
image executable bytes. Thirteen uploaded files remain: eleven archives,
manifest and checksums. Existing published assets remain unchanged.

Build pins are unchanged from the published baseline: Go 1.27.2, storage SDK
v7.3.2 (`afb5be789cad5dda6c5daeffb227e5ea1724888c`), server/admin module
`v0.0.0-20261009091443-bcc238bc4c2c`
(`bcc238bc4c2cc3a21aae05b23321bfdb1b4aad05`), crc64nvme v1.1.3,
md5-simd v1.2.0 and urfave/cli v3.14.0. See
[compatibility.json](../compatibility.json) for their distinct identities.

The browser-off Compose example pins server
`RELEASE.2026-10-09T15-22-07Z` and uses a local OC image. Build pins are not
proof of the remote server. Historical console reports retain their original
source/profile scope; no new server-pair acceptance is claimed here.
See [migration and rollback gates](../console-release-migration.md).

## Validation

Local checks on macOS arm64, Go 1.27.2:

- All 65 release helper tests and release-boundary verification passed.
- All 73 console web behavior groups passed.
- `go test -mod=readonly` passed for `cmd/oc-console`, `internal/console`,
  `internal/consoleapi` (no test files) and `internal/storageclient`.
- `go mod verify` and `git diff --check` passed.

These checks cover the preparation working tree based on the cutoff above.
They do not constitute a full local race suite, eleven formal archive builds,
published-image verification, Windows execution, arm64 container runtime
acceptance or live server upgrade/rollback acceptance.

At preparation review, the cutoff main's
[Code scanning](https://github.com/soulteary/oc/actions/runs/38043131467) and
[CLI compatibility](https://github.com/soulteary/oc/actions/runs/38043131471)
passed. [Go](https://github.com/soulteary/oc/actions/runs/38043131466) was still
running. Recheck all workflows on the final merged preparation source; these
cutoff results cannot establish that future commit's status.

## Publication gates

- [ ] Merge preparation and record the final main SHA.
- [ ] Confirm Go, Code scanning and separate CLI compatibility on that SHA.
- [ ] From clean synchronized main, choose a fresh UTC timestamp tag and run
  release preflight. Never move or reuse a published or partially used tag.
- [ ] Verify all thirteen downloaded assets, archive contents for both programs,
  checksums, source identity and licenses.
- [ ] Verify immutable image digests and both executable identities; retain the
  distinction between architecture publication and runtime acceptance.
- [ ] Verify newest-stable promotion, then deployment-specific transfer, TLS,
  permission and upgrade/rollback acceptance before rollout.

Use the [release procedure](../releasing.md). The earlier
[2026-10-08 preparation](2026-10-08-release-review.md) remains historical.

## 中文说明

本次以 10 月 10 日北京时间凌晨发布的 `RELEASE.2026-10-09T17-33-03Z` 为基线，
审查截止 main `c2e2137d`。增量为控制台对象管理、多语言、偏好保存、Docker 部署、
Windows 权限与 lint 修复，以及控制台加入正式归档和镜像。旧 CLI / SDK 迁移已发布，
不再作为新增内容。依赖未改变；服务端构建依赖和实际部署版本分别记录。

本机通过 65 项发布工具测试、发布边界检查、73 组网页行为检查、控制台相关 Go 包测试、
模块校验与差异格式检查。截止 main 的代码扫描和 CLI 兼容已通过，Go 当时仍在运行。
这些结果不能替代准备合并后同一提交的完整 CI、正式附件和镜像验证，也不能证明全平台
或实际部署的升级回退。本次不创建标签或发布；合并后按清单完成余下门槛。
