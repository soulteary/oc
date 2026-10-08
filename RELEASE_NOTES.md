# OC console, CLI and SDK update

This release preparation covers changes after the published
[`RELEASE.2026-10-07T17-07-26Z`](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-07T17-07-26Z)
(source `eeb95dbeeca8d80ae3e86e133228615ba84e02ce`). The next release keeps
OtterIO's UTC `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tag format. The preparation PR does
not reserve a tag or publish archives or images.

## Changes

- Add the optional, source-built `oc-console` for one operator and one local S3
  alias. It provides authenticated browsing and streaming downloads by default.
  Explicit `--allow-writes` adds uploads, confirmed object/batch/prefix deletion,
  cancellation and per-object results. Credentials stay in OC; operations use
  the selected identity and verified TLS. See [the console guide](docs/console.md).
- Keep create-only uploads atomic on the pinned OtterIO FS and single-pool
  erasure server using its conditional-write capability. Servers without that
  guarantee reject default uploads; explicit replacement needs separate
  confirmation. Canceled transfers attempt cleanup of their own multipart
  uploads, and uncertain commits or failed cleanup remain visible.
- Migrate OC and its server/admin dependency to urfave/cli v3.14.0. Preserve
  command scope, environment and list parsing, help, completion, initialization,
  output streams and signal cleanup using frozen old-binary contracts. Correct
  health usage-error rendering so invalid input reports an argument error
  instead of panicking. Go API consumers must follow [the CLI migration guide](docs/cli-migration.md).
- Use the independently released `github.com/soulteary/otterio-sdk/v7 v7.3.1`
  for S3 operations and the published OtterIO kits `crc64nvme v1.1.2` and
  `md5-simd v1.1.3`. Preserve SDK package names, checksums and transfer behavior;
  the source migration does not change OC configuration or storage formats.
- Pin server/admin packages and the integration server to merged OtterIO source
  `6f6d0835ddff68020f1491c403b958fade22841f`, including the CLI and SDK/kits
  migrations. Its tree matches the previously pinned `fed9cc3` source; this pin
  change records the accepted main commit rather than adding another runtime fix.
  Record the independent SDK, kits and server identities separately in
  compatibility and release manifests.

## Compatibility and upgrade

The OC module remains `github.com/soulteary/mc`; source builds require Go 1.27.1.
OC's S3 client SDK is separate from its OtterIO server/admin module. Exact pins,
source identities and validation limits are recorded in
[compatibility.json](docs/compatibility.json) and the
[compatibility guide](docs/compatibility.md). SDK extraction and independent
publication are complete; notification stream delivery fixes remain deferred.
Live notifications still have no durable replay cursor.

Existing OC CLI commands, configuration and timestamp release conventions remain
in place. `OC_*` settings take precedence over supported legacy `MC_*` settings.
Back up configuration before importing from mc. Use verified OC release assets
and check `oc --version`; MinIO self-update and SUBNET upload remain disabled.

The experimental console is built with `make build-console`. The current CLI
archives and container workflow do not package it. Keep it on literal loopback
for a single operator; it does not replace the existing OtterIO Web console or
provide OIDC, centralized deployment, management editing or version selection.
Default create-only uploads require the recorded server capability. Ordinary
S3 operations and OtterIO administrative operations have different compatibility
requirements; no blanket historical-server or third-party S3 claim is added.

After this PR merges, release only from a clean, synchronized main commit with
passing **Go** and **Code scanning - action** runs for that exact source. The
workflow produces eleven CLI archives, a manifest and checksums, plus verified
Linux amd64/arm64 image identities where configured. Cross-compilation does not
establish native runtime acceptance on every target. See the
[2026-10-08 preparation record](docs/releases/2026-10-08-release-review.md) and
[release procedure](docs/releasing.md).

---

# OC 控制台、CLI 与 SDK 更新

