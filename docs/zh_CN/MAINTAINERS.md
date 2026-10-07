# 维护 OC

[English maintainer guide](../MAINTAINERS.md) · [文档目录](README.md)

这份指南说明本仓库的审查、依赖和发行职责，不是拥有仓库权限的人员名单。贡献者先阅读[贡献指南](CONTRIBUTING.md)和[开发指南](development.md)。

## 问题分类与代码审查

让问题报告能够复现：确认 OC 版本、平台、命令、预期和实际结果，以及相关服务端基线。要求提供已脱敏的证据，不索取原始凭据文件。潜在漏洞按[安全说明](security.md)处理；普通支持和功能讨论使用本仓库的 issue。

新增命令、依赖或兼容性调整，先确认范围再进入实现审查。检查相关命令的帮助、退出状态、JSON 错误、取消和迁移行为是否一致。

涉及数据写入、删除、替换或同步的修改，需要验证失败路径和成功路径。根据改动范围检查所有者、权限、部分写入、清理、重试和信号处理。测试和示例使用一次性样本；完整测试通过也不能代替对潜在数据丢失路径的审查。

结合实际改动和环境阅读测试结果。[Go 工作流](../../.github/workflows/go.yml)记录平台、竞态、服务端集成、依赖清单和漏洞检查，[CodeQL](../../.github/workflows/codeql.yml)另行分析。本地运行不能证明所有平台的运行结果，交叉编译也不会扩大[兼容范围](compatibility.md)。

日常检查优先采用开发指南中的直接命令和本地临时集成环境。`make test`、`make check` 和 `make verify` 还会调用旧 `functional-tests.sh`；没有设置 `SERVER_ENDPOINT` 时，脚本默认连接上游公共服务器 `play.min.io`，并执行对象和桶的写入、删除。只有审查其环境及清理要求，并显式配置一次性测试目标后，才运行这个旧套件。

PR 描述和发布说明围绕最终改动组织：触发条件、修改后的行为、用户影响、验证和剩余限制。拆分无关工作，便于审查。参数或默认值变化时，一起检查中英文文档和示例。

## 依赖与许可证

依赖使用 Go modules 管理。一起审查有意调整的 `go.mod` 和 `go.sum`，说明引入或升级依赖的原因。不要提交本地 `replace`，也不要通过批量升级依赖绕过失败的测试环境。

OtterIO SDK、工具链基线和服务端补丁记录在[兼容清单](../compatibility.json)中，由 `buildscripts/verify-release-boundaries.py` 检查。调整基线时，相关测试、文档和边界检查需要放在同一组已审查改动中。

固定服务端源码需要按兼容清单顺序应用 `otterio-core-compat.patch`、`otterio-runtime-compat.patch`、`otterio-http-api-compat.patch` 三项补丁。这些补丁应用到临时复制的源码，不修改共享模块缓存，也不改变 OC 的 SDK 固定依赖。[开发指南](development.md#复现服务端集成环境)提供具体步骤。

保留 [LICENSE](../../LICENSE)、[NOTICE](../../NOTICE)、[CREDITS](../../CREDITS)、源码版权声明和通知组件的 MIT 许可证。归档与镜像打包必须携带适用的许可证文件。保留的上游名称用于说明来源和兼容关系，不表示与 MinIO, Inc. 存在隶属或背书关系。Go module 路径仍为 `github.com/soulteary/mc`，可执行文件名称为 `oc`。

## 文档与兼容范围

以按任务组织的指南作为当前用户入口：[安装](installation.md)、[配置](configuration.md)、[使用](usage.md)、[迁移](migration.md)、[管理](administration.md)、[排错](troubleshooting.md)、[兼容](compatibility.md)和[开发](development.md)。保留 `oc-phase-*` 文档作为设计和验证历史，不把历史示例直接当作当前操作说明。

说明每项检查能够证明的范围。构建目标、平台运行测试、服务端验收和外部服务商支持是不同的验证层次。没有实际项目决定和证据时，不添加 SLA、支持版本范围、维护者邮箱或生产环境保证。

[行为准则](../../code_of_conduct.md)保留 Contributor Covenant 归属和许可证澄清。保持执行联系途径准确，并与漏洞报告渠道分开。目前 OC 没有公开专用的私密行为报告渠道；上游 MinIO 地址不是 OC 联系地址。在公开渠道请求私密联系方式时，不得披露事件的私密细节。

## 发布版本

按[发布指南](releasing.md)操作，不使用上游旧维护指南中的 `make release` 建议。当前工作流直接构建归档，`.goreleaser.yml` 的发布功能关闭。

创建时间戳标签前，审查 `RELEASE_NOTES.md`，确认干净的 main 位于获取后的远程提交，并检查 Go 和 CodeQL 在同一 main 提交上均已通过。PR 检查通过不够。`buildscripts/release-preflight.py` 只检查前提，不创建标签，也不证明远程 CI 已通过。标签时间采用 UTC 的 `RELEASE.YYYY-MM-DDTHH-MM-SSZ`。

当前发布工作流生成 11 个归档、发布清单和校验文件，以及不可覆盖的 Linux amd64/arm64 镜像。GHCR 使用工作流 token；可选 Docker Hub 发布需要同时设置 `DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。两项都不设置时跳过，只设置一项时发布失败。PR 测试任务不得获取镜像仓库凭据，也不能把它们暴露给不可信代码。说明匿名 GHCR 拉取前，确认包的可见性。

较早的纯归档版本不能证明镜像存在：检查清单的 `images` 字段和实际镜像仓库，包括 `RELEASE.2026-10-07T14-10-00Z` 在内的无镜像版本不能用作拉取示例。提升流程拒绝没有镜像身份记录的清单。

工作流按摘要验证镜像，在原有归档清单中记录镜像身份，重新计算清单校验值，并在正式发布前下载、比较草稿附件。发布任务与整个稳定版本提升工作流共用 `oc-stable-promotion`。修改工作流时保留这个共享临界区，不要把同一锁放到可复用工作流的调用者上：调用者必须先释放锁，提升才能获得锁。

只有最新已发布的稳定时间戳版本能把记录的摘要提升为 `latest`。先验证镜像别名，再更新 GitHub latest 标记。提升不重新构建、不改变版本标签，也不重新生成发布附件。

## 恢复与版本支持

不要为了重试发布而移动标签或覆盖已有版本镜像。一旦任一版本镜像已经推送，等待 main 检查通过后创建新的时间戳标签。正式发布成功但提升失败时，保留版本，针对其标签重试 **Stable release promotion**。已经存在更新的正式版本时，旧版本不能回退镜像别名。

重试前检查部分结果和草稿状态。跨服务发布不是原子事务，支持的恢复路径见[发布指南](releasing.md#校验与恢复)。针对固定版本调查回归，保存已脱敏的复现证据；修复影响用户行为时，更新兼容或迁移说明。自动自更新和上游 MinIO 上传渠道均未启用。
