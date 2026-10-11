# 身份撤销基础：首批作用域 ledger

基线 OC main `e57c0fcf`，已包含 PR #32 原生用户删除。该基础 PR 抽取现有逻辑，不增加 IAM 管理动作或新的登录方式。

## 实际接口

`identityRevocations` 由 Server 持有，**所有方法调用和字段读取都必须持有 Server.mu**，不会自行嵌套锁。

- `begin()`：在已校验登录输入之后、创建/验证客户端之前登记 pending，返回当时 epoch。
- `finish()`：配对一次 begin，所有成功/失败/取消路径都完成；最后一个 pending 结束时清空墓碑。
- `revoke(scope)`：仅接受已知且有效的身份作用域，推进 epoch；只有存在 pending 时记录该 scope 的墓碑。未知/无效 scope 返回拒绝，不改变状态。
- `changedSince(scope, epoch)`：提交时检查对应 scope 自验证开始后是否撤销；无效 scope 保守判为变化。

作用域仅有两个当前实际使用的类型：固定存储连接的原生 principal hash、已验证原生 IAM user。相同字符串在不同类型下完全隔离。它们来自现有已验证 native client/session 的语义；浏览器不能指定作用域类型。

OIDC 的已验证 issuer/subject 必须是另一个独立命名空间，**不等于 IAM access key、邮箱或 IAM 父身份**。本批没有 OIDC scope、父身份猜测、目录身份映射或泛化字符串入口。未来接入需要协调者确认权威证据和匹配范围，不能把未知关系当作空集。

## 保留的行为

原生改密继续按 principal 撤销；用户停用/删除继续按 exact IAM user 撤销。目标请求/登录门控、占满写入槽确保 multipart 清理完成、确认/结果未知退休、明确拒绝保留、自身响应后退休、其他用户继续使用、现有客户端请求租约均保持原机制。

ledger 只防止已验证但尚未提交的旧登录发布。活动会话仍由现有 removeSessionLocked 退休；清空墓碑不恢复被删会话，也不允许旧 cookie 因账户重建恢复。重复撤销覆盖旧 epoch，单作用域只留一个墓碑，pending 清零时回收。

本批未完成身份索引、父身份影响范围执行器、临时凭据替换/刷新调度、主动外部撤销检测。请求前 native 验证仍是按请求检测，并非即时全局撤销。02/04 接入须独立实现并验收这些剩余契约。

## 验证边界

确定性单测覆盖不同作用域隔离、重复 revoke、前/中/后 epoch 登录、多个 pending 未结束时保留墓碑、最后 pending 回收、idle revoke 无墓碑、unknown 拒绝；保留现有原生登录/改密/停用/删除/客户端租约回归。该变更不修改存储协议，不需要用真实服务器重复已有协议验收；实际运行结果在 PR 中记录，不把基础 refactor 解释为完整迁移验收。
