# OC

[![Go 检查](https://github.com/soulteary/oc/actions/workflows/go.yml/badge.svg)](https://github.com/soulteary/oc/actions/workflows/go.yml)
[![发布](https://github.com/soulteary/oc/actions/workflows/release.yml/badge.svg)](https://github.com/soulteary/oc/actions/workflows/release.yml)
[![许可证：Apache-2.0](https://img.shields.io/badge/license-Apache--2.0-blue.svg)](LICENSE)

用于 OtterIO、S3 对象存储和本地文件系统的命令行客户端。

[English](README.md) · [文档目录](docs/zh_CN/README.md) · [版本下载](https://github.com/soulteary/oc/releases) · [参与贡献](docs/zh_CN/CONTRIBUTING.md)

OC 提供对象上传、下载、查询和目录同步，也可以管理 OtterIO 服务端。项目继承 MinIO Client 的 Apache-2.0 代码，保留原始版权与归属说明。OC 是独立的社区项目，与 MinIO, Inc. 没有隶属或背书关系。

## 安装

从 [GitHub Releases](https://github.com/soulteary/oc/releases) 下载对应平台的归档，验证 SHA-256 后，将 `oc`（Windows 为 `oc.exe`）放入 `PATH`。[安装指南](docs/zh_CN/installation.md) 包含 Linux、macOS、Windows 和源码构建步骤。

发布工作流构建 Linux amd64 和 arm64 镜像，使用前选择清单中包含 `images` 的版本：

```sh
# 选择发布清单中记录了镜像的版本，并替换下方占位标签。
TAG=RELEASE.YYYY-MM-DDTHH-MM-SSZ
docker run --rm "ghcr.io/soulteary/oc:$TAG" --version
```

配置持久化、文件挂载、容器网络和摘要固定方式见[容器使用指南](docs/zh_CN/containers.md)。发布仓库配置相应凭据后还会同步推送 Docker Hub；实际发布到哪些仓库，以该版本的发布清单为准。

## 连接并传输文件

安装后，将示例地址替换成部署中的 S3 和管理入口。以下示例使用本机 HTTP；跨越不可信网络时使用 HTTPS。不在命令中填写凭据时，OC 会提示输入 access key 和 secret key：

```sh
oc --version
oc alias set store http://127.0.0.1:9000 \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/example
printf 'Hello from OC\n' > hello.txt
oc cp hello.txt store/example/hello.txt
oc cp store/example/hello.txt downloaded.txt
oc stat store/example/hello.txt
oc admin info store
```

单端口 OtterIO 部署省略 `--admin-url`。连接其他 S3 服务时，使用该服务的地址、凭据和桶寻址方式；OtterIO 管理接口需要单独验收。创建桶需要对应权限。私有 CA 和地址优先级见[配置指南](docs/zh_CN/configuration.md)，复制、同步及对象功能见[常用操作](docs/zh_CN/usage.md)。

## 主要功能

- 本地及 S3 文件操作：`ls`、`cp`、`mv`、`cat`、`find`、`stat`、`du` 和 `mirror`。
- 对象版本、生命周期、标签、保留期、法律保留、通知与复制，具体取决于服务端支持。
- OtterIO 管理：服务信息、用户、组、策略、服务账号、监控配置与诊断。
- 独立的 S3 和管理入口，以及单独配置的管理 CA 信任。
- 显式导入 mc 配置、JSON 输出和不显示凭据的 `doctor` 诊断。

[兼容范围](docs/zh_CN/compatibility.md) 区分已验证的操作和需要按服务商、部署方式验收的功能。固定 OtterIO 测试基线需要五项已记录的服务端补丁。分布式部署、外部 KMS、外部通知目标及任意第三方 S3 服务不在现有验收矩阵内。

## 从 mc 迁移

OC 使用独立配置目录：Unix 为 `~/.oc`，Windows 为用户目录下的 `oc`。它不会自动加载 `~/.mc`。

```sh
oc config import ~/.mc/config.json
oc --json doctor store
```

导入支持配置版本 10，校验通过后整体替换目标别名，并备份已有目标配置。证书和已保存的会话不会自动复制。导入前阅读[迁移指南](docs/zh_CN/migration.md)。

`OC_*` 环境变量优先于支持的旧 `MC_*` 变量。自更新和 MinIO SUBNET 上传已禁用，升级时使用经过校验的 OC 发布附件。

## 按任务查找文档

- [安装与升级](docs/zh_CN/installation.md)，或[使用容器](docs/zh_CN/containers.md)。
- [配置地址和 TLS](docs/zh_CN/configuration.md)、[传输与同步](docs/zh_CN/usage.md)，或[查询命令](docs/zh_CN/commands.md)。
- [管理 OtterIO](docs/zh_CN/administration.md)和[检查兼容范围](docs/zh_CN/compatibility.md)。
- [构建实验性的本机 Web 控制台](docs/zh_CN/console.md)。
- [排查故障](docs/zh_CN/troubleshooting.md)，或[私下报告安全问题](docs/zh_CN/security.md)。
- [构建与测试](docs/zh_CN/development.md)、[参与贡献](docs/zh_CN/CONTRIBUTING.md)，或[准备发布](docs/zh_CN/releasing.md)。

[文档目录](docs/zh_CN/README.md)也提供各阶段实现及验证记录的入口。

## 从源码构建

准备 `go.mod` 声明的 Go 版本（目前为 `1.27.1`）、Git 和 Make：

```sh
git clone https://github.com/soulteary/oc.git
cd oc
make build
./oc --help
```

Go module 路径暂保留 `github.com/soulteary/mc`，以兼容源码引用。构建时克隆 `oc` 仓库，不把历史 module 名称当作安装渠道。[开发指南](docs/zh_CN/development.md)说明测试工具、平台覆盖和集成测试补丁。

## 许可证与归属

OC 使用 [Apache-2.0](LICENSE) 许可证。再分发时保留 [NOTICE](NOTICE)、[CREDITS](CREDITS) 和[内部通知组件的 MIT 许可证](internal/notify/LICENSE)。[通知组件说明](internal/notify/README.md)记录该组件的维护及打包要求。
