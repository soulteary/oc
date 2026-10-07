# OC client reliability and release update

This update includes the changes after Git tag `RELEASE.2026-10-07T04-39-33Z`.
That tag is a source baseline; it does not establish that GitHub release assets
were published. The release uses OtterIO's UTC timestamp tag format.

## Changes

- Migrate OC and the pinned OtterIO source to urfave/cli v3.14.0. Preserve
  command scope, environment/list parsing, help, completion, initialization,
  output streams and signal cleanup with fixed old-binary snapshots. Correct the
  invalid health-selector usage panic. Gateway Go plugins use fresh command
  factories and the native v3 API; see [CLI migration](docs/cli-migration.md).
- Fix Windows notification decoding without weakening race/checkptr checks.
  Preserve asynchronous read buffers until cancellation completes, close idle
  handles, and keep old completions from removing replacement watches. Report
  genuine event loss so live mirrors reconcile their state.
- Handle Windows filename limits, OS-blocked staging-directory replacement and
  unsupported filesystem attributes. Synchronize periodic mirror recovery with
  scan/copy worker shutdown.
- Harden configuration import, endpoint validation and configuration cache access.
  Keep JSON errors on one line with structured categories and codes when available. Correct live
  mirror filtering and event handling.
- Validate object-lock durations and calendar bounds before arithmetic or copying
  data. Reject out-of-range retention dates before data transfer; retain supported
  clear requests and public interfaces.
- Update the Go vulnerability scanner for Go 1.27 and use the existing transfer
  deadline when observing throttled multipart progress. Keep cancellation,
  throughput and residual-session assertions.
- Add timestamp-based binary publication with exact-source main CI checks,
  eleven platform archives, SHA-256 checksums, source identity and licenses.
  Upload to a stable draft and verify every downloaded asset before publication.
- Publish Linux amd64/arm64 container images to GHCR, with optional Docker Hub
  publication. Package the exact release executables, verify pushed image digests
  before publishing the GitHub release and record those digests in the manifest.
  Keep version tags immutable; promote the newest published stable release to
  `latest` separately by digest, then update GitHub's latest release marker.
- Replace inherited installation/support instructions with task-oriented OC
  documentation in English and Simplified Chinese: installation, containers,
  configuration, transfers, migration, administration and troubleshooting.
  Add command, compatibility, security, development and maintainer guides, and
  preserve dated validation records with links to current instructions.

## Compatibility and upgrade

