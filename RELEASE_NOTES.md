# Independent-user Console and migration preparation

Changes since [RELEASE.2026-10-10T11-42-18Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-10T11-42-18Z),
source `ee1846d587a6548db4e595d653ca4f54baea2b1a`, through the current source.
This prepares the next UTC timestamp release; no tag, published artifact, image
digest or completed migration window is declared here.

## Changes

- Require Go 1.27.3 across build images and checks; refresh compatible dependencies, OtterIO SDK v7.3.3, OtterIO Kits, and the coordinated OtterIO server source.
- Add a manually triggered immutable-tag preparation workflow that verifies the exact main commit before dispatching the existing draft builder. Publication and packaged-pair acceptance remain separate.

- Add independent native IAM browser login over direct HTTPS. Sessions use each
  enabled IAM user's own credentials and permissions; root, service, STS and
  directory identities are refused. Signed identity revalidation precedes
  authenticated requests. Explicit rejection retires the session; transient
  failure blocks dispatch without destroying it.
- Add OIDC Authorization Code login with S256 PKCE, RS256 verification and
  OtterIO WebIdentity STS. Fixed issuer/client/endpoints and separate IdP/S3/Admin
  trust prevent browser-selected backends. Refresh rotates owned client
  generations without extending the fixed browser lifetime. Cross-site callback,
  logout, unrelated-user isolation and archive/upload ownership fixes accompany
  this mode. Confidential-client secrets use private files.
- Extend native IAM management with relationship-conditioned user, group,
  policy-binding and service-account changes. Reviewed, single-use plans bind
  the exact target, relationship revision and captured affected identities.
  Confirmed or uncertain submissions retire captured local sessions and pending
  logins; definitive refusal preserves them. Creation never replaces an existing
  native target. User lifecycle checks include real service/STS child authority
  and fresh-login boundaries.
- Add authenticated conditional full downloads and single byte ranges with
  GET/HEAD, validators and If-Range handling. Pin reads to the observed
  object/version and verify upstream range replies. Multipart ranges remain
  unsupported; historical versions retain independent permission checks.
- Add permission-scoped Service diagnostics snapshots and bilingual navigation.
  Missing `admin:ServerInfo` causes denial without privileged fallback.
  Unavailable platform/runtime and global-capacity information is not replaced
  with account-visible bucket totals.
- Fix `oc admin policy update` checking the wrong error value. Empty arguments
  and already-assigned policies now fail before binding replacement instead of
  potentially clearing assignments. Valid additions retain existing policies.
- Add direct-HTTPS single-instance native/OIDC deployment templates and exact
  candidate-pair verification. Native acceptance checks complete coupled user
  scope and confirms child deletion by signed direct access-key lookup,
  independently of parent-filtered inventory.
- Separate release operations: the tag build stops at a verified stable draft;
  manual publication requires an official successful packaged-pair acceptance
  and rechecks its unchanged asset/image identities; stable promotion is a
  separate manual action. Neither build nor publication changes `latest`.

## Compatibility, deployment and limits

`oc` and `oc-console` remain packaged together in eleven platform archives and
Linux amd64/arm64 release images; the image entrypoint remains `oc`. Go source
builds require 1.27.2. SDK/Admin dependencies remain independently pinned in
`compatibility.json` and `go.mod`; they do not identify a remote server. Existing
CLI configuration and supported `OC_*` / `MC_*` precedence remain. Older
published artifacts do not acquire these source features automatically.

Choose local alias, native or OIDC mode explicitly. Local mode shares one startup
identity and must not substitute for independent users. Native/OIDC require
browser HTTPS and HTTPS S3/Admin, with fixed endpoints and independent CA trust.
Initial deployment scope is one OC instance; sessions are in memory and restart
requires fresh login. Preferences persist per identity. See the [Console guide](docs/console.md),
[HTTPS recipes](deploy/README.console-https.md) and [candidate verification](docs/console-migration-candidate.md).

Writes and sharing remain explicit opt-ins subject to backend permissions.
Native IAM mutations, protected settings and historical versions require matching
server capability/relationship protocols. Unsupported operations are refused or
retain documented read-only behavior. OIDC IAM mutations and Console secret
rotation remain disabled. Native revalidation occurs at request time; it does
not proactively detect external changes or continuously check operations in
flight. Proxy TLS termination, multi-instance sessions, alternate browser engines,
long-running stability and native execution on every cross-built platform are
not certified by source-level acceptance.

Pin both programs/images and back up configuration before rollout. OtterIO with
embedded Web removed retains S3, Admin and STS, but explicitly setting
`OTTERIO_BROWSER=on` fails startup; it cannot restore the old UI. A verified older
server deployment is needed for that fallback. Returning to an old Console or
server does not undo writes, secret changes or lifecycle transitions; server
downgrades require separate data/configuration compatibility validation. Public
notification, transition and fallback policy remains a release gate. Any revision
to that policy must be announced explicitly, not relabelled as completed.

