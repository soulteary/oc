# Console 迁移候选验收记录

此记录对应 2026-10-13 的本地验收，后续 CI 或发行需产生自己的报告。
它只记录准确程序和路径的结果，不认证新正式版本、镜像或所有平台。
替代范围见 [28 项能力映射](console-migration-capabilities.md)，执行入口与
发行条件见 [候选指南](console-migration-candidate.md)。

## 准确程序与通过结果

[跨版本升级报告](evidence/console-migration/upgrade-darwin-arm64.json) 的
`passed` 为 true，`single-tls`、`dual-tls` 两项及进程清理均通过。报告生成
时间为 2026-10-12 19:22 UTC（北京时间 2026-10-13 03:22），使用 darwin/arm64
实际程序和临时单节点四存储目录纠删码样本，不认证四物理盘部署，也没有使用
操作者现有配置、账号或数据卷。

旧程序是 OC `RELEASE.2026-10-10T11-42-18Z`（源码
`ee1846d587a6548db4e595d653ca4f54baea2b1a`）和 OtterIO
`RELEASE.2026-10-10T16-38-05Z`（源码
`8cc518b31ac8b69c8f855e19b4bd027f44275c47`）。旧 OC archive、提取的两个
程序字节和官方 checksum/manifest 绑定，旧 OtterIO 程序绑定官方 checksum
和 manifest；报告保留旧 OtterIO 实际 `embeddedModified: true`，没有把它
改称干净源码构建。

候选程序来自干净源码 OC `26a0fa6c4d2f380ff9eba84c9dcb91a378b73c27` 和
OtterIO `c3b3665154264993fa06a28e95ab25fdd03d7c0c`；三程序的嵌入 revision、
`embeddedModified: false`、SHA-256 与验收脚本摘要记录在 JSON。当前文档/CI
准备提交的新 PR head 不是这个已测试的 OC revision，必须重新构建并运行。

通过范围包括：

- 单 HTTPS S3/Admin 入口，以及不同端口、独立 CA 的双 HTTPS 入口；候选
  省略 browser 配置及显式 off 均可启动，五类旧 Web 路由确认拒绝。
- 普通对象、两个历史版本、标签、桶 versioning、用户/组/policy 的有效读取
  与明确拒绝保持；CLI 配置字节保持。没有宣称覆盖所有 IAM 配置变体。
- 真实旧程序到新程序，再用同一存储回退旧程序：分别在没有新写入和新增一个
  对象后验证可读；从升级前快照恢复后，新写入按预期不存在。
- 旧 local Console 在回环 HTTP 写入语言偏好，新 local 读取相同偏好；新
  native Console 在浏览器 HTTPS 下不继承旧共享偏好，并在重启后读取自身
  偏好；回退旧 local 仍可读取它原来的偏好。

完整 [八项源码配对报告](evidence/console-migration/source-pair-darwin-arm64.json)
也已通过，包括 storage、settings、features-version-copy、native、
relationship-iam、real-protocol 和两个真实 Keycloak/Chromium OIDC profile，
由 [配对 runner](../buildscripts/test-console-migration-pair.py) 独立报告。
统计为 360 个功能验收组、Range/diagnostics/OIDC STS 三个真实协议检查、
27 个 OIDC 协议检查及 22 个实际 Chromium 检查。两个 OIDC 路径均清理通过，
验证跨站回调、刷新后可用、固定截止、登出隔离；双入口另验证错误 Admin CA
拒绝。九个原始子报告保留与 manifest 一致的摘要。两套报告使用相同候选
程序字节；配对十项、升级九项 focused 回归通过。Linux amd64 新 CI 尚未运行。

## 首次失败与修正过程

首次准备失败记录在 [manifest 失败报告](evidence/console-migration/upgrade-initial-manifest-failed.json)，
原因是旧正式 manifest 身份/字段未被正确匹配，不是一次通过的运行。
后续原始失败报告保留：

- [存储启动失败](evidence/console-migration/upgrade-storage-startup-failed.json)：
  初始化尚未完成时首次 PUT 得到 503。
- [旧 Console 启动失败](evidence/console-migration/upgrade-old-console-flags-failed.json)：
  旧程序在准备完成前退出，随后按旧版实际支持的 local 参数启动。
- [首次 IAM 读取失败](evidence/console-migration/upgrade-first-iam-read-failed.json)、
  [明确历史对象诊断](evidence/console-migration/upgrade-iam-read-diagnostic-failed.json)
  与 [部分 readiness 失败](evidence/console-migration/upgrade-partial-iam-ready-failed.json)：
  升级/回退后用户历史对象读取得到 403，未作为允许的成功结果。

最终 runner 等待存储及持久 IAM 用户的只读就绪探测后才执行验收；读写、
allow/deny 断言不会自动重试。修正后整条单、双 TLS 路径重新运行通过；
原失败报告及各自脚本摘要不覆盖、不改名为通过。SIGTERM/SIGINT 中断按失败
记录，停止子进程后清理临时存储；SIGKILL 无法运行进程内清理逻辑。
加固前的 [旧通过报告](evidence/console-migration/upgrade-before-cleanup-hardening.json)
也原样保留，当前主报告是在首次信号发生于停止 writer 期间的清理修复后，
重新跑完整两项得到的结果。

## 配方检查与剩余门槛

新增 native/OIDC Compose 配方已用 Docker Compose 解析配置，并核对实际
Console 参数、独立 CA、文件 secret、默认只读和两个持久工作卷。这是配置
检查，没有启动配方容器，也没有认证某个镜像 digest。

上述升级未测试 OIDC 旧版到新版、gateway、代理 TLS 终止、distributed/
federated、多 OC 实例、其他浏览器或所有平台，也不证明任意服务端降级可用。
`releaseAssetCertification: false` 保持，报告不认证 OC/OtterIO image digest；
Keycloak 的镜像身份仅代表 IdP 测试样本。正式附件、
checksum/manifest 和镜像内容需发行时验证，并对用户实际收到的精确组合复验。

已公布的至少两次通知、独立 default-off 过渡和有限回退窗口仍是发行门槛，
合并删除代码和本地通过不能代替履约；若修订政策，需明确公告。现有 stable
Release 流程即使不推广 latest 也会发布正式版本，不是 RC。未执行发布。
