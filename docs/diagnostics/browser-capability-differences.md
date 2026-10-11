# 旧浏览器诊断差异与第一批后端

调查基线：OtterIO checkout `8cc518b31ac8b69c8f855e19b4bd027f44275c47`，OC `0a162c82`。本切片不是完整替代验收，也不授权移除任何旧功能。

## 实际旧功能

`cmd/web-handlers.go` 的 `ServerInfo`、`StorageInfo` 和 `browser/app/js/web.js`、`browser/actions.js` 是实际浏览器入口。

- ServerInfo：版本、hostname/OS/arch、Go runtime/CPU、globalInfo（包括公共域）、IAM/temp 用户标志。旧实现对任意已登录主体开放这些字段。
- StorageInfo：集群 `ObjectsTotalSize`，并非当前主体可访问桶容量。旧实现对任意已登录主体开放。
- 旧浏览器未发现 log/trace/profile/heal 订阅或管理入口。服务端向 globalTrace 发布 Web 请求，是被管理客户端观察的行为，不是浏览器订阅功能。CLI 管理能力不能自动计为旧 Web 差异，也没有被废弃。

## OC 当前覆盖与明确缺口

`/api/account` 已提供当前身份可读或可写桶及其容量；测试 `TestAccountMapsOnlyVisibleBucketFields` 阻止隐藏桶/策略泄露。该容量不能声称等价于旧集群已用容量。

账户种类已由 self-account/native identity 路径处理；精确旧 temp/runtime/globalInfo/public-domain 展示仍需逐项接入与验收，不能通过浏览器推测身份或借启动管理员读取。

本切片新增可选 `DiagnosticsBackend.ServerDiagnostics`，只读取当前客户端签名的 `/otterio/admin/v3/info`，上游要求 `admin:ServerInfo`。白名单仅包含节点状态、版本、运行秒数、heap 分配字节、驱动数量。不暴露 SDK 完整响应、endpoint/path、deployment ID、env、服务配置、其他用户桶或凭据；节点数量最多 256，响应最多 1 MiB，请求最长 15 秒，无自动重试或重定向。

这是受权限约束的最小管理快照，不是旧 ServerInfo/StorageInfo 的全部等价实现。普通用户收到明确拒绝，不能管理员 fallback。旧普通用户平台/runtime/集群容量语义需要服务端可信、按当前身份授权的接口，或用户明确批准并公告的范围变更；本 PR 未做后者。

## 接入要求

`/api/diagnostics/server` 已在 `Server.ServeHTTP` 的正常 native revalidation 和 runtime retain/release 之后注册，路由调用 `s.serveDiagnostics(w,r)`。实际登录路由测试确保未登录拒绝、每次请求校验当前身份、被撤销身份不能到达诊断后端；退出/到期/关闭测试通过完整路由执行。不得注册成绕过标准会话链的裸 handler。

handler 仅 GET、无 query，使用现有 apiSlots 并发上限与 sess.ctx 取消，响应 no-store。退出、到期、关闭由 session cancel 结束 HTTP 读取；请求取消也结束读取并关闭响应体。它是一次快照，无长订阅、持久 cursor、重连或事件回放语义，也无需引入慢消费者缓冲。

UI 应显示“服务器诊断（需管理员权限）”，不得把本机 runtime 或 OC 版本当上游服务器值。501 表示接口未实现，403 表示当前身份无权限，502 表示协议/响应失败。主 UI 和共享 capability 响应由协调者拥有，尚未接入。

## 验证

本包测试包括 signed request、白名单、权限拒绝、redirect/超限/非法 JSON、取消后上游连接结束，以及现有账户容量隔离测试。真实服务验收单独记录在交付消息，不以 mock 通过冒充正式发行组合认证。

实际验收：本切片已对独立临时数据服务器 `6f6d0835ddff68020f1491c403b958fade22841f` 原始构建执行管理员快照读取和临时 readwrite IAM 用户 403 负例；账户创建/绑定/清理由测试管理员完成，待测普通用户的读取使用其自身凭据。该证据仅覆盖单 HTTP 隔离服务，不代表 TLS、分离 admin listener 或正式发行矩阵认证。独立重跑：`python3 buildscripts/test-console-diagnostics.py --server /path/to/otterio`。

正式服务器复核：对 `/private/tmp/oc-parallel-release-server/otterio-darwin-arm64`（base revision `8cc518b31ac8b69c8f855e19b4bd027f44275c47`，资产 SHA256 `48b673ad0e03723eeeedb2ab5773569093b426d8fa04e132d17183ac7afb6f9c`，构建标记 `vcs.modified=true`）运行同一隔离脚本，管理员快照成功、临时 readwrite IAM 用户诊断 403，清理完成。仍仅是单 HTTP 诊断后端证据，不是完整控制台或 TLS 发行组合认证。

审查修复：HTTP 200 必须包含非空 servers，每节点必须是对象，有合法 online/offline/initializing state、mem_stats 和 HeapAlloc；online/initializing 必须有非空 version。null、空对象、空数组、null 节点、缺必填字段均返回统一 502。根据 SDK 实际 JSON 的 omitempty，零 uptime/空 drives 可以省略，offline 的 version 可以省略；HeapAlloc=0 合法且仍必填。显式 null 数值/驱动列表及负数等非法形状不作合法零值解释。

上述发布资产的 modified 标记表示 base revision **不能代表二进制全部确切源码**。证据绑定实际资产 SHA，而不是声称它等同于 clean `8cc518b...` 源码构建。若另以干净源码 checkout 构建服务，应分别记录源码 revision、clean/modified 状态和该构建的 SHA，不能混用本资产证据。
