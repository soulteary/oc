# 兼容性与验证范围

[English](../compatibility.md) · [文档首页](README.md)

OC 的兼容承诺限于已有通过记录的版本、部署和操作。API 能编译、帮助中存在命令，都不足以证明兼容。机器可读基线见 [compatibility.json](../compatibility.json)。

## SDK 固定版本与服务端测试基线

当前 Go 工具链、独立 S3 SDK、OtterIO kits、服务端 / 管理模块及完整源码 SHA 记录在 [compatibility.json](../compatibility.json)。OC 的 module 名保持 `github.com/soulteary/mc`。

S3 操作使用独立发布的 `github.com/soulteary/otterio-sdk/v7 v7.3.1`，发布源码为 `c11549d350d8d1f7474bc26037616e912f344c15`，并采用已发布的 kits `crc64nvme v1.1.2`、`md5-simd v1.1.3`。清单中的 `storageSDK` 与 `otterioKits` 分别记录这些依赖。旧字段 `otterioSDK` 继续记录服务端 / 管理模块版本，保留已有验收及发布身份读取器的兼容性；该字段不是独立 S3 SDK 的版本。

发布边界检查将 SDK 版本和完整源码 SHA，与 `buildscripts/verify-release-boundaries.py` 中独立固定、已经审查的标签与源码映射比较。SHA 长度正确并不能证明发布身份。升级 SDK 时，必须先核实已发布标签的源码，再与依赖及兼容清单一起更新该映射；检查本身不访问网络。

服务端 / 管理包与核心集成服务端使用同一固定远程 OtterIO 源码 `6f6d0835ddff68020f1491c403b958fade22841f`，不使用本地替换或兼容补丁。其源码树与原先的 `fed9cc3` pin 一致；此次更新记录正式合并的 main 来源。管理查询桥接、运行时关闭、HTTP API、账户信息和条件写修复已包含在该源码中。`otterio-*-compat.patch` 与旧核心报告保留为历史证据，不再作为当前环境搭建步骤；独立的可选控制台协议环境见下文。任一依赖更新后都必须重跑验收矩阵，见[开发指南](development.md)和 [CLI 迁移说明](../cli-migration.md)。

## 测试工具覆盖的部署

核心矩阵包含：

- `single-http`：对象与管理接口共用 HTTP 监听。
- `dual-http`：对象与管理接口使用独立 HTTP 监听。
- `single-tls`：共用 TLS 监听。
- `dual-tls`：独立 TLS 监听，分别验证证书信任。
- `dual-http-public`：独立 HTTP 监听，指标公开访问。

稳定性测试使用 `dual-tls`。高级验收使用单节点四盘纠删码部署。这些场景不等于多节点分布式矩阵。独立 CLI 验收还覆盖本地文件系统 NAS gateway 和以本地 OtterIO 为上游的 S3 gateway，包括文件操作和信号退出；不代表外部服务提供商或其他 gateway 后端已经兼容。

对象验收覆盖桶操作、空对象与小对象、特殊字符和非 ASCII 名称、65 MiB 分片传输与下载哈希、服务端复制、stat、mirror、分享及权限失败。高级检查覆盖部分 IAM 与服务账号操作、服务配置往返、配额、对象版本与标签、对象锁与保留、生命周期配置、CSV Select、SSE-C、实时事件、管理流、profile / 健康输出、heal 状态及服务控制。

这些核心检查各有边界：生命周期配置往返不代表真实到期删除；heal 状态不代表故障磁盘恢复；KMS 拒绝不代表 KMS 加密通过；实时订阅不代表外部通知目标投递通过。入口和权限要求见 [管理说明](administration.md)。

## 可选控制台协议与生命周期环境

未补丁固定 pin 继续作为核心与旧协议安全降级基线。OtterIO 当前源码 HEAD 已包含 P3，但 OC 没有悄悄更新依赖 pin；导出补丁只应用于固定 pin 的可写副本。

