# OC 阶段一构建与发布基线

> 阶段实现与验证记录：本文保留编写时的源码背景、验证结果和限制。当前安装、容器与操作步骤见[文档目录](zh_CN/README.md)，支持范围见[兼容指南](zh_CN/compatibility.md)。早期阶段的待办状态不代表当前项目状态。

阶段一对应 P0-01、P0-05、P0-06：固定可获取的 OtterIO SDK 版本，统一构建工具链，隔离继承自 MinIO 的更新、上传和发行渠道。完整 S3 与管理功能兼容属于阶段二及后续工作。

## 固定版本

- OtterIO 提交：`be8596f0d69d530586f35366fb2d5c79bdc54399`。
- Go module 版本：`v0.0.0-20261004215341-be8596f0d69d`。
- Go 工具链：`1.27.1`；构建、容器与 CI 禁止隐式下载其他工具链。
- OC 的 Go module 路径暂时保留 `github.com/soulteary/mc`，配置迁移和 module 改名另行实施。
- `go.mod`、`go.sum` 固定共同依赖，不包含本地 replace。
- 构建产物名称为 `oc`；Windows 为 `oc.exe`。

SDK 是通过远程仓库获取并校验的固定版本，不依赖相邻的 OtterIO 目录。OC 对 SDK 的调用经过编译校验，管理契约测试验证真实客户端的 SigV4 请求、`/otterio/admin/v3/info` 路径以及响应结构解析。该测试使用本地模拟服务，不代表完整 OtterIO 联调通过。

## 构建与检查

在 OC 目录执行：

```sh
go mod download
go mod verify
make build
go vet ./...
go test -race ./...
python3 buildscripts/verify-release-boundaries.py
```

CI 覆盖 Linux、macOS 和 Windows。测试配置使用临时目录；HTTP 契约测试需要允许绑定本机临时端口。退出码测试使用受控子进程，避免依赖 GNU/BSD 命令之间的行为差异。

本地构建元数据默认采用 Git 提交时间，可用 `SOURCE_DATE_EPOCH` 显式指定；不再使用执行构建时的当前时间。`MC_RELEASE` 的旧名称暂时保留，等待后续环境变量迁移。

## 更新与健康报告

`oc update` 返回退出码 1 和明确禁用信息，不检查远程版本、不下载二进制、不替换当前程序。启动时不再自动检查 MinIO 更新。独立 OC 更新渠道完成校验、平台匹配和失败恢复设计后才能重新启用。

`oc admin report ALIAS` 保留本地报告功能；历史命令名暂时兼容。显式传入 `--license` 或 `--dev`（包括空 license 和 `--dev=false`）会在连接服务端之前报错。SUBNET 上传实现已经移除。

普通别名显示和 JSON 状态输出隐藏访问密钥与私钥。调试请求隐藏认证头、会话令牌、Cookie、SSE-C 密钥、签名查询参数及 URL 内嵌凭据，并使用请求副本，不修改实际发送的内容。调试响应隐藏敏感头和重定向 URL，不再打印可能回显凭据的响应正文。明确请求生成的分享链接、配置导出和本地健康报告仍可能包含敏感数据。

新配置不再自动添加 MinIO 公共演示别名；已有配置和历史迁移数据保留。

## 发行边界

- GoReleaser 项目、二进制及 deb/rpm 包使用 OC 名称；自动发布保持关闭。
- Dockerfile 从当前工作区源码构建，不克隆另一份仓库，也不下载官方 mc。
- Dockerfile.dev 与 Dockerfile.release 使用本地 `oc` 产物。
- 容器入口为 `oc`；许可证、NOTICE 和归属说明保留。
- `docker-buildx.sh` 默认仅构建；发布必须显式传入 `--push`。
- 默认镜像命名为 `soulteary/oc`，支持 `OC_IMAGE`、`OC_TAG`、`OC_PLATFORMS` 覆盖；命名配置不表示镜像已经发布。
- `functional-tests.sh` 默认调用当前目录的 `oc`，可通过 `OC_BINARY` 指定程序。

发布边界检查已纳入 CI，防止更新、上传、容器及发行入口再次引用继承的 MinIO 渠道。历史配置、许可证和文档归属引用不属于自动外部访问。

## 本次验证与后续边界

已验证：同一源码连续两次构建的 SHA-256 一致、发行与 CI YAML 可解析、macOS arm64 原生构建、完整 Go 测试及竞态检查、静态检查、依赖校验、Linux amd64 和 Windows amd64 交叉构建、发布边界检查、脚本语法检查，以及更新与上传禁用命令的失败行为。

尚未验证：Docker 镜像构建与运行、GoReleaser 完整打包、真实 OtterIO 对象及管理联调。当前环境缺少 Docker Buildx 与 GoReleaser；镜像和安装包须在具备工具的环境完成检查后发布。其他发行架构仅保留构建配置，尚未完成本次交叉构建验证。

阶段二继续实施独立管理地址、Prometheus 路径修复和核心端到端联调。阶段一不改变服务端代码，也不宣称完整协议兼容。

阶段二实现与真实联调结果见 [核心接入说明](oc-phase-two.md)，其中明确列出了配套服务端修复和验证范围。
