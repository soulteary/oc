# OC console, CLI and SDK update

This release preparation covers changes after the published
[`RELEASE.2026-10-07T17-07-26Z`](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-07T17-07-26Z)
(source `eeb95dbeeca8d80ae3e86e133228615ba84e02ce`). The next release keeps
OtterIO's UTC `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tag format. The preparation PR does
not reserve a tag or publish archives or images.

## Changes

- Package `oc-console` alongside `oc` in every timestamp release archive and
  the Linux amd64/arm64 release image. Stamp its version with the release tag,
  verify archived/image executable bytes, and smoke-test both entry points.
  Existing releases are unchanged; no new release is published by this PR.
- Begin migration of the local single-identity deployment example with
  `OTTERIO_BROWSER=off`. Keep the internal Admin listener and server defaults.
  See [compatibility and retirement gates](docs/console-release-migration.md).

- Add the optional `oc-console` for one operator and one local S3
  alias. It provides authenticated browsing and streaming downloads by default.
  Explicit `--allow-writes` adds uploads, confirmed object/batch/prefix deletion,
  cancellation and per-object results. Credentials stay in OC; operations use
  the selected identity and verified TLS. See [the console guide](docs/console.md).
- Keep create-only uploads atomic on the pinned OtterIO FS and single-pool
  erasure server using its conditional-write capability. Servers without that
  guarantee reject default uploads; explicit replacement needs separate
  confirmation. Canceled transfers attempt cleanup of their own multipart
  uploads, and uncertain commits or failed cleanup remain visible.
- Read complete bucket policy JSON and versioning/lifecycle XML in the console.
  With `--allow-writes` and the matching console base protocol, review and apply
  revision-protected replacements, remove policy/lifecycle configurations, and
  rotate the selected native IAM user's secret. Rotation retires local sessions
  and requires updating the alias in the terminal before restarting.
- Add confirmed bucket creation/empty-bucket deletion, native IAM user/group/
  service-account management, historical version selection, ZIP downloads and
  opt-in presigned sharing to the console. Protected version and
  IAM operations require their matching server protocols. See
  [feature scope and validation](docs/console-features.md).
- Add labeled ILM destinations to `oc admin bucket remote` and require the
  native lifecycle-transition capability before adding, editing or removing an
  ILM target. The matching base/storage/hardening patches implement current and
  noncurrent transitions, persistent destination references and version-specific S3
  restore on single-node, single-pool erasure. See [runtime scope and evidence](docs/lifecycle-transition.md).
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
- Retire Windows notification handles before closing them, so late successful
  zero-byte completions from intentional shutdown do not report event loss or
  trigger unnecessary mirror reconciliation.
- Compare transfer throughput with fresh destinations and three fixed,
  alternating measured/reference pairs after warmup. Retain every download's
  hash check and the existing 5 MiB/s floor, 50% ratio and 512 MiB memory budget.
  Combine sampled RSS with each process's exit-accounted peak so short transfers
  still receive a memory measurement.
- Parallelize cross-compilation, refresh compatible Go build caches and update
  English/Chinese onboarding, administration and console guides. Record the two
  intended ILM help changes as exact Unix/Windows approvals; retain the fixed
  baseline and strict checks for all other CLI output and behavior.

## Compatibility and upgrade

The OC module remains `github.com/soulteary/mc`; source builds require Go 1.27.2.
OC's S3 client SDK is separate from its OtterIO server/admin module. Exact pins,
source identities and validation limits are recorded in
[compatibility.json](docs/compatibility.json) and the
[compatibility guide](docs/compatibility.md). SDK extraction and independent
publication are complete; notification stream delivery fixes remain deferred.
Live notifications still have no durable replay cursor.

`oc admin policy update` currently fails to stop on an already assigned or empty
policy argument and can submit an empty replacement. Read the existing user/group
assignments first and use `policy set` with the complete desired policy list;
see [administration guidance](docs/administration.md).

Existing OC CLI commands, configuration and timestamp release conventions remain
in place. `OC_*` settings take precedence over supported legacy `MC_*` settings.
Back up configuration before importing from mc. Use verified OC release assets
and check `oc --version`; MinIO self-update and SUBNET upload remain disabled.

