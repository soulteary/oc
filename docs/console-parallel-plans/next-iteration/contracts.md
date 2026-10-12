# 共享契约与文件窗口

## 必须保持的边界

1. `internal/oidc` 负责已存在的授权码/PKCE/nonce、ID token 验证、STS 交换和 Grant 刷新。Console 负责 cookie、精确 callback、会话、客户端和任务生命周期。不得为 OIDC 发现恢复旧 Web JSON-RPC。
2. OIDC 主体使用经过验证的 issuer/sub，并包含固定存储目标的命名空间；native 使用权威原生身份。subject/email 不等于 IAM access key，未知关系不是已知空集。偏好、客户端、任务、确认 token 和撤销均按主体/会话隔离。
3. 每个会话只使用该用户的 S3/Admin/STS 凭据。凭据仅留进程内；浏览器不接收已有 secret、session token 或 refresh token。
4. 刷新候选必须验证同一主体和固定目标；替换与退休通过 generation/epoch 屏障原子提交。旧客户端保留在途租约，退出/撤销/到期后的迟到结果不得恢复会话。cleanup 恰好一次；关闭不持锁等待网络或清理。
5. 临时身份到期是 ID token、STS 和固定会话期限中最早者。刷新不能延长固定会话期限；旧凭据在途操作也不能越过其到期。刷新失败结束该会话并清理所属任务，不影响其他用户。
6. OC 登出终止本实例会话，不宣称自动注销 IdP 或撤销所有 STS 凭据。策略与对象权限仍由存储服务验证。无权威父映射时禁用受影响的 IAM 修改。
7. IAM/config 写入必须遵守服务端 revision/原子条件协议。客户端先读后写不能冒充原子保护。明确拒绝保留会话；确认成功或结果未知按实际影响集合退休，不自动重放写入。

## 接口冻结要求

00 与 01/02 在编辑核心前记录实际类型/函数签名、所有权和调用顺序，至少覆盖：主体键、关系状态（已知/未知/过期）、影响集合、client generation、租约获取/释放、候选提交、退休/取消和审计事件。以现有实现扩展，不预先承诺未经验证的接口。

契约变更提交给 00，说明消费者、兼容影响、需迁移的测试和合并顺序。未获得确认时可在专属模块做实验，不开放生产入口。

## 文件负责人

- `internal/console/server.go`、`session_clients.go`、`cmd/oc-console/*` 现有文件：平时 00；认证阶段以显式窗口转交 01，02/03/05 只交接入补丁。
- `identity_revocations.go`、身份影响/索引专属模块：02；涉及现有 native 退休/改密文件须申请窗口。
- `internal/oidc/*`：01 使用既有模块；确需协议修改由 00 确认范围和消费者后授予。
- `internal/console/iam.go`、`internal/storageclient/iam.go`、`internal/consoleapi` 现有共享文件：00。03 子任务分别拥有新增的 `iam_users_*`、`iam_groups_*`、`iam_service_accounts_*` 文件，命名前登记。
- `web/app.js`、`web/index.html`、`web/i18n.js`、`web/style.css`：00；可将明确窗口转交单个 UI 任务，其他 agent 提供补丁和专属测试。
- 现有 `.github/workflows/go.yml`：04；发布工作流/Compose/版本 pin：05，交叉修改必须交接。
- OtterIO IAM/关系协议及专属测试：02；Web 删除及构建清理：06。两者若涉及同文件，由 00 排序，不同时编辑。
- registry.json、contracts.md 和本目录总状态：00；任务 agent 通过交接消息请求更新，避免状态文件并发覆盖。

窗口登记包含文件、owner、开始基线、状态和归还提交。所有 owner 初始为空；任务编号表示规划归属，不代表窗口已经授予。00 也不得修改已经转交的文件。
