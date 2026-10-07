---
name: Bug report / 问题报告
about: Report a reproducible OC problem / 提交可复现的 OC 问题
title: ''
labels: ''
assignees: ''
---

Before reporting, read the [troubleshooting guide](https://github.com/soulteary/oc/blob/main/docs/troubleshooting.md). For vulnerabilities, use [private reporting](https://github.com/soulteary/oc/security/advisories/new).

提交前请阅读[故障排查指南](https://github.com/soulteary/oc/blob/main/docs/zh_CN/troubleshooting.md)。安全漏洞请使用[私下报告入口](https://github.com/soulteary/oc/security/advisories/new)。

## Expected behavior / 预期行为

## Actual behavior / 实际行为

## Minimal reproduction / 最小复现步骤

Include the command with credentials removed, sample input and relevant flags. Explain whether the source and destination are local files or S3 paths.

请提供移除凭据后的命令、示例输入和相关参数，注明源与目标是本地文件还是 S3 路径。

## Versions and platform / 版本与平台

- `oc --version`:
- OS and architecture / 系统与架构:
- Server version and deployment / 服务端版本与部署方式:
- Installation method / 安装方式: archive / container / source
- Container tag or digest, if used / 使用容器时的标签或摘要:

## Diagnostics / 诊断

Include the relevant output of `oc --json doctor ALIAS` if an alias is involved. `--online` contacts the management endpoint; use it only when appropriate. Review all output before posting. Do not attach credentials, complete configuration exports, secret keys, signed URLs or unreviewed health/debug reports.

涉及别名时，可附上 `oc --json doctor ALIAS` 的相关输出。`--online` 会连接管理入口，请按需要使用。发布前检查所有内容，不上传凭据、完整配置导出、secret key、签名 URL 或未检查的健康与调试报告。

## Scope and workarounds / 影响范围与临时处理
