# Console 迁移的 28 项能力范围

此记录逐项对应原 [capability matrix](console-capability-matrix.json) 的 28 个
ID，说明当前替代方式、主动撤销的旧行为和未覆盖范围。它不修改原 matrix 的
`status`、`requiredChecks`、`testIDs` 或历史证据，也不创建新的 `parity-*`
CheckID。当前接口支持、一次候选程序实测和正式发行门槛是不同结论。

准确源码、程序、报告与后续发行要求见 [候选指南](console-migration-candidate.md)，
实际执行结果见 [验收记录](console-migration-verification.md)。每项所列 profile
来自 [源码配对 runner](../buildscripts/test-console-migration-pair.py)；
[Darwin arm64 manifest](evidence/console-migration/source-pair-darwin-arm64.json)
八项通过，完整范围需读取原始报告，不能将八项通过改称旧 matrix 28 项全 verified。

八个 profile 为 `storage`、`settings`、`features-version-copy`、`native`、
`relationship-iam`、`real-protocol`、`oidc-browser-single`、`oidc-browser-dual`。
前三项在单、双 HTTP 和单、双 HTTPS 存储入口运行；native/relationship IAM
只在单、双 HTTPS 运行。协议和浏览器检查保留各自报告的实际拓扑。
[升级报告](evidence/console-migration/upgrade-darwin-arm64.json) 是单独证据，
没有并入八项源码配对结果。所有支持均受操作人的权限、显式写入/分享开关及
真实后端能力约束；local profile 使用所选 alias，不能冒充独立用户验证。

1. **`legacy-ServerInfo` — 主动撤销旧敏感信息暴露；支持有限替代。**
   替代为 `/api/session` 与 `/api/diagnostics/server`。不向每个已登录用户返回
   主机、平台、runtime 或全局信息；诊断快照使用当前身份并要求
   `admin:ServerInfo`，拒绝后不借用管理员。入口：`real-protocol` 的 diagnostics；
   原敏感信息暴露行为不作为兼容目标，也不因此关闭历史决策门槛。

2. **`legacy-StorageInfo` — 主动撤销全局容量暴露；支持有限替代。**
   替代为权限限定的 `/api/diagnostics/server` 节点快照和当前账户可见桶信息。
   账户可见信息不是整个集群容量，未恢复普通用户取得全局总量的旧接口。
   入口：`real-protocol` 的 diagnostics；全局容量 parity 未认证。

3. **`legacy-MakeBucket` — 当前支持。**
   `/api/buckets/create` 默认私有，需要显式写入和后端创建权限，重复目标冲突。
   入口：`features-version-copy`、`native`；native 另验证授权用户创建及其他用户拒绝。

4. **`legacy-DeleteBucket` — 当前支持。**
   `/api/buckets/delete` 要求准确桶名确认，非空桶拒绝删除。
   入口：`features-version-copy`、`native`；不将删除桶扩展成删除其所有内容。

5. **`legacy-ListBuckets` — 当前支持。**
   `/api/buckets` 通过所选身份的 S3 权限列桶；不能列桶的用户仍可使用已授权
   的精确对象入口。入口：`storage`、`settings`、`native`。

6. **`legacy-ListObjects` — 当前支持。**
   `/api/objects` 保留字面前缀和 continuation token，不按文件系统规范化键。
   入口：`storage` 的真实分页、前缀与权限测试；gateway 路径未由本次认证。

7. **`legacy-RemoveObject` — 当前支持，采用计划和任务接口。**
   使用 `/api/deletions/plan`、计划执行及 `/api/jobs`，保留准确确认、单次令牌、
   有界前缀快照、部分失败与取消。入口：`storage`。当前键删除及其 delete marker
   与历史版本的永久删除不是同一操作，后者不在这里宣称支持。

8. **`legacy-Login` — 当前支持独立 native 用户。**
   `/api/login` 验证已启用 IAM 用户自己的签名身份；拒绝 root、service、STS、
   directory 和错误凭据，不借用启动 alias。入口：`native`；local 登录码继续
   只代表原单身份模式，OIDC 是下面的独立入口。

9. **`legacy-SetAuth` — 当前支持受限替代。**
   `/api/self-secret` 仅允许满足后端能力和权限的 IAM 自身改密。native 改密
   撤销被捕获的用户范围，local 改密保留进程退出及人工更新 alias 的规则；
   OIDC 不支持此写入。入口：`settings`、`native`、`relationship-iam`。

10. **`legacy-CreateURLToken` — 主动替换旧 token 格式。**
    `/api/shares` 使用显式开启、有效期有界的 S3 presigned GET；不接受或生成
    旧 Web URL token，也不恢复旧下载路由。入口：`features-version-copy`；
    旧 token 跨版本延续未支持。

11. **`legacy-Upload` — 当前支持。**
    `/api/uploads` 与任务接口提供普通/分片上传、显式覆盖、条件创建、取消和
    自有分片清理。入口：`storage`；`native` 另验证独立用户写入及任务隔离。
    不具备所需条件写协议的后端拒绝默认创建，不用 HEAD 代替原子条件。

12. **`legacy-Download` — 当前支持受权限约束的下载。**
    `/api/download` 提供完整 GET、HEAD、条件请求和单 byte range，并按精确
    当前/历史引用校验权限。入口：`storage`、`features-version-copy`、
    `real-protocol` 的真实 Range；不认证 multipart ranges。

13. **`legacy-DownloadZip` — 当前支持任务替代。**
    `/api/archives` 生成完整、受限、会话所有的 ZIP；核验字节与引用清单，
    拒绝不安全路径，权限混合失败不提供部分压缩包。入口：
    `features-version-copy` 的取消、权限与任务所有权检查；临时文件不是备份。