The experimental console is built with `make build-console`. The current CLI
archives, GHCR images and container workflow do not package it. A dedicated
console image and macOS Podman Compose deployment have not been delivered;
macOS users can run the native console against mapped S3/admin ports. Keep it on
literal loopback for a single operator. It provides the five console features
described above; OIDC, centralized deployment and historical restore/permanent
deletion buttons remain outside this implementation.
Default create-only uploads require the recorded server capability. Ordinary
S3 operations and OtterIO administrative operations have different compatibility
requirements; no blanket historical-server or third-party S3 claim is added.

The fixed OtterIO pin does not contain the optional console protocols. On a
clean writable copy of that pin, apply [console base](buildscripts/console-server-base.patch)
for protected settings and own credentials. The five-feature server adds
[version authorization](buildscripts/console-features-server.patch), then
[conditional IAM](buildscripts/console-iam-bindings.patch). Lifecycle deployment
instead requires base, [lifecycle storage](buildscripts/lifecycle-storage.patch)
and mandatory [lifecycle hardening](buildscripts/lifecycle-storage-hardening.patch);
the full composition adds versions and IAM after those three layers. See
[separate source builds and validation](docs/development.md#reproduce-the-optional-console-protocol-fixture).
The old `console-server-p3.patch` is frozen historical evidence and must not be
used as a current deployment input or combined with the new exports. Current
OtterIO HEAD80 already contains P3 and lifecycle hardening, while its working
source has separate authorization repairs; it lacks the independent conditional
IAM protocol. Do not reapply the fixed-pin export stack there. Building OC does
not upgrade the server, and the five-feature IAM proof uses independent patched
builds rather than that current checkout.
Older servers retain configuration reads while protected mutations remain
disabled. Returning to the old UI does not undo saved rules, credentials or
transitioned data; a server with new transition references cannot be downgraded
directly to a server that cannot read them.

After this PR merges, release only from a clean, synchronized main commit with
passing **Go** and **Code scanning - action** runs for that exact source. The
separate **CLI compatibility** result also needs review; the release job does
not automatically enforce it. The new ILM help contracts are approved, and all
339 cases passed on Linux, macOS and Windows for the reviewed pre-PR main
`f7b784affbf3e3d4d7c54663932d9d1925dca2b3`; review the final merged source again.
The workflow produces eleven CLI archives, a manifest and
checksums, plus verified
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
- 控制台读取完整桶策略 JSON、版本及生命周期 XML。显式开启 `--allow-writes` 且服务端
  具备配套基础协议时，可以复核并提交受 revision 保护的完整配置替换、删除策略或
  生命周期配置，以及修改当前原生 IAM 用户的 secret。改密会撤销本机会话，需要在
  终端更新别名后重新启动。
- 源码构建的控制台增加确认后创建桶/删除空桶、原生 IAM 用户/组/服务账号管理、
  历史版本选择、ZIP 下载和显式开启的预签名分享。受保护的版本及 IAM 操作还需
  服务端提供相应协议，见[功能范围与验收](docs/console-features.md)。
- `oc admin bucket remote` 增加带 label 的 ILM 目标；添加、编辑及删除 ILM 目标前
  必须确认服务端的原生转换能力。配套 base/storage/hardening 补丁在单节点、单 pool
  erasure 上实现当前/非当前版本转换、持久化目标引用和指定版本的 S3 恢复，见
  [执行边界与验证](docs/lifecycle-transition.md)。
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
- Windows 通知句柄在关闭前先标记退役，避免主动停止后到达的成功零字节完成事件
  被误报为事件丢失，进而触发不必要的镜像状态核对。
- 吞吐验收在预热后使用全新目标和固定三组交替测量 / 参考传输，保留每次下载的
  hash 检查，以及既有 5 MiB/s 下限、50% 比例与 512 MiB 内存预算。
  内存检查结合定时采样与每个子进程退出时记录的峰值，短传输也能保留测量结果。
- 并行执行交叉编译，更新兼容的 Go 构建缓存，并同步中英文入门、管理与控制台指南。
  将两项有意新增的 ILM 帮助文本登记为精确 Unix / Windows 批准差异；
  固定历史基线与其他 CLI 输出、行为检查保持严格。

## 升级与兼容性

OC 的 Go 模块仍为 `github.com/soulteary/mc`，源码构建要求 Go 1.27.2。
S3 客户端 SDK 与 OtterIO 服务端 / 管理模块是独立依赖，准确版本、源码身份和验证边界
见[兼容清单](docs/compatibility.json)及[兼容说明](docs/zh_CN/compatibility.md)。
SDK 提取和独立发布已经完成；通知流投递修复仍延期，实时通知仍无持久化重放游标。

`oc admin policy update` 当前遇到已绑定或空策略参数时没有停止请求，可能提交空替换值。
先读取用户 / 组的现有绑定，再用 `policy set` 提交期望保留的完整策略列表，见
[管理说明](docs/zh_CN/administration.md)。

保留现有 OC CLI 命令、配置和时间戳发布约定。`OC_*` 优先于继续支持的旧 `MC_*` 设置。
导入 mc 配置前先备份。使用经过验证的 OC 发布附件并检查 `oc --version`；
MinIO 自更新与 SUBNET 上传保持禁用。

实验控制台通过 `make build-console` 构建，当前 CLI 归档、GHCR 镜像和容器工作流不打包
该程序。独立控制台镜像与 macOS Podman Compose 部署尚未交付；macOS 当前可使用
原生程序连接已映射的 S3/Admin 端口。控制台仅供一个操作员在回环 IP 地址使用，
已具备上述五项功能；OIDC、集中部署和历史恢复/永久删除按钮仍未实现。
默认不覆盖上传要求服务端具备记录的条件写能力。普通 S3 操作与 OtterIO 管理操作有各自的兼容要求，
本次没有扩大为全部历史服务端或第三方 S3 均兼容的承诺。

固定 OtterIO pin 尚未包含可选控制台协议。在该 pin 的干净可写副本上，先应用
[基础协议](buildscripts/console-server-base.patch)提供保护性设置与自身改密；五功能服务端
继续应用[版本授权](buildscripts/console-features-server.patch)和
[条件 IAM](buildscripts/console-iam-bindings.patch)。生命周期部署使用 base、
[生命周期存储](buildscripts/lifecycle-storage.patch)和必带的
[存储加固](buildscripts/lifecycle-storage-hardening.patch)；完整组合再追加 versions 与 IAM。
源码独立构建和验证步骤见[开发指南](docs/zh_CN/development.md#复现可选控制台协议环境)。
旧 `console-server-p3.patch` 仅保留为冻结历史证据，不再作为当前部署输入，也不与新导出
补丁混用。当前 OtterIO HEAD80 已包含 P3 与生命周期加固，工作区另有授权修复，
尚未加入独立条件 IAM 协议；不要在该工作区重新应用固定 pin 的整套导出补丁。
构建 OC 不会升级服务端，五功能 IAM 证明来自独立补丁构建，而非当前 OtterIO 工作区。
旧服务端保留配置读取，受保护修改继续禁用。退回原 UI 不会撤销已保存规则、
secret 或转换数据；存在新转换引用时，不能直接降级到不理解该引用的服务端。

本 PR 合并后，只能从干净且与远程同步的 main 创建发布，并要求 **Go** 和
**Code scanning - action** 在同一源码提交上通过。
独立的 **CLI compatibility** 结果也需要审查，发布任务没有自动强制执行它；
新增 ILM 帮助合同变化已获批准，准备 PR 前的 main
`f7b784affbf3e3d4d7c54663932d9d1925dca2b3` 已在 Linux、macOS、Windows
全部匹配 339 项合同，合并后的最终源码仍需再次核对。工作流生成 11 个 CLI 归档、
发布清单及校验文件，并按配置记录经过验证的 Linux amd64/arm64 镜像身份。
交叉编译不能代替每个目标的原生运行验收，见
[2026-10-08 发布准备记录](docs/releases/2026-10-08-release-review.md)和
[发布流程](docs/zh_CN/releasing.md)。