`console-server-base.patch` 独立提供保护性设置与自身改密。其条件生命周期仅支持前缀到期和非当前到期，拒绝转换、标签过滤及到期删除标记规则；已有配置仍可完整读取，普通无条件 S3 行为保留。base 后必须叠加 `lifecycle-storage.patch` 和 `lifecycle-storage-hardening.patch`，才构成当前生命周期部署来源。hardening保留HEAD80已合入的12个storage-class snapshot、覆盖quorum和tier元数据保护源码/测试文件；当前源码未启用独立IAM补丁，不能声称与固定pin core组合全等。

五功能先以 base + versions + IAM 验收，不依赖存储层。另一份 base + storage + hardening 程序执行独立归档端生命周期验收，第四份程序检查完整组合。GET/HEAD、CopyObject 和 UploadPartCopy 版本授权均有明确回归。旧 `console-server-p3.patch` 及原阶段三、生命周期、五功能报告保留原身份，不再用作当前部署输入。

复现见[开发指南](development.md#复现可选控制台协议环境)。base设置/对象、独立五功能/copy、硬化生命周期、完整五功能/copy及设置/对象报告已本地通过；完整组合生命周期与汇总也已通过。这些结果来自本地macOS arm64，配置的远程CI尚未由本任务运行。见[五功能记录](../console-features.md)、[生命周期范围](../lifecycle-transition.md)及[兼容清单](../compatibility.json)的 `consoleServerAcceptance`。最终加固组合的Go/根module与独立current+IAM projection逐字一致；实际current缺IAM，嵌套Mint module仍不同。本地单节点验收不证明外部提供者、分布式、gateway或FS转换通过。控制台仍从源码构建，当前CLI发行归档和镜像不包含它。

## 原生 CI 与交叉编译

[Go CI](../../.github/workflows/go.yml) 配置了 Linux、macOS、Windows 的原生单元、竞态、控制台前端与辅助程序测试。真实 OtterIO 集成配置在 Linux / macOS 上分别使用 `CGO_ENABLED=0`、`1`：先测试未补丁固定服务端，再独立构建 base、base + versions + IAM、base + storage + hardening 与完整组合，分别验收并归档。工作流无论成功或失败都会归档报告和诊断证据。独立的 [CLI 兼容工作流](../../.github/workflows/cli-compat.yml)在各原生 CI 平台比较固定基线与候选程序。

交叉编译包含 11 个目标：

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

交叉编译通过只证明能生成目标程序，不能替代原生运行、文件系统 / ACL 测试或真实服务联调。Windows、FreeBSD 等目标没有与 Linux / macOS 相同的服务集成验证记录。当前发行工作流为后续时间戳版本配置了 Linux amd64、arm64 容器发布，验证索引中的两个平台，但只在 Linux amd64 上运行容器冒烟测试。旧版可能只有程序归档；只有发行清单记录了 `images`，才能据此使用镜像标签或摘要。安装方法见 [安装说明](installation.md)。

工作流配置不等于运行通过。应在 [GitHub Actions](https://github.com/soulteary/oc/actions) 中检查对应源码提交的实际结果。本地报告记录二进制哈希和运行平台，只为对应运行提供证据，不自动覆盖后续每个发行版。

## 稳定性预算的含义

当前 [预算值](../compatibility.json) 为：

- 单次测量传输不超过 120 秒。
- 传输期间 OC 主进程峰值 RSS 不超过 512 MiB。
- 测量取消耗时不超过 5 秒。
- 短期持续采样的内存增长不超过 64 MiB。
- 测量吞吐至少为 5 MiB/s。
- 相对同主机、同次运行的参考传输，吞吐下降不超过 50%。

工具使用 65 MiB 对象和 1、4 两种并发数，核对下载内容，检查取消与分片清理，并测试限速及中断连接。CI 请求 30 秒持续采样，已有本地验收记录还包含 60 秒采样。

核心吞吐检查为每种上传 / 下载操作及并发数保留一次预热和固定三组测量 / 参考配对。两边通过同一个已预热的线程池创建新的远程对象或本机文件；即使测量组有四个并发传输，参考组仍只有一个传输。三组配对速度比的中位数必须至少为 50%，真实测量组吞吐的中位数至少为 5 MiB/s。计时从 CLI 启动到观察到进程退出，不包含单独记录的 RSS 采样线程清理时间。预热、测量及参考传输均保留 120 秒超时和 512 MiB 进程 RSS 预算；每个上传对象都下载核对 SHA256。报告保留全部样本，失败后不追加尝试。

Console 写入验收固定测量五组 CLI / console 配对上传，交替先后顺序，并逐个下载核对大小与 SHA256。配对速度比的中位数必须至少为 50%，console 中位吞吐至少为 5 MiB/s；保留全部样本，失败后不追加采样。

这些是严重回归的检查阈值，不是生产容量建议或性能 SLA。Linux / macOS 每 100 ms 采样一次 OC 主进程 RSS。传输检查同时保留操作系统在该进程退出时记录的峰值 RSS，并按两种测量中的较大值检查预算，因此首次采样前结束的快速传输也有内存记录。报告分别保存采样峰值、退出时记录的峰值和合并后的进程峰值。这些测量不统计服务端或整棵进程树；console 和持续采样仍可能错过短暂峰值。吞吐包含程序启动和本机文件系统开销。30 或 60 秒采样不能证明长时间稳定性；部署前仍需使用自己的代表性负载验证。

## 需要独立验收的功能

兼容清单明确列出以下未验证范围：

- 分布式拓扑、其他 gateway 后端和外部 gateway 上游。
- 外部 KMS、外部通知目标。
- 跨实例复制。
- 第三方 S3 服务和历史 OtterIO 版本。
- 长时间持续运行和磁盘耗尽恢复。

接入其他 S3 服务时，应验收实际使用的对象操作、认证、寻址、加密、分片行为和权限语义。OtterIO 管理命令需要 OtterIO 管理 API，不能从 S3 兼容性推导出管理兼容性。

实时通知没有持久化重放游标，重连不保证补发断线期间的事件。需要完整历史时，应使用持久化通知目标和消费确认机制。`mirror --watch` 的周期核对用于当前状态收敛，不提供完整事件审计，见 [使用说明](usage.md)。

SDK 提取及独立发布已经完成；SDK 流修复仍延期，本次不改变通知投递行为。`MC_*` 在 OC 0.x 系列内继续兼容，移除前至少提前一个次版本公告，见 [迁移说明](migration.md)。

## 查看验证记录

当前 CLI 迁移及对应提交的检查见 [OC PR #7](https://github.com/soulteary/oc/pull/7)
和 [OtterIO PR #30](https://github.com/soulteary/otterio/pull/30)。CLI 快照、程序模块清单和
联合验收报告由关联工作流上传，可在对应检查中下载。

SDK/kits 迁移见 [OC PR #8](https://github.com/soulteary/oc/pull/8)和 [OtterIO PR #31](https://github.com/soulteary/otterio/pull/31)；已经合并的服务端存储就绪验收修正见 [OtterIO PR #32](https://github.com/soulteary/otterio/pull/32)。[2026-10-08 发布准备记录](../releases/2026-10-08-release-review.md)列出本次发布比较基线及尚需执行的发布门槛。

当前合同覆盖 OC 339 项、OtterIO 133 项 CLI 调用。批准的 health 用法错误渲染修复，
将旧版 panic 和退出码 2 改为具体参数错误和退出码 1。非法健康选择器、时长、布尔值
及未知参数各有独立精确批准差异；范围与保留的原始证据见 [CLI 迁移说明](../cli-migration.md)。

[阶段二](../oc-phase-two.md) 记录核心入口和 CA 验收；[阶段三](../oc-phase-three.md) 及其 [结果](../oc-phase-three-results.json) 记录迁移和高级操作；[阶段四](../oc-phase-four.md) 及其 [结果](../oc-phase-four-results.json) 记录稳定性和后续联合审查，包括二进制哈希及本地平台信息。

这些文档是历史实现与验证记录。其中早期关于发布功能禁用的说明描述的是对应阶段；当前发布流程以 [发行说明](releasing.md) 为准。部分完成的报告和跳过的测试不能算作完整矩阵通过。