本次发布准备覆盖已发布版本
[`RELEASE.2026-10-07T17-07-26Z`](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-07T17-07-26Z)
之后的变化，比较基线源码为 `eeb95dbeeca8d80ae3e86e133228615ba84e02ce`。
后续版本继续使用 OtterIO 的 UTC `RELEASE.YYYY-MM-DDTHH-MM-SSZ` 标签。
准备 PR 不预留标签，也不发布程序归档或容器镜像。

## 本次变化

- 新增可选的源码构建程序 `oc-console`，供一个操作员通过本机已配置的 S3 别名使用。
  默认提供登录保护的浏览和流式下载；显式开启 `--allow-writes` 后支持上传、确认后
  删除对象 / 批量 / 前缀、取消和逐项结果。凭据留在 OC，操作遵循当前身份的实际权限，
  TLS 证书验证保持开启，见[控制台说明](docs/zh_CN/console.md)。
- 默认上传依赖固定版本 OtterIO 在 FS 和单 pool erasure 上的条件写能力，保证并发时
  不覆盖已有对象。缺少该保证的服务端会拒绝默认上传；替换需要单独确认。
  取消时清理本次创建的分片上传，提交结果未确认或清理失败会明确显示。
- OC 与服务端 / 管理包依赖迁移到 urfave/cli v3.14.0，用冻结的旧程序合同保留参数
  作用域、环境与列表解析、帮助、补全、初始化、输出流及信号清理。修复 health 用法
  错误渲染，使非法输入显示参数错误而不再 panic。Go API 调用者需按
  [CLI 迁移说明](docs/cli-migration.md)更新类型并重新编译。
- S3 操作采用独立发布的 `github.com/soulteary/otterio-sdk/v7 v7.3.1`，并使用已发布的
  OtterIO kits `crc64nvme v1.1.2`、`md5-simd v1.1.3`。保留 SDK 的包名、校验和及
  传输行为；源码迁移不改变 OC 配置或存储格式。
- 服务端 / 管理包和集成服务端固定到已经合并的 OtterIO 提交
  `6f6d0835ddff68020f1491c403b958fade22841f`，包含 CLI 与 SDK/kits 迁移。
  其源码树与原先固定的 `fed9cc3` 一致；本次换 pin 记录正式合并的 main 来源，
  没有新增运行时修复。兼容与发布清单分别记录独立 SDK、kits 和服务端身份。

## 升级与兼容性

OC 的 Go 模块仍为 `github.com/soulteary/mc`，源码构建要求 Go 1.27.1。
S3 客户端 SDK 与 OtterIO 服务端 / 管理模块是独立依赖，准确版本、源码身份和验证边界
见[兼容清单](docs/compatibility.json)及[兼容说明](docs/zh_CN/compatibility.md)。
SDK 提取和独立发布已经完成；通知流投递修复仍延期，实时通知仍无持久化重放游标。

保留现有 OC CLI 命令、配置和时间戳发布约定。`OC_*` 优先于继续支持的旧 `MC_*` 设置。
导入 mc 配置前先备份。使用经过验证的 OC 发布附件并检查 `oc --version`；
MinIO 自更新与 SUBNET 上传保持禁用。

实验控制台通过 `make build-console` 构建，当前 CLI 归档和容器工作流不打包该程序。
仅供一个操作员在回环 IP 地址使用，不取代现有 OtterIO Web 控制台；
尚不提供 OIDC、集中部署、管理编辑或对象版本选择。默认不覆盖上传要求服务端具备
记录的条件写能力。普通 S3 操作与 OtterIO 管理操作有各自的兼容要求，
本次没有扩大为全部历史服务端或第三方 S3 均兼容的承诺。

本 PR 合并后，只能从干净且与远程同步的 main 创建发布，并要求 **Go** 和
**Code scanning - action** 在同一源码提交上通过。工作流生成 11 个 CLI 归档、
发布清单及校验文件，并按配置记录经过验证的 Linux amd64/arm64 镜像身份。
交叉编译不能代替每个目标的原生运行验收，见
[2026-10-08 发布准备记录](docs/releases/2026-10-08-release-review.md)和
[发布流程](docs/zh_CN/releasing.md)。
