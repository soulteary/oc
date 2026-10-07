# 参与 OC 开发

[English contribution guide](../../CONTRIBUTING.md)

可以通过代码、测试、文档、诊断工具或可复现的问题报告参与项目。这个仓库维护 OC 命令行客户端，OtterIO 是对应的服务端项目。[开发指南](development.md)介绍代码布局、本地检查和服务端测试环境。

## 选择要做的工作

小修复和文档纠正可以直接向 `main` 提交范围明确的 Pull Request。新增命令、调整依赖，或改变涉及存储数据的行为，先在 [OC Issues](https://github.com/soulteary/oc/issues) 讨论范围和兼容影响。开始前检查已有 issue 和 PR，避免重复工作。

安装和使用问题先查阅[安装](installation.md)、[配置](configuration.md)、[使用](usage.md)和[排错](troubleshooting.md)指南。仍无法解决时，在 issue 中说明命令和运行环境。安全问题按[安全报告说明](security.md)处理，不要在公开 issue 中提交凭据或尚未评估的利用细节。

参与项目时请遵守[行为准则](../../code_of_conduct.md)。其中的执行条款说明目前已公开的联系途径；不要在公开讨论中披露私密事件细节。

## 提交可复现的问题

报告中请提供：

- `oc --version` 输出、操作系统和架构，以及安装方式。
- 实际命令、参数、预期结果和实际结果；与问题有关时附上退出状态。
- 使用临时文件、对象或账号的最小复现。说明哪一步会写入或删除数据。
- 服务端产品和版本、S3 与管理地址是否分离，以及是否使用 TLS 或代理。
- 脱敏后的错误信息，或 `oc --json doctor`、`oc --json doctor ALIAS` 输出。诊断默认离线；添加 `--online` 才会请求管理接口。

分享前移除 access key、secret key、session token、签名 URL 和私有配置。日志和命令追踪可能包含离线 doctor 输出没有的敏感信息。能够编译，或上游存在同名命令，都不能证明兼容；具体范围见[兼容说明](compatibility.md)。

## 在分支上修改

Fork [soulteary/oc](https://github.com/soulteary/oc/fork)，把自己的仓库克隆到方便的目录，从最新 `main` 创建分支。项目使用 Go modules，不要求放在 `$GOPATH/src` 下。

```sh
git clone https://github.com/YOUR-USERNAME/oc.git
cd oc
git switch -c fix/describe-the-change
go version
go mod download
```

工具链以 `go.mod` 为准，具体命令见[本地构建与检查](development.md#本地构建与检查)。可执行文件叫 `oc`；Go module 路径仍为 `github.com/soulteary/mc`，以保留源码兼容性。不要在无关修复中批量更改 import 路径。

## 准备 Pull Request

一个 PR 集中处理一个问题及其必要改动。说明问题如何触发、修改后的行为，以及迁移或数据影响。行为修复应补充回归测试；纠正错别字不需要代码测试。

- 用 `gofmt` 格式化修改过的 Go 文件，运行相关包的测试。
- 按开发指南检查依赖和发行边界。列出执行的命令和结果，也说明无法运行的检查。
- 同步修改受影响的帮助文本和中英文文档。暂时缺少译文时，在 PR 中说明。
- 保留源码版权声明、[LICENSE](../../LICENSE)、[NOTICE](../../NOTICE)、[CREDITS](../../CREDITS) 和通知库的 MIT 许可证。保留上游归属不表示获得 MinIO, Inc. 的认可。
- 解释有意调整的依赖。避免批量升级、临时 `replace` 和无关的 `go.sum` 改动；CI 会检查固定的 OtterIO 基线。
- 关联已有 issue，说明尚未验证的平台或服务端行为。

提交应围绕具体改动组织，方便审查；讨论前不要求把整个 PR 强制压成一个 commit。根据审查意见修改实现，或解释保留原方案的原因；后续编辑后重新运行受影响的检查。测试样本不得包含凭据和私有生产数据。

[Go 工作流](../../.github/workflows/go.yml)和 [CodeQL 工作流](../../.github/workflows/codeql.yml)定义仓库检查。本地测试只覆盖当前机器，不能代替 Linux、macOS、Windows 和服务端集成矩阵。只改文档的 PR 也应核对链接、示例和实现是否一致。

## 发布与支持

维护者通过[发布流程](releasing.md)发布时间戳标签。验证 PR 不需要创建标签、推送容器镜像或配置镜像仓库凭据。发行辅助脚本的回归测试是离线检查；正式发布和 `latest` 提升是独立操作。

涉及已有用户的改动应对照[迁移](migration.md)、[管理](administration.md)和[兼容说明](compatibility.md)。问题和改进建议提交到本仓库；响应时间取决于维护者的时间安排，文档不承诺支持 SLA。
