# 阶段二：OtterIO 核心接入

阶段二覆盖 P0-02 独立管理地址和 CA、P0-04 Prometheus 配置修复，以及 P0-03 核心端到端联调。对象请求继续使用别名的 S3 地址，管理请求可以使用独立地址。

## 配置管理入口

```sh
oc alias set store http://127.0.0.1:9000 ACCESS_KEY SECRET_KEY \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc admin info store
```

TLS 管理入口可以指定独立 CA：

```sh
oc alias set store https://s3.example.com ACCESS_KEY SECRET_KEY \
  --api s3v4 --admin-url https://admin.example.com \
  --admin-ca /path/to/admin-ca.pem
```

别名保存可选的 `adminURL`、`adminCAFile` 字段；旧配置不需要迁移。未配置管理地址时沿用 S3 地址。别名列表会显示管理地址和 CA 路径。

`alias set --admin-ca` 将相对 CA 路径转换为绝对路径保存，后续切换工作目录不会使管理连接失效。临时参数和环境变量中的相对路径按执行时的工作目录解析。

管理地址和 CA 各自按照以下顺序取值：显式命令参数、别名环境变量、全局环境变量、别名配置。管理地址最后回退到 S3 地址。环境变量分别为 `OC_ADMIN_URL_store` / `OC_ADMIN_CA_store` 和 `OC_ADMIN_URL` / `OC_ADMIN_CA`；别名后缀保持原大小写。临时覆盖不会写入配置：

```sh
oc --admin-url https://admin.example.com --admin-ca /path/admin.pem admin info store
OC_ADMIN_URL_store=https://admin.example.com oc admin info store
```

兼容原有 `MC_HOST_store` 对象入口覆盖，同时继承该别名已保存的管理配置。独立管理 CA 加入系统信任池，不自动信任对象入口配置目录中的额外 CA；对象入口的 CA 仍放在配置目录的 `certs/CAs/` 中。客户端缓存区分入口、凭据、会话令牌、TLS 和 CA 内容。

管理地址必须是 HTTP/HTTPS 根地址，不接受内嵌凭据、查询参数、片段或路径前缀。管理请求拒绝重定向，避免认证头或加密管理正文被转发。使用反向代理时须直接暴露管理根路径。

## Prometheus

```sh
oc admin prometheus generate store
oc admin prometheus generate --metrics-type node store
oc admin prometheus generate --metrics-type legacy store
oc admin prometheus generate --public store
oc admin prometheus generate --metrics-ca /etc/prometheus/s3-ca.pem store
```

默认 cluster 路径为 `/otterio/v2/metrics/cluster`，node 路径为 `/otterio/v2/metrics/node`，legacy 路径为 `/otterio/prometheus/metrics`。抓取目标始终使用 S3 地址和协议，服务信息查询使用管理地址；双端口部署不会把指标请求发到管理端口。默认生成静态凭据对应的认证令牌；服务端启用公开指标时使用 `--public`。临时会话凭据不能生成长期指标令牌。配置生成不再依赖 MinIO 版本日期，空服务列表会明确报错。

私有 HTTPS 证书使用 `--metrics-ca` 生成 `tls_config.ca_file`；此路径属于 Prometheus 所在主机，不要求文件存在于 OC 所在主机。必须提供 S3 指标入口的 CA，双端口部署不能复用独立管理 CA。公开可信证书可省略此参数；HTTP 目标不接受此参数。测试会按生成的 CA 配置实际抓取指标，并检查对应 YAML 字段。

## OtterIO 服务端配套修复

基线 SDK / 服务端版本为 `v0.0.0-20261004215341-be8596f0d69d`。真实联调发现 Fiber 管理路由验证了查询参数，却没有将其传给旧管理处理器，导致策略绑定等请求报 `InvalidArgument`。

配套修改位于 OtterIO 的 `cmd/fiber_admin_router.go`：普通与流式管理路由仅桥接该路由声明并验证过的查询参数，已有路径参数优先。`cmd/admin_query_bridge_test.go` 验证普通/流式路由、参数白名单、路径优先级及无效参数拒绝。

过渡补丁同时包含该服务端回归测试，OC 的集成 CI 会先运行桥接测试，再执行真实联调矩阵。

本阶段完整通过的联调使用包含上述修复的服务端。部署时需要带上该修复；未修复的基线服务端仍存在策略绑定问题。OC 没有引入本地 `replace` 或引用未发布的服务端版本；CI 从固定模块源码构建服务端，并应用 `buildscripts/otterio-core-compat.patch`。该补丁已经验证可应用于固定基线；后续发布包含修复的 OtterIO 版本后，应升级依赖并移除过渡补丁。

客户端还规范化 SDK 的空 PUT 请求，避免空正文以 chunked 形式发送时被 Fiber 错误解释；回归测试检查实际传输的请求体和编码。

## 验证方法和范围

```sh
go test -race ./...
golangci-lint run --max-same-issues=100000
python3 buildscripts/verify-release-boundaries.py
python3 buildscripts/test-core-integration.py \
  --oc /path/to/oc --otterio /path/to/otterio --report /tmp/core-integration.json
```

集成脚本需要 Python 3、OpenSSL 和本机监听权限，使用临时数据目录、随机端口和临时凭据，退出时清理进程及目录。CI 已加入同一矩阵，并保存 JSON 结果。

矩阵包含单端口 HTTP、双端口 HTTP、单端口 TLS、双端口独立 TLS 证书，以及双端口公开指标。每个场景验证桶操作、空对象、小对象、中文特殊字符名称、65 MiB 分片上传与下载校验、服务端复制、stat、mirror 覆盖及删除、分享下载、三种指标路径实际抓取、管理用户和自定义策略、受限账号读写边界、错误凭据拒绝及清理。另有管理配置优先级与错误入口检查。

2026-10-07 本地真实联调全部通过：以上五个场景分别通过 49、50、52、53、49 项检查，共 253 项。矩阵额外覆盖 `MC_HOST` 管理配置继承、管理 CA 环境变量覆盖及相对 CA 路径持久化后切换目录。CI 配置已加入，但本次未在远程 GitHub Actions 执行。

单元测试覆盖地址校验、配置兼容、缓存隔离、独立 CA、未信任证书拒绝、管理重定向拒绝、空 PUT 编码，以及指标路径和认证令牌。OC 完整竞态测试和静态检查、OtterIO 参数桥接竞态回归测试均已通过。

配置文件整体校验也包含 `adminURL`，防止手工编辑后绕过别名创建时的地址检查。CA 文件在管理连接初始化时读取和校验，不要求启动时访问所有别名的 CA 文件。

验证范围为本机单节点服务。分布式、纠删码、网关、对象锁、复制、生命周期、事件及其他高级管理功能不属于本阶段的验收范围；镜像及发行包验证继续沿用阶段一的待办边界。