14. **`legacy-GetBucketPolicy` — 当前支持。**
    `/api/bucket-settings` 按明确桶读取完整 policy，权限由实际 S3 身份决定。
    入口：`settings`；没有列桶权限不是读取已授权明确桶的前提。

15. **`legacy-ListAllBucketPolicies` — 主动撤销旧敏感汇总接口。**
    不恢复旧前缀策略汇总；替代为 `/api/bucket-settings` 对指定桶、当前权限
    下的完整 policy 读取/编辑。入口：`settings`。这是明确收窄，不能写成
    旧汇总的无差异 parity，也不改原 `pending-decision` 状态。

16. **`legacy-SetBucketPolicy` — 当前支持条件修改。**
    `/api/bucket-settings` 用准确 revision、审阅确认和服务端 CAS 替换/删除。
    冲突需重读，缺少能力拒绝不受保护的写入。入口：`settings`。

17. **`legacy-PresignedGet` — 当前支持。**
    `/api/shares` 使用独立分享开关、明确公开 HTTPS 地址、准确当前/历史引用
    和有效期；历史分享仍要求历史读取权限。入口：`features-version-copy`。

18. **`legacy-GetDiscoveryDoc` — 当前支持固定 issuer 替代。**
    `internal/oidc` 从配置的 HTTPS issuer 取得 discovery，不恢复匿名旧 RPC。
    入口：`oidc-browser-single`、`oidc-browser-dual` 的真实 IdP 流程；
    旧 RPC URL/返回格式不是兼容目标。

19. **`legacy-LoginSTS` — 当前支持 OIDC 浏览器身份替代。**
    `/api/oidc/login` 与 `/api/oidc/callback` 完成 code/PKCE、用户绑定和
    WebIdentity STS；不让用户凭共享管理员进入浏览器。入口：`real-protocol`
    的真实 STS 与两个 `oidc-browser-*` profile。不声称旧 STS 登录协议原样延续。

20. **`object-special-keys` — 当前支持，拓扑范围有限。**
    字面 Unicode、斜杠、空对象和分页由读取/上传/ZIP 路径保留。
    入口：`storage`、`features-version-copy`；拒绝和取消按各 profile 的实际
    操作覆盖，不把这些结果扩展为 gateway 全量验证。

21. **`version-markers` — 当前支持读取、分享、复制及当前键删除。**
    区分 null、历史 version、delete marker，历史读取与复制采用对应源权限。
    入口：`storage`、`features-version-copy`、`real-protocol`；升级报告另保留
    两个真实历史版本。历史版本永久删除和任意迁移结构未由本次认证。

22. **`iam-lifecycle` — 当前支持 native 条件生命周期。**
    `/api/iam/*` 提供用户/组/service 创建和受关系快照约束的修改、绑定、
    状态/删除与捕获范围退休。入口：`native`、`relationship-iam`；
    `features-version-copy` 另覆盖实际权限和创建行为。OIDC IAM 写入、所有
    外部身份后端和多实例退休不在支持承诺中。

23. **`oidc-lifecycle` — 当前支持，按实际浏览器 profile 验证。**
    两个 `oidc-browser-*` profile 使用真实 Keycloak/Chromium、跨站顶层
    callback、用户绑定、刷新后可用、固定截止和 logout 隔离；
    `real-protocol` 验证 STS 协议。竞态细节另由源码单测覆盖，不能全部计作
    本次真实浏览器路径；OIDC 旧版到新版升级没有测试。

24. **`session-isolation` — 当前支持单实例隔离。**
    native/OIDC 使用独立存储身份；任务、取消、偏好与登出按身份/会话约束。
    入口：`native`、`relationship-iam`、两个 `oidc-browser-*`，加 `storage`/
    `features-version-copy` 的任务所有权。升级报告只对 native 验证旧 local
    偏好不会泄露、重启持久；不推广为 OIDC 跨版本或多实例会话证明。

25. **`diagnostic-stream` — 只支持权限限定快照，长流未覆盖。**
    `/api/diagnostics/server` 提供手动 ServerInfo 快照。入口：`real-protocol`
    的 diagnostics；此次没有认证长时间日志/trace 流、持续资源上界或其取消。
    不将页面退出时取消一个快照请求称为长流验收。

26. **`browser-off-core` — 核心服务保留，实测子集明确。**
    删除版不含旧 Web，省略/关闭 browser 不影响已验证的 S3/Admin/STS；旧
    assets/RPC/upload/download/ZIP 路由拒绝。入口：各 integration profile、
    `real-protocol`、升级报告的 omitted/off 与 readiness。health/metrics/
    节点通信是保留的服务端能力；本次没有全量认证 metrics 和节点通信路径。

27. **`deployment-topologies` — 支持首批声明范围；其他拓扑未覆盖。**
    首批为单 OC 实例直接 HTTPS、单节点四存储目录纠删码、单 S3/Admin HTTPS 或
    独立双 HTTPS/CA。八项 profile 与升级报告保留精确入口范围；新 Compose
    只经过配置解析，未据此认证容器执行。gateway、代理 TLS 终止、distributed/
    federated、多副本及所有原生平台仍需另验。

28. **`release-migration` — 候选实测有记录；正式门槛仍未完成。**
    八项源码配对和实际旧正式程序到候选程序的升级/回退是分开的入口，不能
    冒充正式附件或镜像验收。入口：[候选指南](console-migration-candidate.md)、
    [升级报告](evidence/console-migration/upgrade-darwin-arm64.json) 和
    [发行门槛](console-release-migration.md)。新 PR head 的 CI、正式 OC/OtterIO
    程序/镜像配对和公开通知、default-off 过渡及回退窗口仍需完成；公开政策
    可经明确公告修订，不能悄悄撤销或将历史窗口标成通过。
