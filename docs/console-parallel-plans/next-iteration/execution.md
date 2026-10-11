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

最新 OIDC 安全修复集成 `51ee915235ac20a3ee49a75fd60136f3d2032b9d`：请求固定世代后仍检验绝对到期；刷新失败原子关闭再唤醒等待者；尾斜线根URL回调规范化。相关Console/OIDC/CLI race通过，lint 0 issues。local四入口各17组再次通过，共68组；[原始结果](../../console-next-oidc-features-results.json)。

已知空关系集合和未知关系集合严格区分，`85eec74d` 的执行器测试证明空组不撤销无关会话，但仍保留全部写槽屏障；组原子协议尚在开发，未因此开放组写动作。

## 条件 IAM 与真实浏览器收尾

截至服务端 `ff6e66f3`，用户改密/绑定、组成员与状态/空组删除、服务账号状态/改密/策略/删除均已具备锁内重新授权和全关系条件写协议。OC 已接入一次性计划、权威影响范围、单次派发和会话撤销；旧服务端缺少耦合协议时保持拒绝。服务账号策略只允许 root 或权威 native parent 本人修改；委托管理员不能编辑其他人的子账号策略。OIDC 会话仍禁止 IAM 写操作。

真实 IAM 验收发现服务端明确权限拒绝曾被客户端视为未知结果；`52310833` 补齐精确错误码分类，保留未知错误的保守处理。该构建与最终服务端的单、双 TLS 各22组通过；同构建与旧服务端各12组通过。[原始验收与失败记录](../../console-relationship-iam-acceptance.md)保留资产及脚本绑定，未将后续源码变更冒称为已验收。

临时 Keycloak 协议验收的双用户隔离、刷新和固定期限已通过，参见[双CA报告](../../console-keycloak-console-dual-results.json)。真实 Chromium 另发现 OIDC 回调303重定向链在根页面仍标记跨站，被已有保护拒绝。`4f32ddba` 改用不含回调参数的固定返回文档，保留根页面和API的跨站拒绝；实际浏览器和同构建 IAM 复验正在进行。最终报告需记录临时浏览器环境及证书处理边界。

最终 IAM 夹具增加“恢复原同名同密码后旧浏览器 cookie 仍401”的检查，用于区分 Console 主动撤销与上游凭据不匹配；验收包装器独立记录临时进程清理。两者需实际复跑，不能倒填到既有报告。

旧 Web 只整理删除依赖清单。本轮开发代码和临时构建不等于正式发行；跨架构、升级/回退、稳定性、真实通知周期和有限回退窗口仍是删除前置条件。

## 受保护创建与最终固定开发配对

OC `a7ebefb03f8669cb93eb927a96f320a6e20229d4` 与 OtterIO `f2bdc23403ee0c4b35f9ce4092c68b80074349e1` 已固定为同来源、clean BuildInfo 的开发程序。上述历史记录不覆盖这组程序；新的报告逐项独立绑定。

本轮补齐用户 enable/disable/delete 的全关系条件协议；服务账号创建使用服务端锁内重新授权、父身份状态/生命周期检查与签名关系前提，防止旧请求在父用户删除重建后创建孤儿身份。旧服务端缺少新能力时拒绝，不能退回无条件创建。组和用户一次性计划同时绑定 backend、存储命名空间、actor 和 session lifetime。

- [存储验收](../../console-protected-pair-storage.md)：写入 88、设置 84、版本复制 52，共 224 项通过，12 个场景清理全部通过。
- [功能验收](../../console-protected-pair-features.json)：四种 HTTP/TLS 入口各 17 项，共 68 项通过，覆盖 ZIP 字节、特殊 key、分享与版本等。
- 相关 Go race 回归通过；lint 0 issues；界面行为 88 项通过。默认 Go OIDC 测试不能代替真实 STS 验收。
- [协议包装器首次失败](../../console-protected-pair-protocols-wrapper-failed.json)：Range 与 diagnostics 已通过，随后 preparationError PermissionError。独立真实 STS 测试另得到 400 InvalidParameterValue，正在定位；不能将任一失败改为通过。
- 真实 Keycloak 单 TLS 浏览器验收已通过；双 TLS 首次失败保留，诊断与受限清理改进后复验中。最终原始报告另行集成。

这是开发迭代结果。原生 IAM 最终同资产验收、真实 STS/双 TLS 浏览器收尾仍在执行。正式发行配对、跨架构、升级回退、通知周期及有限回退窗口仍未完成，旧 Web 默认和资源保持。

## 真实 STS 父授权缺陷与修复

最终 `f2bdc234` native fixture 发现父用户 disable 后 service GET 返回403，而 STS GET 仍200。该失败及无效长度/reader权限的夹具失败全部保留。不能通过修改预期将200解释为权限撤销。

服务端 `d2479684b344fb1f2975f546471888749f75d671` 在既有读锁内检验当前 STS child 与 active regular native parent，并将当前父用户/组权限与临时凭据发行策略、session restriction 求交集；不改变没有 native parent 的外部 WebIdentity 或已有 LDAP 分支。父 username/userid 条件来自权威身份，保留实际 AssumedRole/source 等请求条件。显式 service key 缺席检查移到生成前，已有长 native key 的碰撞也得到零写冲突，不放宽凭据长度限制。

- root 复验身份/创建/关系协议 race 通过；新增 STS parent race 通过。
- [修复后的真实 IAM](../../console-relationship-protected-sts-fixed-results.json)：单、双 HTTPS 各29项首次通过，两层清理通过；service/STS disable403、enable200、delete及同名同secret重建后拒绝，原cookie不复活，19/22字符collision409零写。
- [旧服务端能力拒绝](../../console-relationship-protected-legacy-results.json)：同OC a7ebefb0、legacy cbbffbd，单、双TLS各13项通过，缺新cap不写入。
- [修复后的设置与协议](../../console-d247968-final-settings-protocols.md)：设置四入口各21项首次通过，range/diagnostics/真实OIDC STS均通过、无skip或PermissionError。

CLI/Console程序仍是同一clean source a7ebefb0；新Darwin/Linux服务端精确source均d2479684、vcs.modified=false，摘要随报告保存。f2的既有写入/版本/功能/OIDC报告保持其真实来源，不改成d247验收。d247真实Keycloak浏览器single/dual复验中。
