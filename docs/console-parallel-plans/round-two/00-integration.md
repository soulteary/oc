# 第二轮契约与集成

## 目标与基线

OC main `7d4fb7fb922a9599f8d9805651693c7c20d0a2f1`，包含已合并 #34–#38 和各 PR 后续审查修正。首批 Range、诊断 API、OIDC 协议客户端和原生撤销 ledger 已存在，不重复开发。开始时记录新的实际 main SHA。

## 职责

- 冻结身份作用域、权威关系、撤销执行器、临时客户端替换、审计事件以及新增公共类型接口。
- 按任务授予共享文件独占窗口，登记实际负责人；未授予窗口的任务只修改自己的新增模块。
- 集成 IAM handler/dispatcher、公共类型、主 UI、capabilities 和总文档。
- 在实际合并版本执行组合 race/lint、权限与生命周期回归；记录单项通过不能覆盖的矩阵。
- 管理需要服务端原子协议的独立 PR 和最低版本，禁止客户端 preflight 冒充原子授权保证。

## 文件归属

- 01 独占 `.github/workflows/go.yml` 与本轮真实协议 CI 接入；07 不并行修改该文件。
- 02 拥有身份 ledger、索引及撤销模块；修改 `native_rotation.go`、`native_iam_retirement.go` 须先取得窗口。
- 04 独占 `server.go`、`session_clients.go`、会话认证与 `cmd/oc-console` OIDC 启动配置，其他任务提交接入补丁。02 的核心接入先完成，再转交 04。
- 03a/03b/03c 分别拥有新增用户/组/服务账号辅助模块、专属 handler 与验收脚本。共享 `internal/storageclient/iam.go`、`internal/console/iam.go` 及 `internal/consoleapi` 原有文件归 00，agent 不各自重写总 dispatcher。
- `web/app.js`、`web/index.html`、`web/i18n.js`、全局 capabilities 和总迁移文档归 00。第一批临时授权 05 独占 app.js/index.html/i18n.js 和专属 DOM 测试，00 不同时编辑，交付后回收；其他阶段 05 可独立开发诊断组件及测试；04/03/06 的 UI 接入也由 00 排序。
- 06 拥有新增审计/部署模块、独立示例和压力脚本；启动/会话 hook 交 04/00。
- 07 拥有发布管线、兼容清单、Compose pin、最终矩阵报告；08 在 OtterIO 独立分支工作。

文件路径是职责范围，不要求为了隔离而改变现有架构。新增文件命名与模块接口先确认，避免同名冲突。

## 本次授予的独占窗口

本轮第一批 CI agent 独占 go.yml 和真实协议 wrapper；身份 agent 暂时独占 server.go/session_clients.go、identity 模块及原生退休实现；诊断 UI agent 暂时独占 app.js/index.html/i18n.js 和专属 DOM 测试。协调者只审查与组合，不同时编辑这些文件。交付后窗口回收，04 尚未启动。

## 验收与交付

形成接口冻结记录、认领清单、合并顺序和组合验收证据。OIDC issuer/sub 不等于 IAM 父用户；未知关系不是空影响集合。发布、默认关闭和删除分别判断，不用一次局部验收替代全部门槛。

## 交付规则

在独立 worktree 和分支开发，开始前核对 origin/main、目标任务、冻结接口及文件负责人。共享文件的接入补丁交 00 集成，不在其他 agent 分支切换或覆盖文件。

每个 PR 说明最终行为、相关验证、真实服务器/资产摘要、权限负例、未解决项及后续依赖。未知身份关系、未知变更结果或服务器缺少必要协议时保持拒绝，不自动重放身份修改。凭据不进入浏览器 JSON、日志、偏好或报告。真实验收仅使用临时账户、数据、配置和进程，不修改已有 alias 或部署。

本任务文件用于认领和交付，不表示实现、合并或正式发布已经发生，也不授权提前删除旧 Web。
