# 本轮执行记录

执行日期：2026-10-12（Asia/Shanghai）。从同步后的 OC origin/main `1c5c3edc` 和 OtterIO `8cc518b` 启动，原 checkout 保留。四个并发名额：root协调/集成，identity身份与服务端协议，parity能力矩阵后接 OIDC，deployment部署后接临时 Keycloak。独立 worktree 和分支；当前认领见 registry.json。

## 首批集成

- OC主体键及严格关系快照：原提交 `0f7adee5`，集成 `157fac2e`。快照验证不是权威关系查询，生产关系协议仍在开发。
- 28项功能矩阵与删除门禁：原提交 `5b5426f1`，集成 `5b32e51a`；协调者追加 `4c4767f1`，绑定选定CLI/Console程序摘要并拒绝错误profile形状。矩阵尚未认证完整替代。
- Compose持久化/回退及报告清理：原提交 `72d5e464`，集成 `c10b4948`。真实发布对arm64/local/HTTP的8项检查通过；5项单测通过。未改发行pin。
- OtterIO三项条件IAM协议与持久化锁内重新授权：`cbbffbd40531f101cb67c263c69010a0a1b8ed4e`。专属race/真实路由检查通过；源自既有导出补丁并补授权竞争保护。正式发行仍未包含本轮代码。

## 集成检查

客户端准确基线 `b32781defdcc859081fb7797ea15339c894804b0`，服务端 `cbbffbd40531f101cb67c263c69010a0a1b8ed4e`。程序摘要、build-info与harness信息见[功能组合记录](../../console-next-features.md)。本轮独立存储/身份/CA夹具，不复用用户配置。

- local alias features四入口各17组，共68组通过。
- native单/双HTTPS各26组，共52组通过。
- Console、storageclient、consoleapi、oidc、Console入口的Go race回归通过；consoleapi无测试，OIDC的真实STS独立测试需指定服务端，未把默认skip计为端到端通过。
- lint 0 issues；门禁5项单测与Compose5项单测通过。

这些结果是OIDC接入前基线，后续集成需重跑受影响组合；不是正式发行、OIDC登录或删除授权。

## 继续中的工作

parity获得认证核心/启动/UI独占窗口，实现OIDC安全会话、临时客户端刷新和生命周期。deployment按用户确认使用隔离临时Keycloak，记录固定镜像与CA/profile、两用户权限和清理证据。identity继续生产权威关系接口；OIDC未知native父关系的IAM写保持拒绝。尚未完成的任务不改verified状态。

旧Web默认值和资源保持。OIDC替代、正式配对、全平台/升级和通知/回退门槛未全部完成，删除不得先执行。

## OIDC接入后的检查

开发集成 `ddcc66bb89d7cc41966286a7a2f887d772fee047` 已包含 OIDC 登录、独立 STS 客户端、不可变凭据世代、后台刷新和固定浏览器期限。启动和界面由 root 实现，生命周期由 parity 实现；随后安全复查仍继续。88组界面行为和5个相关Go包race通过（consoleapi无独立测试）。

新Console与服务端 `19560364ec735f5b811964f5199b2b59e7100400` 的native单/双HTTPS各26组通过，原始记录为[OIDC接入后的native回归](../../console-next-oidc-native-results.json)。此次使用CLI `b32781de` 与Console `ddcc66bb`，不把不同源资产声称为同一发行。

临时Keycloak协议夹具已集成；真实浏览器Console的刷新、固定期限、退出和双用户隔离仍在验收。发现Docker身份提供方与本机存储亚秒时钟差可能触发未来iat拒绝，保留失败记录并使用同容器时钟进一步验证。OIDC的IAM写/自身改密仍拒绝；用户关系耦合改密协议正在实现。
