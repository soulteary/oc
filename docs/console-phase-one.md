# Web 控制台第一阶段实现与验证

[完整迁移计划](console-migration.md) · [使用指南](zh_CN/console.md) · [English usage](console.md)

日期：2026-10-08（本地时区）；测试结果中的时间为 UTC。本文保留 P1 完成时的范围和证据：本机单操作员、单别名的独立只读 `oc-console`，当时 P2–P5 尚未执行。后续进展见[第二阶段记录](console-phase-two.md)。OtterIO 的旧 Web 入口及 browser 默认值保持不变。

## 已交付的代码边界

- `cmd/oc-console`：只读加载 config v10，明确选择别名，只监听回环 IP，打印随机登录码，负责有界关闭。新增 Make 构建/测试目标；默认 CLI 构建不变。
- `internal/consoleapi`：不含凭据、配置写入或本地文件能力的接口与 DTO。
- `internal/clienttransport`：端点验证、独立 CA、签名空请求体、拒绝管理重定向。现有 CLI adapter 委托这些规则，保留管理流适配。
- `internal/storageclient`：按启动身份执行桶、单页对象列表、HEAD/GET 与 AccountInfo；连接池按实例持有，无 CLI 全局缓存。固定 SDK 的分页接口缺少 context 参数，因此每次分页使用专属 transport wrapper 注入调用 context，不修改共享客户端。
- `internal/console`：会话、CSRF、精确 Host/Origin、跨站拒绝、CSP/禁止缓存、并发限制、流式附件下载和取消。元数据 15 秒、登录读取 10 秒、下游下载写入空闲 30 秒；取消能解除已阻塞的真实 socket 写入。
- `internal/console/web`：内嵌 HTML/CSS/JS，提供桶选择、原样前缀导航、真实分页、刷新、下载和退出；AccountInfo 可独立降级。没有新增 Node/npm 依赖，也没有浏览器凭据持久化。

## 真实接入发现及修复

原始当前服务端 `c8a09ca` 的 root AccountInfo 返回 HTTP 404、`XOtterioAdminNoSuchUser`。有效 root 凭据不存储在 IAM 用户表，接口却仍调用普通用户的 PolicyDBGet。修复在已验签的 owner 分支使用内置 Admin policy，保留普通、STS 和服务账户路径与逐桶 IsAllowed 判定；没有把 root 注册为 IAM 用户。

修复位于 OtterIO 的 `cmd/admin-handlers-users.go`，新增 `TestAccountInfoOwnerAndScopedPrincipals`。普通与 race 测试覆盖 8 个子测试：root/internal、root/LDAP、普通读写隔离、STS 会话限制与 claim 不匹配、服务账户会话限制与 claim 不匹配、无效 root 签名。存储输入为确定性 mock，SigV4、JWT 和 IAM 使用真实实现；下方真实进程验收补足监听器和 SDK 路径。

[otterio-account-info-compat.patch](../buildscripts/otterio-account-info-compat.patch) 将同一修复及测试应用到固定 SDK 的服务端测试副本。四补丁依序 apply/check 成功，该隔离副本的新增 8 个子测试通过。SDK pin 和 Go 依赖没有修改。

## 本次通过的检查

1. OC 完整 `go test -mod=readonly -tags kqueue ./...`；新增客户端、HTTP 会话及启动入口的 race tests；受影响的 CLI 管理端点、独立 CA、拒绝重定向、控制台流取消和 multipart 清理回归。
2. `go vet ./...`、golangci-lint 2.14.0（0 issues）、格式与 diff 检查，以及发布边界检查。构建使用离线已有依赖；lint 缓存因沙箱无法写入产生警告，但检查本身成功。
3. 本机 Darwin arm64 的 CLI 与控制台构建、Linux amd64 和 Windows amd64 的控制台交叉构建。后两者没有原生运行验证。
4. [当前服务端结果](console-phase-one-results.json)：`c8a09caf06c56483e1d21a1c9cdedc3dc3b76381` **加本次 owner 补丁**，单 HTTP、双 HTTP、独立 CA 的双 TLS，3 场景各 9 组验收。
5. [固定基线结果](console-pinned-baseline-results.json)：`be8596f0d69d530586f35366fb2d5c79bdc54399` **加四项兼容补丁**，同样 3 场景各 9 组验收。不是未打补丁的 SDK/server commit 验收。
6. 实际浏览器验证：登录、111 条真实数据从 100 条追加至 111 条、中文前缀导航、账户概览、退出返回登录。中文/空格/`+`/`%` 下载通过浏览器下载器完成，18 字节内容的 SHA-256 与源数据一致。截图作为本次聊天附件保存。

每个真实场景覆盖：匿名拒绝、登录码和 Origin、cookie/CSP/禁止缓存、Host/只读路由、continuation token、不重复列表、前缀/特殊 key、空与 1 MiB 下载摘要、对象不存在时在响应头前报错、AccountInfo 字段白名单、受限身份允许/拒绝、CSRF/退出撤销、原配置不变和正常有界退出。所有凭据与存储都是临时生成，不使用用户的配置或已有部署。

真实 OtterIO 当前拒绝包含连续双斜杠的对象名。客户端/HTTP/UI 对这种 literal key 的保真由模拟协议与前端合同测试覆盖，不能将其记作当前 OtterIO 的成功存储测试。

## 重现与 CI

先按[开发指南](development.md#reproduce-the-server-integration-fixture)复制固定模块源码并应用四个补丁，然后构建控制台：

```sh
go build -mod=readonly -trimpath -o "$OC_INTEGRATION/oc-console" ./cmd/oc-console
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio" \
  --server-source be8596f0d69d530586f35366fb2d5c79bdc54399 \
  --server-patch buildscripts/otterio-core-compat.patch \
  --server-patch buildscripts/otterio-runtime-compat.patch \
  --server-patch buildscripts/otterio-http-api-compat.patch \
  --server-patch buildscripts/otterio-account-info-compat.patch \
  --output "$OC_INTEGRATION/console-integration.json"
```

脚本需要 Python 标准库与 OpenSSL，自动使用临时目录、测试账号和回环服务，结束时清理自己的进程。`--preview` 可保留最后一组临时数据供手工 UI 验证，此模式不会被记作通过的自动验收。

Go CI 已接入控制台构建、AccountInfo 回归及该真实验收脚本，跟随 Linux/macOS 与 CGO 0/1 的服务端矩阵保存结果。工作流尚未在本次聊天中远程运行；本机通过不等于远程 CI 已通过。默认发行归档和容器尚未增加实验控制台附件。

## 当前限制与下一阶段

这是本机只读原型。尚未实现上传、删除、版本 UI、Range、ZIP、预签名共享、OIDC、管理员编辑、临时凭据自动续期、多 alias 切换或多人集中部署。未验证分布式、历史版本、第三方 S3、原生 Linux/Windows 控制台运行、长时间并发稳定性或生产代理拓扑。

下一工作包为 P2 的流式上传和取消清理，再实现显式删除与批量失败报告。P3 补必要账户/桶管理；P4 验证集中部署与独立发行；P5 才进入旧 UI 的弃用和移除。回退第一阶段只需关闭 `oc-console`，继续使用原 Web 或 CLI。
