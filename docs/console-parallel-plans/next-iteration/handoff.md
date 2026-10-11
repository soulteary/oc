# 本轮开发交接与后续并行迭代

本轮范围是先补齐 OC 的 OIDC 和安全 IAM 替代，再执行迁移门禁。代码与证据位于独立 worktree，原 checkout 保留；未推送、未发布、未改生产部署、未默认关闭或删除 OtterIO 旧 Web。

## 可审查的候选

- OC 分支 `feat/console-next-integration`，目录 `oc/.worktrees/next-integration`。固定 CLI/Console 程序来自 clean source `a7ebefb03f8669cb93eb927a96f320a6e20229d4`；后续提交是验收夹具和文档，不冒称已重建程序。
- OtterIO 分支 `feat/console-next-protocols`，目录 `otterio/.worktrees/next-protocols`。固定修复版服务端来自 clean source `d2479684b344fb1f2975f546471888749f75d671`。
- 程序均是开发构建，不是已发布资产。实际 BuildInfo、摘要、harness 版本、通过/失败及清理边界保存在原始报告中。

OIDC 使用隔离临时 Keycloak，保留严格 discovery、state/nonce/PKCE、JWT/STS 校验、后台刷新、固定浏览器期限和独立身份会话。未知 native parent 的外部 OIDC 会话不开放 IAM 写入或自身改密。

用户、组、服务账号写入采用一次性计划与服务端锁内重新授权、全关系 CAS、权威影响范围。旧服务端缺少新能力时拒绝且零写；创建不能回退到无条件路径。真实验收发现并修复父用户停用后 native STS 仍可访问的问题，native STS 现在与父账户当前状态和当前用户/组权限求交集。

## 已完成的开发证据

- [执行记录](execution.md)与[任务状态](registry.json)提供依赖、所有权和实际证据。
- [修复版原生 IAM](../../console-relationship-protected-sts-fixed-results.json)：单/双 HTTPS 各29项，包含 service/STS 停用、恢复、删除、同名重建与旧 cookie 不复活；19/22字符已有身份碰撞冲突且零写。
- [旧服务端能力拒绝](../../console-relationship-protected-legacy-results.json)：同 OC，单/双 HTTPS 各13项。
- [修复版设置和协议](../../console-d247968-final-settings-protocols.md)：设置四入口84项，Range、diagnostics、真实 OIDC STS通过。
- [f2 开发存储证据](../../console-protected-pair-storage.md)：写入88、设置84、版本复制52项；[功能证据](../../console-protected-pair-features.json)四入口68项。它们保留其真实旧 server source，不改写成 d247 的全功能认证。d247仅重跑与授权修复有关的验收。
- Go并发回归、界面88项、lint0issues通过。默认跳过的 OIDC Go集成测试没有计入真实 STS通过。
- 真实浏览器报告见[OIDC验收说明](../../console-oidc-browser-acceptance.md)。最终 d247 配对单 TLS（13项协议+11项真实浏览器）与双独立 CA TLS（14项协议+11项浏览器）均首次通过，清理通过。

## 后续 agent 的明确工作边界

1. 集成 agent：核对每份来源/摘要/实际运行检查，整理真实报告到能力矩阵；不能伪造 gate check IDs，也不能把 development/module build 改称 release asset。维护共享文件窗口，先分配写入所有权再并行。
2. 发布与部署 agent：按任务05生成可安装配对候选、镜像/归档/来源/校验和及 SBOM；实际运行承诺平台和 amd64/arm64、server/gateway、代理边界、升级持久化与回退。交叉编译不替代运行；发布 pin 仅在实际认证后更新。
3. 安全与稳定性 agent：检查权限安全的诊断与全策略枚举范围、脱敏审计、并发刷新/传输/诊断和退出后资源增长；预先登记预算并保留测量。未完成范围不能声称等价替代。
4. 删除 agent：以 OtterIO `docs/web-console-removal-inventory.md` 为依赖清单，严格保留 S3/Admin/STS/metrics/health、共享 JWT、终端日志与 Admin listener 配置。等待任务05正式配对、OIDC认证及真实通知周期后才默认关闭；有限回退窗口结束后再物理删除。

每轮最多四个 agent（包括协调者），对同一源码/程序组合执行受影响测试，保留首次失败。临时身份提供方与存储夹具不能访问现有账号或部署；没有新的授权不推送/发布/合并或改生产部署。

## 删除尚未满足的门禁

28项能力矩阵尚未获得完整正式发行证据。通知要求至少两个实际通知发行周期，并解决迁移问题，再以公布期限的 opt-in fallback 过渡；物理删除在窗口之后。当前没有本轮正式发行 tag/date 或回退结束日期，开发通过不能替代这些条件。

首次 STS400 与包装器 PermissionError 未复现，原原因仍未知；失败证据和安全诊断都保留。已确证 native STS200 授权缺陷则另有明确修复和新资产通过证据，不能把两类问题混为同一根因。
