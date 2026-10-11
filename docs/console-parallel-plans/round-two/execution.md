# 第二轮第一批执行记录

基线为 OC main `7d4fb7fb922a9599f8d9805651693c7c20d0a2f1`。三个开发任务在独立 worktree 进行，协调者保留第四个并发名额。

## 已认领范围

- 01 真实协议 CI：复用现有服务构建，接入 Range/诊断/STS wrapper、严格实际执行检查、脱敏失败报告和清理。
- 02 身份基础：统一原生修改影响范围与撤销执行器，保留登录屏障、写入静止和响应后退休；权威父/组关系协议不足时保持拒绝，不声称已完成索引。
- 05 诊断 UI：已有管理快照的导航/节点展示、权限/协议错误提示、退出取消与清理。普通用户旧平台/runtime/全局容量仍是后续缺口。

实际独占窗口见 [00 集成计划](00-integration.md)。本批不修改现有认证默认值、不实现完整 OIDC 控制台、不关闭或删除旧 Web。

## 验收与交付

第一批实现已交付，四个 PR 独立审查：

- [#39 第二轮计划与执行记录](https://github.com/soulteary/oc/pull/39)。
- [#40 原生身份修改执行器](https://github.com/soulteary/oc/pull/40)，提交 `084432b8`、`ef3fefe1`；包含影响范围复制与回归。
- [#41 诊断页面](https://github.com/soulteary/oc/pull/41)，提交 `b8c9bc86`；包含权限错误、页面切换取消和会话清理。
- [#42 真实协议 CI](https://github.com/soulteary/oc/pull/42)，提交 `51757f0e`；复用固定服务器构建，拒绝 skip 冒充执行成功。

组合版本的 console、storageclient、consoleapi、oidc race 回归通过，lint 为 0 issues，页面测试 86/86，wrapper 单元测试 5/5。组合 CLI/console 配合未修改的固定 module 服务 `bcc238bc4c2cc3a21aae05b23321bfdb1b4aad05`，真实 Range 三种拓扑、诊断权限和 OIDC STS 均通过，见 [脱敏报告](real-protocol-results.json)。报告分别记录调用者声明的源码和实际二进制摘要；它是源码组合验收，不是正式发行版认证。诊断浏览器预览使用合成快照，不代替真实服务器浏览器验收。

此前一次 Range 夹具在约 0.96 秒失败，诊断和 STS 仍继续执行；见 [原失败报告](real-protocol-observed-failure.json)。后续独立夹具和最终组合验收通过，但该失败尚未复现、尚未定位。补充了 profile/phase/异常类别定位和强制失败回归，CI 不自动重试；不能宣称已消除偶发失败。

远端 PR 检查仍须以各 PR 当前状态为准。本批只完成 01 和 02/05 的上述子范围；权威父/组关系、IAM 完整管理、OIDC 控制台、普通用户 discovery、部署审计和正式发行矩阵继续按后续计划推进。旧 Web 弃用与回退窗口尚未完成，保留入口。

## 客户端组合源码追溯

真实协议报告的两份客户端二进制均内嵌 `vcs.revision=9df4ea4ec766b126d1f983a7406d114741b1e26e`、`vcs.modified=false`，对应 tree `d701f85cf7ccb7155208a9c4bc158c3a7825492f`。该组合提交保存于 `test/console-round-two-baseline` 分支，可 fetch 后 checkout 精确重建。报告的 checkoutSHA/checkoutTree 则记录执行最终 harness 的干净组合工作树（在上述客户端源码上加入 CI 提交）。两者分开记录，避免把 harness 提交误称客户端构建来源。该历史基线不包含之后的审查修正，07 必须对最终集成提交重新构建并验收。
