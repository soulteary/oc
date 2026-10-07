# 兼容性与验证范围

[English](../compatibility.md) · [文档首页](README.md)

OC 的兼容承诺限于已有通过记录的版本、部署和操作。API 能编译、帮助中存在命令，都不足以证明兼容。机器可读基线见 [compatibility.json](../compatibility.json)。

## SDK 固定版本与服务端测试基线

当前 Go 工具链、固定 OtterIO SDK 版本、完整服务端源码 SHA 和 CLI 框架版本记录在 [compatibility.json](../compatibility.json)。OC 的 module 名保持 `github.com/soulteary/mc`。

客户端与集成服务端使用同一固定远程 OtterIO 源码，不使用本地替换或兼容补丁。管理查询桥接、运行时关闭、HTTP API、账户信息和条件写修复已包含在该源码中。旧补丁与旧报告保留为历史证据，不再作为当前环境搭建步骤。更新固定版本后必须重跑验收矩阵，见[开发指南](development.md)和 [CLI 迁移说明](../cli-migration.md)。

## 测试工具覆盖的部署

核心矩阵包含：

- `single-http`：对象与管理接口共用 HTTP 监听。
- `dual-http`：对象与管理接口使用独立 HTTP 监听。
- `single-tls`：共用 TLS 监听。
- `dual-tls`：独立 TLS 监听，分别验证证书信任。
- `dual-http-public`：独立 HTTP 监听，指标公开访问。

稳定性测试使用 `dual-tls`。高级验收使用单节点四盘纠删码部署。这些场景不等于多节点分布式矩阵。独立 CLI 验收还覆盖本地文件系统 NAS gateway 和以本地 OtterIO 为上游的 S3 gateway，包括文件操作和信号退出；不代表外部服务提供商或其他 gateway 后端已经兼容。

对象验收覆盖桶操作、空对象与小对象、特殊字符和非 ASCII 名称、65 MiB 分片传输与下载哈希、服务端复制、stat、mirror、分享及权限失败。高级检查覆盖部分 IAM 与服务账号操作、服务配置往返、配额、对象版本与标签、对象锁与保留、生命周期配置、CSV Select、SSE-C、实时事件、管理流、profile / 健康输出、heal 状态及服务控制。

这些检查各有边界：生命周期配置往返不代表真实到期删除；heal 状态不代表故障磁盘恢复；KMS 拒绝不代表 KMS 加密通过；实时订阅不代表外部通知目标投递通过。入口和权限要求见 [管理说明](administration.md)。

## 原生 CI 与交叉编译

[Go CI](../../.github/workflows/go.yml) 配置了 Linux、macOS、Windows 的原生单元和竞态测试。真实 OtterIO 集成配置在 Linux / macOS 上分别使用 `CGO_ENABLED=0`、`1`，先构建固定版本服务端，再执行矩阵。工作流无论成功或失败都会归档报告和诊断证据。

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

交叉编译通过只证明能生成目标程序，不能替代原生运行、文件系统 / ACL 测试或真实服务联调。Windows、FreeBSD 等目标没有与 Linux / macOS 相同的服务集成验证记录。当前发行工作流为后续时间戳版本配置了 Linux amd64、arm64 容器发布。旧版可能只有程序归档；只有发行清单记录了 `images`，才能据此使用镜像标签或摘要。安装方法见 [安装说明](installation.md)。

工作流配置不等于运行通过。应在 [GitHub Actions](https://github.com/soulteary/oc/actions) 中检查对应源码提交的实际结果。本地报告记录二进制哈希和运行平台，只为对应运行提供证据，不自动覆盖后续每个发行版。

## 稳定性预算的含义

当前 [预算值](../compatibility.json) 为：

- 单次测量传输不超过 120 秒。
- OC 主进程采样 RSS 不超过 512 MiB。
- 测量取消耗时不超过 5 秒。
- 短期持续采样的内存增长不超过 64 MiB。
- 测量吞吐至少为 5 MiB/s。
- 相对同主机、同次运行的参考传输，吞吐下降不超过 50%。

工具使用 65 MiB 对象和 1、4 两种并发数，核对下载内容，检查取消与分片清理，并测试限速及中断连接。CI 请求 30 秒持续采样，已有本地验收记录还包含 60 秒采样。

这些是严重回归的检查阈值，不是生产容量建议或性能 SLA。Linux / macOS 每 100 ms 采样一次 OC 主进程 RSS，不统计整棵进程树和服务端，也可能错过短暂峰值。吞吐包含程序启动和本机文件系统开销。30 或 60 秒采样不能证明长时间稳定性；部署前仍需使用自己的代表性负载验证。

## 需要独立验收的功能

兼容清单明确列出以下未验证范围：

- 分布式拓扑、其他 gateway 后端和外部 gateway 上游。
- 外部 KMS、外部通知目标。
- 跨实例复制。
- 第三方 S3 服务和历史 OtterIO 版本。
- 长时间持续运行和磁盘耗尽恢复。

接入其他 S3 服务时，应验收实际使用的对象操作、认证、寻址、加密、分片行为和权限语义。OtterIO 管理命令需要 OtterIO 管理 API，不能从 S3 兼容性推导出管理兼容性。

实时通知没有持久化重放游标，重连不保证补发断线期间的事件。需要完整历史时，应使用持久化通知目标和消费确认机制。`mirror --watch` 的周期核对用于当前状态收敛，不提供完整事件审计，见 [使用说明](usage.md)。

SDK 流修复、SDK 拆分和 SDK 独立发布仍列为暂缓事项。CLI 迁移更新了 SDK 固定源码版本，SDK 的流投递行为不在本次变更范围内。`MC_*` 在 OC 0.x 系列内继续兼容，移除前至少提前一个次版本公告，见 [迁移说明](migration.md)。

## 查看验证记录

[阶段二](../oc-phase-two.md) 记录核心入口和 CA 验收；[阶段三](../oc-phase-three.md) 及其 [结果](../oc-phase-three-results.json) 记录迁移和高级操作；[阶段四](../oc-phase-four.md) 及其 [结果](../oc-phase-four-results.json) 记录稳定性和后续联合审查，包括二进制哈希及本地平台信息。

这些文档是历史实现与验证记录。其中早期关于发布功能禁用的说明描述的是对应阶段；当前发布流程以 [发行说明](releasing.md) 为准。部分完成的报告和跳过的测试不能算作完整矩阵通过。
