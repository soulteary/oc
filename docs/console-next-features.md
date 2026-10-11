# 本轮源码组合功能验收

2026-10-12（Asia/Shanghai）。使用独立一次性存储/身份/CA，不修改已有部署。

OC 客户端源码 `b32781defdcc859081fb7797ea15339c894804b0`，程序内嵌 vcs.modified=false；CLI SHA-256 `ec5f6bade1a9ed7a50376a3749a289e6caa4768a01b8187333a8589e326aae6a`，Console SHA-256 `d08223893a6c13d1b5ac84cad007e783e322e0bbe58eec7bf93b3eac132b66db`。

OtterIO 源码 `cbbffbd40531f101cb67c263c69010a0a1b8ed4e`，程序内嵌 vcs.modified=false，SHA-256 `7c52ea3a1e8a4ed6c50ef3c570261268ee5bcb50d0a89a7e3661bdcba459d4fe`。此源码增加三项条件 IAM 协议和锁内重新授权，不是原正式发行版。

`test-console-integration.py --features` 在 single-http、dual-http、single-tls、dual-tls 各通过17组，共68组。与原发布对的 iam-create 缺口不同，本轮真实服务器已实现创建协议。报告包含版本/桶/ZIP/共享/IAM 等具体检查，见[原始记录](console-next-features-results.json)。此运行使用该集成提交中的 harness，未混入 settings 的版本状态夹具。

本报告认证该源码组合的 local alias features 范围，不认证 OIDC、完整原生管理、发行资产、所有平台、升级、长期稳定性或旧 Web 删除。68组通过不能替代其余能力矩阵项。

## 原生独立会话回归

相同准确程序组合，`--native --scenarios single-tls,dual-tls` 各26组通过，共52组。见[原始记录](console-next-native-results.json)。这是 OIDC 接入前的原生 HTTPS 回归基线；OIDC 接入后必须复验，不能由此推定临时身份已支持。
