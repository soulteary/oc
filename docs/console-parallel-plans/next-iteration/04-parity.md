# 04 功能等价清单与回归

可立即建立夹具与旧能力清单，完整 OIDC 验收依赖 01。

逐项枚举旧 `webAPIHandlers`、静态页面及上传/下载/ZIP 路由，不只检查页面按钮。至少覆盖对象浏览/分页、特殊 key、上传/multipart、删除/部分失败、版本/标记、Range/条件下载、ZIP/共享、桶设置、账户/IAM、OIDC、普通用户发现信息、诊断流和取消。

每项登记旧行为、OC 对应实现/协议、权限差异、能力限制、支持拓扑、测试 ID 和结论。结论只能为已验证替代、明确保留 CLI、待实现或待决定。未验证列为空或待验，不能按现有历史报告自动打勾。减少功能支持须交 00 请求用户决定；OIDC 已确认必须保留。

独立验收夹具使用一次性桶/身份/配置，避免 settings 开启版本污染 features 的 null-version 前置条件。调查已有 Range 失败记录，不自动重试掩盖失败。真实协议 wrapper 中 required profile 被 skip 必须失败。

在 OTTERIO_BROWSER=off 下验证旧页面/RPC/upload/download/ZIP 不可用，同时按正常身份校验 S3/Admin/STS/health/metrics；覆盖 server/gateway 和承诺的单双入口/TLS/独立 CA。同一用户与不同用户的任务、偏好、取消、退出和权限均验证。

独占新增等价/浏览器/协议测试模块和 go.yml（登记后）；现有 UI 与业务代码修复交相应负责人。最终报告记录执行环境、源码/modified、程序和镜像摘要、harness SHA、通过/失败/skip、清理结果。与 05 的平台/部署报告互相引用，不重复宣称同一范围。
