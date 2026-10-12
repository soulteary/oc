# 01 OIDC Console 会话接入

关键路径；依赖 00/02 冻结生命周期契约。复用 `internal/oidc`，不重做协议客户端。详细历史参考 [第二轮 OIDC 任务](../round-two/04-oidc-console-sessions.md)。

## 小步交付

1. 固定 issuer/client/callback/STS/TLS 配置、启动校验和独立认证模式；保持 local/native 默认行为。第三方 IdP 的算法、同源发现与 HTTPS 限制先调查；扩大协议支持须独立评审。
2. 授权跳转及精确 GET callback：随机 HttpOnly binding cookie、Secure/SameSite、state/PKCE/nonce、Host/Origin/CSRF、重复参数拒绝、error/重放/过期负例。未具备安全会话生命周期的阶段不开放可用登录入口。
3. 构建独立 STS 客户端；以 issuer/sub 和存储目标隔离身份/偏好/任务。会话和操作在硬到期取消，退出清理。
4. 刷新及客户端替换：并发请求合并、租约、generation/撤销屏障、失败关闭、旧客户端到期和 cleanup。刷新不能扩展固定会话期限。
5. 登录/刷新/退出 UI、脱敏错误和操作提示；共享 UI 在窗口内接入或交 00。说明 IdP 登出、OC 登出和 STS 到期差异。

## 验收

两名真实 IdP 用户分别具有不同桶/前缀权限；验证客户端、偏好、任务和退出隔离。覆盖回调错 Host/Origin、缺/错 binding、重复参数、重放、nonce/issuer/audience 错误、刷新主体变化、并发刷新与退出/到期/关闭竞争、迟到结果和在途租约。登录码/native 凭据不能登录 OIDC 入口。

真实 IdP、单/双 HTTPS 存储、独立 CA、token/STS 硬到期、权限 deny 和取消清理均需记录证据。fixture 协议测试与真实 IdP 测试分别标记。没有可用 IdP 时交付可复现夹具与明确待验项，不宣称端到端完成。

专属新增 OIDC handler/refresh/测试归 01；核心与 UI 文件须登记窗口。交付包含旧模式回归、race、临时程序/来源摘要、未支持 IdP profile 和最低服务端能力。