The module path remains `github.com/soulteary/mc`. The OtterIO SDK is pinned
to the full remote source recorded in `docs/compatibility.json`; the same source
builds the integration server without additional patches. This release does not claim compatibility with
all historical OtterIO versions or every third-party S3 service.
See [the compatibility manifest](https://github.com/soulteary/oc/blob/main/docs/compatibility.json) and
[current validation scope](https://github.com/soulteary/oc/blob/main/docs/compatibility.md).

OC uses its own configuration directory. Back up configuration before importing
from mc; import replaces destination aliases after validation and a private backup.
`OC_*` settings take precedence over supported legacy `MC_*` settings.
MinIO self-update and SUBNET upload remain disabled. Replace the executable using
verified release assets and check `oc --version`; the Windows archive contains
`oc.exe`. Test aliases, TLS, copy/mirror filters and object-lock operations before
rollout. Notifications have no durable replay cursor.

Select a release whose manifest contains `images`; archive-only releases,
including `RELEASE.2026-10-07T14-10-00Z`, have no container image identities.
Container images use `ghcr.io/soulteary/oc:<release-tag>`. Mount `/root/.oc` for
persistent configuration. Pin a version tag or manifest digest for deployments;
`latest` is a moving alias. Docker Hub publication requires both
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN` repository Actions secrets. A partially
pushed version requires a fresh release tag; a failed `latest` update can be
retried through **Stable release promotion** without rebuilding the release.

Source builds require Go 1.27.1. Cross-compilation is not runtime acceptance for
every architecture. The Windows notification implementation retains its MIT
license alongside OC's Apache-2.0 license and upstream notices. The manifest
records identity, not a signature or attestation.

Start with [the documentation index](https://github.com/soulteary/oc/blob/main/docs/README.md)
for verified installation steps and current operational guidance.

---

# OC 客户端可靠性与发布更新

本次包含 Git 标签 `RELEASE.2026-10-07T04-39-33Z` 之后的变化。该标签是源码比较基线，
不能作为 GitHub 附件已经发布的证明。新版本沿用 OtterIO 的 UTC 时间戳标签格式。

## 本次变化

- OC 与固定版本的 OtterIO 迁移到 urfave/cli v3.14.0，使用旧程序快照保留参数作用域、
  环境变量和列表解析、帮助、补全、初始化、输出流与信号清理；修复非法健康参数的帮助渲染
  panic。Go gateway 插件改用独立命令工厂和原生 v3 API，见 [CLI 迁移说明](docs/cli-migration.md)。
- 修复 Windows 通知解码、空闲句柄释放、异步取消缓冲区生命周期和重复监听。
  保留竞态及 checkptr 检查，真正的事件丢失仍会触发镜像同步恢复。
- 处理 Windows 长文件名、操作系统阻止的暂存目录替换和不支持的文件属性；
  周期镜像恢复等待扫描与复制工作结束后再更新共享状态。
- 加固配置导入、端点验证和配置缓存访问，保持单行 JSON 错误、结构化类别及可用时的错误代码，
  修复实时镜像过滤与事件处理。
- 在日期运算和复制前验证对象锁保留期限，拒绝超出可表示日历范围的日期，
  保留清除保留配置的请求和公开接口。
- 更新 Go 漏洞扫描器，分片进度观察采用既有传输期限，继续保留取消、吞吐和残留会话检查。
- 新增时间戳标签二进制发布：要求同一源码的 main 检查成功，构建 11 个平台归档，
  附带 SHA-256 校验文件、源码身份清单与许可证；草稿附件逐个下载比对后再发布。
- 发布 Linux amd64/arm64 容器镜像到 GHCR，并可选发布到 Docker Hub。
  镜像使用发布归档中的同一可执行文件，推送后按摘要验证，再发布 GitHub 版本，
  并把镜像摘要写入清单。版本标签禁止覆盖，最新已发布稳定版本单独按摘要提升为 `latest`，
  验证后再更新 GitHub 的最新版本标记。
- 将继承的安装与支持说明改为按任务组织的 OC 中英文文档，覆盖安装、容器、配置、传输、
  迁移、管理和排错。补齐命令、兼容、安全、开发与维护指南；带日期的验收记录继续保留，
  并链接当前操作说明。

## 升级与兼容性

Go 模块路径仍为 `github.com/soulteary/mc`，OtterIO SDK 固定到
`docs/compatibility.json` 记录的完整远程提交；集成服务端使用同一源码构建，无需附加补丁。
本次不扩大为所有历史 OtterIO 或第三方 S3 均兼容的承诺，具体范围见
[兼容性清单](https://github.com/soulteary/oc/blob/main/docs/compatibility.json)和[当前验收范围](https://github.com/soulteary/oc/blob/main/docs/zh_CN/compatibility.md)。

OC 使用独立配置目录。导入 mc 配置前先备份；导入在验证和私有备份后替换目标别名。
`OC_*` 优先于继续支持的旧 `MC_*` 设置。MinIO 自更新与 SUBNET 上传保持禁用。
使用经过校验的发布附件替换可执行文件，并检查 `oc --version`；Windows 使用 `oc.exe`。
部署前验证别名、TLS、复制与镜像过滤、对象锁操作。通知没有持久化重放游标。

选择发布清单中包含 `images` 的版本；包括 `RELEASE.2026-10-07T14-10-00Z` 在内的纯归档版本，
清单没有容器镜像身份记录。容器镜像使用 `ghcr.io/soulteary/oc:<release-tag>`，挂载 `/root/.oc` 保留配置。
部署时固定版本标签或清单中的摘要；`latest` 是会移动的别名。Docker Hub 发布需要同时配置
仓库 Actions secrets `DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。
版本镜像只推送了一部分时，必须使用新的发布标签；仅 `latest` 更新失败时，
可以通过 **Stable release promotion** 重试，无需重新构建或发布该版本。

源码构建要求 Go 1.27.1；交叉编译不能代替全部架构的运行验收。
归档同时保留 OC 的 Apache-2.0 许可证、上游声明和 Windows 通知实现的 MIT 许可证。
身份清单不是签名或供应链证明。

从[文档目录](https://github.com/soulteary/oc/blob/main/docs/zh_CN/README.md)查看已核对的安装步骤及当前操作说明。
