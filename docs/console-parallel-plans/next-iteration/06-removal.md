# 06 OtterIO 默认关闭与 Web 删除

现在可调查引用、准备删除清单和测试。**执行删除依赖 OIDC 等价、真实发行配对及迁移窗口完成**，不能把用户要求“补齐后删除”解释成允许现在删除。参考 [OtterIO 弃用公告](https://github.com/soulteary/otterio/blob/main/docs/web-console-deprecation.md)；执行时核对实际服务端提交中的该文件。

## 两个独立交付

1. 05 确认可默认关闭，且至少两个实际通知发行周期和活跃迁移问题满足约定后，交付默认 off 改动；明确 opt-in 回退结束发行/日期，验证 on/off 同版本切换。
2. 回退窗口结束且 05 确认可删除后，交付物理删除；重新跑核心协议、构建/发行和迁移验收。删除后 UI 回退需要受支持旧版本，数据/配置兼容必须提前验证。

当前已有记录只证明第一个通知周期；执行前核对真实发行事件，不能用 PR 合并或快速连续发版冒充迁移时间。窗口若需改变，由 00 提出具体证据和方案，不自行改约定。

## 删除与保留边界

删除候选：`browser/` 和前端工具链、`fiber_web_router.go`、旧 Web handlers/context/router 和专属测试、Web RPC/upload/download/ZIP、专用登录/URL token、浏览器重定向/缓存分支、专属 trace/RPC 依赖、前端 CI/容器/发行资源和旧文档。先追踪引用，再确认哪些共享 helper 可以删除。

必须保留：S3/Admin/STS、health/metrics、节点通信、共享 JWT 认证、Admin 日志/trace、终端输出。`pkg/console` 是终端能力；`webRequestAuthenticate` 也被 metrics 使用，不能按名字整块删除。`--console-address`/`--console-certs-dir` 同时控制 Admin 监听器，保留兼容配置并准确更新说明。server 和 gateway 两条路由均清理。

## 验收

旧 Web 路径无处理器/资源，相关前端依赖/CI/资产无残留；核心认证、S3、Admin、STS、health/metrics、节点通信、CLI 和日志正常；单双入口、TLS/独立 CA、server/gateway 及错误请求回归通过。构建和 SBOM 变化记录实际结果，不把代码行数等同漏洞减少。

OtterIO 独立 worktree/PR，不与 02 的服务端协议共享编辑窗口。最终交付删除范围、保留理由、真实回归、准确升级/回退路径、发行组合及未完成项。
