# 首批并行开发契约

冻结日期 2026-10-11，首批基线为 OC main `0a162c82`，包含 PR #33 审查修正。冻结时 PR #32 为独立未合并依赖；本批新包不复制其删除实现。执行期间 #32 已于 2026-10-11T10:18:26Z 合并，后续身份基础以 `e57c0fcf` 为基线。

## 当前文件拥有者

OIDC 协议 agent 拥有新增 `internal/oidc` 及专属 STS 适配、测试、验收与任务文档。Range agent 拥有新增 `internal/consoleapi/range.go`、Range 后端与 handler/helper、专属测试。诊断 agent 拥有新增 `internal/consoleapi/diagnostics.go`、白名单快照后端及独立 handler/helper、专属测试。协调者拥有核心路由的接入、主 UI、总文档、共享验收入口和发行基线。

该委派仅涵盖指定新增公共文件，不能修改其他公共类型或旧核心。现有 CLI、默认本机模式、原生默认只读、请求前身份验证和 PR #33 的协议拒绝规则保持原语义。

## OIDC 协议边界

客户端固定 issuer、client ID、callback 和 STS 目标；`Begin` 绑定随机浏览器标识并返回一次 state/nonce 和 PKCE 授权请求，`Complete` 只消费匹配事务。OC 对用于建立身份的 JWT 严格核验 issuer、受众、签名、时间和 nonce，不把上游 STS 缺少的校验视为已完成。

可信 OIDC 身份键为已核验 issuer/subject；它不是原生 IAM access key、父用户或邮箱。服务端未返回权威父身份时保留未知，不自行推导。临时凭据仅供进程内客户端，不向浏览器 JSON 导出。期限受 ID token、STS 凭据和配置上限共同限制；不支持刷新时必须重新登录。并发刷新、轮换与取消语义须清楚；不宣称 Go 字符串赋空即可安全擦除内存。

生产配置不跳过 HTTPS、TLS 或 token 校验；测试注入与生产配置严格分离。协议客户端交付不代表 HTTP 登录/UI 已接入。

## Range 边界

新增可选接口采用闭合字节区间并带可信选中对象元数据；返回 `Object.Size` 是范围长度。后端验证 `Content-Range` 起止/全长和响应长度，HTTP 层正确返回 206/416/Content-Range，不能把完整对象误标为部分响应。

普通无条件 GET 保留已有路径；Range、条件和 HEAD 才进入新 helper，沿用现有客户端租约、下载槽和取消。HEAD/条件遵循协议而非机械复用 206；历史/null version 必须保持能力与权限校验。多 Range 首批可以明确拒绝。不全文件缓冲，不降低原传输/取消预算。

## 诊断范围修正

旧 Web 实际浏览器诊断入口是 ServerInfo/StorageInfo 快照，没有日志、trace、profile 或 heal 订阅；这些 CLI 能力不列作旧 Web 移除的必需浏览器迁移项。

首批仅提供当前主体权限下的白名单服务诊断快照与准确差异记录。原 Web 的全局容量不等同于 AccountInfo 的主体可见桶容量；Admin ServerInfo 权限比原 Web 严，且字段不完全对应。管理员快照接入不能冒充普通用户已获得等价信息。需要普通用户最小可信发现 API 时另提服务端契约，不借启动管理员 fallback。完整 SDK ServerInfo、环境、配置、凭据、他人桶信息不得直接向浏览器输出。

## 实施与验收

每个包先独立验证和签名提交；协调者审查接入补丁后发布独立 PR。必须报告未接入与真实服务器能力缺口。正式版本基线独立记录与补丁矩阵的差异，缺失能力保持拒绝。默认关闭/移除等最终动作仍受公告及回退门槛约束。