Build from clean synchronized main after required exact-commit CI and reviewed
CLI/candidate-pair checks. The build verifies thirteen draft assets and pushes
fixed-version images, which may already be public. A stable draft is not an RC.
The [official packaged-pair gate](docs/console-release-acceptance.md) must bind
the actual packages, programs and immutable images and pass its declared runtime
and upgrade checks before manual publication. Local source or packaged reports
are not official publication proof. Publication consumes the successful main
run ID and independently reviewed attestation hash, rechecks the same bytes and
leaves latest unchanged. Only separate manual promotion may move it.

The [English migration announcement](https://github.com/soulteary/otterio/blob/main/docs/releases/console-migration-announcement.en.md)
records the approved accelerated decision: explicit notice plus verified
older-server/snapshot rollback. Its revision is not effective until the first
approved public Web-free release containing that announcement. The October 10
notice remains history; its second-notice/default-off/fallback windows have not
been completed. Review the supported scope and retained-old-program restore
boundary as part of the publication decision. No new tag, deadline, all-platform
runtime certification or passed final artifact run is declared by these notes.
See [release and migration gates](docs/console-release-migration.md) and
[release procedure](docs/releasing.md).

---

The Chinese preparation record below is retained unchanged. Its former
automatic stable publication/promotion description is historical; the current
English procedure above separates draft preparation, verified publication and
manual promotion. No historical migration window is relabelled complete.

# 独立用户 Console 与迁移准备

本次变化以 [RELEASE.2026-10-10T11-42-18Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-10T11-42-18Z)
为基线，源码为 `ee1846d587a6548db4e595d653ca4f54baea2b1a`，覆盖其后至当前
源码的变更。本次准备不声明新标签、已发布附件、镜像摘要或已完成的迁移窗口。

## 本次变化

- 提供直接 HTTPS 的独立 native IAM 登录。每个浏览器使用自己的启用 IAM
  身份，不接受 root、服务账号、STS 或目录身份。请求执行前重新校验签名身份；
  明确拒绝撤销对应会话，临时故障阻止执行但保留会话。
- 提供 OIDC Authorization Code、S256 PKCE、RS256 与 WebIdentity STS。
  固定 issuer/client/后端及独立 CA，刷新替换客户端代次但不延长固定期限。
  修复跨站回调、刷新后的上传/归档归属和登出隔离；client secret 使用私有文件。
- native IAM 用户、组、绑定和服务账号操作采用关系版本条件与一次性确认
  计划，绑定准确目标及受影响身份。结果确认或不确定时撤销捕获范围的
  本机会话及待登录，明确拒绝保留会话。创建不覆盖身份；生命周期验证包括
  真实 service/STS 子身份权限和同名重建不恢复旧会话。
- 提供条件完整下载和单段 Range，支持 GET/HEAD、validators、If-Range，
  固定实际对象/版本并校验上游响应；不支持多段 Range，历史版本单独授权。
- 提供按身份权限读取的服务诊断快照及中英文导航；缺少 `admin:ServerInfo`
  时拒绝，不借用管理员。平台/运行时和全局容量不可用时保持明确缺失。
- 修复 `oc admin policy update` 检查错误变量：空参数或已有绑定在替换前
  拒绝，不再可能清空绑定；合法新增保留现有策略。
- 补充 native/OIDC 单实例 HTTPS 配方与精确候选配对。原生验收检查完整
  用户影响范围，并按子账号 access key 直接签名查询，不能仅用父过滤列表证明删除。

## 升级与兼容性

继续在 11 个平台归档和 Linux amd64/arm64 镜像中分发 `oc` 与 `oc-console`，
默认入口为 `oc`。源码要求 Go 1.27.2，SDK/Admin 固定依赖不是远程服务端身份。
保留现有 CLI 配置与受支持的 `OC_*`／`MC_*` 规则；旧发行包不自动获得新功能。

local 共用一个 alias 身份；独立用户选择 native 或 OIDC。首批为 OC 直接处理
浏览器 TLS 的单实例，使用实际 HTTPS S3/Admin 地址及独立 CA。会话在内存中，
重启后重新登录，偏好按身份持久保存，见[控制台指南](docs/zh_CN/console.md)、
[HTTPS 配方](deploy/README.console-https.md#中文说明)及[候选验收](docs/console-migration-candidate.md)。

写入和分享需显式开启及实际权限；IAM、设置、历史版本需对应能力/关系协议，
缺失时拒绝或只读。OIDC IAM 写入和 Console 改密仍禁用。请求前校验不等于
主动检测外部变化或持续轮询在途操作。代理 TLS 终止、多实例、其他浏览器、
长期稳定性及全部交叉编译平台的原生运行未由开发验收认证。

删除版 OtterIO 保留 S3/Admin/STS，但 `OTTERIO_BROWSER=on` 拒绝启动，不能
恢复旧 UI；回退旧服务端需验证数据和配置兼容性。回退不撤销写入、改密和
生命周期转换。通知、过渡与回退政策仍是发行门槛，修订时必须明确公告。

正式发布需从干净同步的 main 开始，在同一提交完成要求的 CI 和候选配对，
再核验 13 个附件、程序校验和、镜像身份及承诺的升级路径。开发报告不等于
正式产物认证。关闭 latest promotion 仍创建 stable，不会创建 RC，见
[迁移门槛](docs/console-release-migration.md)和[发行流程](docs/zh_CN/releasing.md)。
