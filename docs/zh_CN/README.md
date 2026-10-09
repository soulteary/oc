# OC 文档

[English](../README.md) · [项目介绍](../../README_zh_CN.md)

第一次使用时，按安装、配置别名、传输文件的顺序阅读。使用指南说明当前 OC 的行为；文末的阶段记录保留编写时的代码背景和测试结果。

## 开始使用

1. [安装与升级](installation.md)：发布归档、校验、Windows 和源码构建。
2. [在本机试用 OC 与 OtterIO](quickstart.md)：启动测试服务、复用启动凭据，并验证 S3 和管理操作。
3. [容器使用](containers.md)：配置和数据挂载、网络入口及版本固定。
4. [配置 OC](configuration.md)：别名、环境变量、S3/管理地址和 TLS。
5. [传输与同步](usage.md)：本地文件、S3 对象、mirror 行为和自动化。
6. [从 mc 迁移](migration.md)：版本 10 导入、备份和证书迁移。

## 运行与支持

- [命令索引](commands.md)：命令分类和参数查询方法。
- [OtterIO 管理](administration.md)：服务信息、身份、策略与诊断。
- [本机 Web 控制台](console.md)：可选源码构建、单别名、对象写入、受保护桶配置与自身 IAM 改密。
- [控制台五项功能补全](../console-features.md)：存储桶、IAM、历史版本、ZIP 和预签名分享。
- [生命周期转换与恢复](../lifecycle-transition.md)：CLI 目标配置、服务端协议要求、执行及回退边界。
- [兼容与验证范围](compatibility.md)：SDK 基线、部署覆盖和已知边界。
- [故障排查](troubleshooting.md)：认证、TLS、地址、流式命令与导入问题。
- [安全说明](security.md)：凭据保护、发布信任与私下报告漏洞。
- [发布说明](../../RELEASE_NOTES.md)与[已发布版本](https://github.com/soulteary/oc/releases)。

## 开发与维护

- [参与贡献](CONTRIBUTING.md)、[开发与测试](development.md)和[行为准则](../../code_of_conduct.md)。
- [维护者流程](MAINTAINERS.md)与[发布流程](releasing.md)。
- [文档维护](documentation.md)：内容组织、示例约定及本次参考的开源实践。
- [命名和 module 兼容](../../CONFLICT.md)。
- [内部通知组件](../../internal/notify/README.md)。

## 相关项目

- [OtterIO](https://github.com/soulteary/otterio)：存储服务端、部署步骤及服务端配置文档。
- [OtterIO SDK](https://github.com/soulteary/otterio-sdk)：供 Go 应用集成的独立 S3 客户端。OC 分别固定 SDK 与服务端/管理模块版本，见[兼容说明](compatibility.md)。

## 实现与验证记录

这些记录保留原来的源码范围、限制和测试结果。安装及日常操作以当前指南为准；早期阶段中的“尚未实现”或“尚未验证”不表示项目现在的状态。

- [2026-10-08 项目现状复查](../reviews/2026-10-08-project-status.md)：当前源码与发行边界、文档问题及剩余工作。
- [阶段一：构建与发布边界](../oc-phase-one.md)。
- [Web 控制台迁移计划](../console-migration.md)：分阶段实施、权限差异与旧入口移除门槛。
- [控制台第一阶段实现与验证](../console-phase-one.md)：已交付功能、服务端修复、实际验收与剩余限制。
- [控制台第二阶段实现与复查](../console-phase-two.md)：对象写操作、交叉审查修复和实际验证。
- [控制台第三阶段实现与验证](../console-phase-three.md)：桶配置、自身 IAM 改密及历史验收，后续转换另有验证记录。
- [生命周期转换验证记录](../lifecycle-transition-verification.json)：当前可选服务端补丁、运行时验收及 P2/P3 回归。
- [阶段二：核心接入](../oc-phase-two.md)。
- [阶段三：常用功能与迁移](../oc-phase-three.md)及[结果记录](../oc-phase-three-results.json)。
- [阶段四：稳定性与兼容边界](../oc-phase-four.md)、[结果记录](../oc-phase-four-results.json)和[开发构建依赖清单](../oc-phase-four.cdx.json)。
- [2026-10-07 发布准备记录](../releases/2026-10-07-release-review.md)。
- [2026-10-08 发布准备记录](../releases/2026-10-08-release-review.md)：控制台、CLI 与独立发布的 SDK/kits。

机器可读的[兼容清单](../compatibility.json)记录工具链、SDK 基线、目标平台和测试预算。检查数量和依赖清单只描述各自的运行结果，不是发布签名，也不能保证所有部署都能通过验收。

旧 `minio-*-guide` 路径继续作为导航页保留。OC 的安装与支持入口以这里的说明为准。
