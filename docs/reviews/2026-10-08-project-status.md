# OC 项目现状与文档复查：2026-10-08

[项目入口](../../README_zh_CN.md) · [文档目录](../zh_CN/README.md) · [兼容基线](../compatibility.json)

本记录以 `main` 提交 [`58d3bd81b7eee69d39f14bcb7fb0177aa72ced32`](https://github.com/soulteary/oc/commit/58d3bd81b7eee69d39f14bcb7fb0177aa72ced32) 为源码基线，日期使用 Asia/Shanghai。源码、已发布附件、工作流和已有验收报告分别核对；历史报告只证明其中记录的二进制与场景。初次核查仅修改文档，后续已纳入下述 CLI 合同检查修复；没有修改运行时代码、发布版本或重跑完整服务端矩阵。

## 1. 当前结论

OC 已有独立 CLI 发布、明确的 mc 配置迁移路径和较完整的日常操作文档。本机 Web 控制台已经交付浏览、下载、可选对象写入，以及配套协议下的桶配置和原生 IAM 自身改密。生命周期转换、持久化目标引用和指定版本恢复也有本机验收证据。

这些能力处在不同交付阶段：最新 CLI 发布并不包含 main 上的控制台和 ILM 新功能；当前服务端依赖也不包含 P3 设置、改密与转换协议。不能只安装一个最新 CLI 归档，就认为整个控制台迁移或新服务端能力已经完成。

现有文档的主要问题是最新实现未同步到入口、发布说明和开发指南，以及部分配置/权限行为说明不充分。另有一个实际 IAM 策略更新缺陷，需要在后续代码工作中解决；CLI 合同差异已按下述流程登记。

## 2. 发布、依赖与产品边界

截至本次核查，最新发布为 [`RELEASE.2026-10-07T17-07-26Z`](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-07T17-07-26Z)，附件清单记录源码 `eeb95dbeeca8d80ae3e86e133228615ba84e02ce`、Go 1.27.1、11 个 CLI 平台归档，以及 `ghcr.io/soulteary/oc` 镜像摘要 `sha256:492f6dfbaacb67b3cf091d1c9c3f785c51ae457069923fad2ce0b3e7970adc45`。这里核验的是发布清单，没有重新拉取镜像运行，也没有把历史版本的依赖清单改成当前 main 的依赖。

当前 main 的 [go.mod](../../go.mod) 与[兼容清单](../compatibility.json)记录：

- Go module 仍为 `github.com/soulteary/mc`，仓库与程序名为 OC；历史 module 名称不是安装渠道。
- 工具链为 Go 1.27.1，CLI 框架为 `urfave/cli/v3 v3.14.0`。
- S3 客户端为独立的 `github.com/soulteary/otterio-sdk/v7 v7.3.1`。
- 服务端/管理包为 `github.com/soulteary/otterio v0.0.0-20261008035209-6f6d0835ddff`。
- kits 分别固定 `crc64nvme v1.1.2` 和 `md5-simd v1.1.3`。

核心 CLI 测试实例使用固定 OtterIO 源码，不再需要旧兼容补丁。`requiredServerPatches=[]` 描述这一核心基线。当前 [`console-server-p3.patch`](../../buildscripts/console-server-p3.patch) 则是另一条可选实例：它加入尚未进入依赖 pin 的桶配置、自身凭据和生命周期协议，不是已发布服务端版本，也不是默认客户端构建的一部分。

## 3. 实现结构与维护情况

该基线跟踪 368 个 Go 文件，其中 77 个为 `_test.go` 文件。文件数量描述规模，不能用作功能覆盖率。主体 CLI 仍集中在 `cmd/`，保留历史命令结构、help 和全局状态；[CLI 迁移说明](../cli-migration.md)记录通过冻结旧程序合同维护参数与输出兼容的方式。

新控制台已分出明确边界：

- [`cmd/oc-console`](../../cmd/oc-console/main.go)负责配置、回环监听和进程生命周期。
- [`internal/storageclient`](../../internal/storageclient/client.go)持有 S3/管理身份、签名客户端和传输资源。
- [`internal/consoleapi`](../../internal/consoleapi/types.go)定义 Web 与存储之间的数据结构。
- [`internal/console`](../../internal/console/server.go)负责登录、会话、CSRF、并发和任务。
- [`internal/console/web`](../../internal/console/web/index.html)以内嵌静态资源提供界面，构建及运行不需要 Node；前端行为测试需要 Node。
- [`internal/clienttransport`](../../internal/clienttransport/transport.go)共享地址与 CA 校验、独立管理信任和禁止重定向规则。

控制台无需通过 CLI 命令处理函数调用存储。这有利于独立管理会话和取消，但 CLI 与控制台的配置解析、信任和错误语义仍需共同回归。沿用的通知实现还有独立 MIT 许可证；[正式打包](../releasing.md)需保留各组件归属。

## 4. 功能成熟度与验证依据

### CLI 与常规对象操作

客户端提供本地/S3 复制、查询、mirror、版本与对象锁，以及 OtterIO 身份、策略、配置、监控与诊断命令。S3 能力取决于具体服务端；`oc admin` 是 OtterIO 管理 API，不是通用 S3 管理接口。

核心实例覆盖五种 HTTP/TLS/端口配置，并在扩展测试中覆盖临时四盘 erasure、选定管理和稳定性行为。[兼容指南](../zh_CN/compatibility.md)与[工作流](../../.github/workflows/go.yml)区分实际验收、CI 配置及未验证场景。11 个交叉编译目标不等于 11 个平台均有原生运行验收。

通知流没有持久化重放游标。本地 `mirror --watch` 可以重新订阅并核对当前状态，但不能恢复已经出现又消失的对象历史；远程事件重连也不保证断线期间事件完整。

### 本机 Web 控制台

默认支持浏览、流式下载和桶配置读取；`--allow-writes` 增加上传、确认后删除及任务取消。它只服务本机一个操作员和一个固定别名，不提供共享部署、OIDC 或多人身份隔离。现有 OtterIO Web 仍保留。

默认上传要求服务端具备原子条件写能力。受保护桶设置另外要求 P3 协议、S3v4、对应读取及修改权限；保存后还会读回核对。自身改密只适用于符合条件的 enabled 原生 IAM 用户，root、STS 和服务账号不能使用此入口。改密结果确认或不确定后，进程会撤销会话并要求终端核实、更新别名、重启。

当前 Web 尚无用户/组/服务账号管理面板、诊断流、历史版本选择、Range、ZIP、分享或恢复按钮。它通过 `make build-console` 单独构建，不在当前 CLI 发布归档和镜像中。[使用指南](../zh_CN/console.md)描述当前行为；[P3 记录](../console-phase-three.md)保留早期补丁证据。

### 生命周期转换与恢复

CLI 新增带 label 的 ILM 目标管理，修改前要求签名的生命周期协议发现。配套运行时在单节点、单 pool erasure 上实现当前/非当前转换、持久化准确目标与版本引用、指定版本恢复和远端删除保护。FS 转换、分布式、多 pool、SELECT restore、`NewerNoncurrentVersions` 及外部 S3 提供者互操作仍未纳入这次保证。

最新[验证汇总](../lifecycle-transition-verification.json)记录 macOS arm64、本机真实服务、当前 61 文件补丁及摘要；[执行报告](../lifecycle-transition-integration-results.json)覆盖 `single-http`、`dual-tls` 两场景，各 9 组检查，并另有设置、旧服务端和对象写回归报告。其范围包括源服务重启、删除规则后持久引用、准确版本恢复和删除顺序。旧 P3 记录中拒绝非当前转换的结论属于当时的实现，不能覆盖这份后续结果。

本次核对补丁 SHA-256 与该汇总一致：`a0281fc4489fc8c145f78bd1cb50eb41ae50252b812761ccc3340d4b626e18d9`。这只证明仓库补丁身份相符，不等于本次重跑其验收。存在新转换引用时，旧服务端无法理解远端位置，不能直接降级；关闭 OC 或返回旧 UI 也不会撤销已经保存的规则或转换数据。

## 5. 当前问题与处理顺序

### 优先修复 IAM 策略更新错误

[`admin-policy-update.go`](../../cmd/admin-policy-update.go) 中 `updateCannedPolicies` 遇到重复追加或空策略返回错误和空结果，但调用方判断的是此前创建管理客户端的 `err`，不是本次的 `e`；随后仍把空结果传给 `SetPolicy`。这可能清空已有用户/组策略。本次依据源码确认调用路径，未对使用者的账号执行该操作。

修复前不要将 `policy update` 作为幂等追加操作。先查询用户/组的现有绑定，再用 `policy set` 提交经过核对的完整策略集合；`set` 本身会替换绑定，并非追加。后续代码修复应验证重复、空输入与部分重复列表均不发送修改请求，并验证正常追加保留既有集合。

### CLI 合同差异与后续修复

在 2026-10-08 21:30（Asia/Shanghai）核查时，基线 main 的 [CLI compatibility 运行](https://github.com/soulteary/oc/actions/runs/37783653305)已失败。Linux/macOS 日志显示 `admin bucket remote add` 和 `edit` 两个 help 合同差异：新增 `--label`，且 add 的 `--service` 范围加入 `ilm`。日志末尾为 `CLI contract differs in 2 case(s); baseline was not updated`。应按既有合同流程审阅和记录这项有意变化，而非笼统更新全部快照。

同一时点 [Code scanning - action](https://github.com/soulteary/oc/actions/runs/37783653213)已通过，[Go](https://github.com/soulteary/oc/actions/runs/37783653208)仍运行。此处是该时点和该 SHA 的状态，不是后续提交或文档 PR 的 CI 结论。

提交本文前再次核对同一 main SHA：Go 已完成并通过，CodeQL 通过，CLI compatibility 仍失败。完整构建检查通过不会自动消除独立合同差异。

后续修复已纳入 [PR #14 的合同修复提交 `4c7d73b5`](https://github.com/soulteary/oc/commit/4c7d73b50654cf7a6fc966c78c8d485c7cd2ce90)：在 `approved-deltas.json` 中记录上述两项 Unix、Windows 完整前后 stdout，并让检查器选择平台对应的旧值。固定历史基线、命令目录和工作流保持不变，其他输出、退出码及配置副作用仍严格比较。[该修复的原生三平台 CI](https://github.com/soulteary/oc/actions/runs/37785550405)已全部通过，每个平台匹配 339 个合同案例，并通过适用的检查器回归测试。这里记录的是该修复提交的结果，本分支后续提交仍需独立通过 CI。

现行发布工作流只自动要求 Go 和 CodeQL 通过，没有把独立 CLI 合同工作流纳入门槛；因此即使这两个检查通过，也仍需人工核对该提交的合同检查结果。发布指南已补充这一步，自动门槛的调整属于后续代码工作。

### 先交付服务端协议，再扩大控制台支持

配套服务端源码需要独立审查、合并与发布，再更新 OC 的固定版本和联合验收。仅“补丁可应用”不能替代这个流程。随后再完成剩余 P3 管理/诊断、目标平台原生验收、控制台归档/镜像、升级与回退演练。共享模式、OIDC 与旧 Web 移除仍按[迁移门槛](../console-migration.md)单独推进。

### 清理本地构建渠道的历史元信息

[`Dockerfile`](../../Dockerfile)、[`Dockerfile.dev`](../../Dockerfile.dev) 的源码标签及本地 [GoReleaser 配置](../../.goreleaser.yml) 的包主页仍指向历史 `soulteary/mc`。正式 [`Dockerfile.release`](../../Dockerfile.release) 已指向 `soulteary/oc`，但本地构建产物的归属元信息仍需后续统一。旧 [`buildscripts/build.sh`](../../buildscripts/build.sh) 使用 `MC_RELEASE` 和四目标交互打包，不是目前的正式发布入口；维护指南应继续指向现行 Python 打包与 GitHub 工作流。

## 6. 本次文档修订与复查范围

此次修订同步英文和中文任务指南：

- 更新入口、控制台和发布说明，区分已发布 CLI、源码控制台、未 pin 服务端协议。
- 用已核验的真实发布标签演示安装与容器，并说明镜像以各版本清单为准。
- 补充重设别名会整体替换设置、`alias list/remove` 只操作配置文件、环境覆盖须另行检查。
- 准确说明 `doctor.adminSDK` 指服务端/管理模块，不是独立 S3 SDK。
- 补齐桶配置编辑、自身改密、ILM 目标权限及结果不确定时的处理步骤。
- 更新 Node 测试前提、双服务端实例复现方式和官方/本地打包差别。
- 修正“返回旧 UI 就可回退服务端”的泛化描述，保留旧阶段和机器可读报告的原始内容。

本次从该基线重新构建 CLI 与控制台，使用隔离临时配置核对帮助和受影响示例；运行相关别名、诊断与协议回归、维护检查、前端行为测试及发布边界检查。链接、锚点及 Shell 语法另行复查。没有使用真实别名、生产账号或存储服务，也没有把文档验证当作新的完整集成验收。

文档检查覆盖本次变更的 30 份 Markdown、484 个本地链接、16 个标题锚点和 129 个 Shell 命令块。14 项维护检查、29 组前端行为检查与相关 Go 回归均通过；Windows PowerShell 示例没有在本机原生执行。
