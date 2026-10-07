# 开发 OC

[English development guide](../development.md) · [贡献指南](CONTRIBUTING.md)

这份指南用于修改 OC 和复现仓库检查。安装已发布的二进制或镜像见[安装指南](installation.md)；日常操作见[配置](configuration.md)、[使用](usage.md)和[管理](administration.md)指南。

## 工具链与代码布局

Go 版本以 [go.mod](../../go.mod) 为准，当前为 `1.27.1`。[兼容清单](../compatibility.json)记录相同工具链和固定的 OtterIO SDK。CI 和 Makefile 使用 `GOTOOLCHAIN=local`，本机版本过旧时会失败，不会自动下载更新版本；运行检查前先安装要求的工具链。

此外需要 Git 和 Python 3。下面的命令使用 Linux 或 macOS 上的 POSIX shell；Makefile 和交叉编译脚本需要 Bash。服务端集成测试还需要 OpenSSL，竞态测试和 `CGO_ENABLED=1` 需要对应平台的 C 编译器。Windows 的原生构建和测试命令见 [Go 工作流](../../.github/workflows/go.yml)，输出文件应使用 `oc.exe`。

仓库不必放在 GOPATH 下。主要目录如下：

- `main.go`：可执行文件入口。
- `cmd/`：命令、参数、配置、文件系统和 S3 客户端，以及大部分行为测试。
- `pkg/`：客户端辅助包和包测试。
- `internal/notify/`：保留独立 MIT 许可证的通知实现。
- `buildscripts/`：依赖检查、集成环境、发行打包和镜像验证。
- `docs/compatibility.json`：已审查的 SDK、工具链、编译目标、服务端补丁和测试预算。
- `.github/workflows/`：平台检查、CodeQL、发布和稳定版本提升。

二进制名为 `oc`，Go module 路径仍是 `github.com/soulteary/mc`。除非单独讨论迁移，否则保留模块路径和已有版权归属。项目不使用 `govendor`。

## 本地构建与检查

在 Git 工作区中执行：

```sh
export GOTOOLCHAIN=local
go version
go mod download
CGO_ENABLED=0 go build -mod=readonly -tags kqueue -trimpath \
  -ldflags "$(go run buildscripts/gen-ldflags.go)" -o ./oc .
./oc --version
./oc --help
```

元数据辅助程序读取当前提交和提交时间，所以带版本信息的开发构建需要 Git 历史。Unix 上也可以使用 `make build`。开发时直接运行 `GOTOOLCHAIN=local go build -o ./oc .` 能生成二进制，但不会注入上述构建和正式发行使用的版本参数。Makefile 的工具链设置不会配置另一次 shell 调用，直接构建时应显式保留该设置。

修改过程中先运行相关测试。例如，下面选择诊断命令的回归测试，其中一个场景会启动本地 TLS 测试服务：

```sh
go test ./cmd -run 'TestDoctorEffectiveAliasConfiguration|TestDiagnosticEndpointAllowlist' -count=1
```

提交代码前，再运行常规本地检查：

```sh
go test ./...
go vet ./...
go mod verify
PYTHONDONTWRITEBYTECODE=1 python3 buildscripts/test-maintenance.py
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s buildscripts -p 'test_release_*.py' -v
PYTHONDONTWRITEBYTECODE=1 python3 buildscripts/verify-release-boundaries.py
```

用 `gofmt -w PATH/TO/CHANGED.go` 格式化修改过的 Go 文件，再检查 diff。涉及并发、取消、流式处理或文件监听时，还应执行：

```sh
go test -race --timeout 20m ./...
```

CI 使用 golangci-lint `v2.14.0` 和 [.golangci.yml](../../.golangci.yml)。安装相同版本后，可运行 `golangci-lint run --timeout=5m`。`make getdeps` 会安装工具，也可能下载依赖；上面的 Go/Python 检查不要求先运行它。Python 发行测试使用临时样本，不会发布标签、GitHub Release 或镜像。

`make test`、`make check` 和 `make verify` 还会调用继承的 `functional-tests.sh`。没有设置 `SERVER_ENDPOINT` 时，这个脚本默认连接上游公共服务器 `play.min.io`，并执行对象和桶的写入、删除。日常开发使用上面的直接检查和下面的本地临时集成环境。只有检查过脚本的环境变量及清理要求，并显式配置一次性测试目标后，才运行旧套件；不要指向生产部署。`make crosscompile` 检查兼容清单中的 11 个编译目标，不会运行生成的程序。

## 复现服务端集成环境

集成脚本启动临时 OtterIO 进程，使用随机凭据、独立客户端配置、本地端口和临时存储。测试会写入和删除测试对象，并对这些临时进程执行管理操作；脚本不接收已有部署的服务地址。

OC 的 SDK 依赖固定在 `go.mod` 中，服务端必须从该版本源码构建；原有兼容修复已包含在源码中。先把模块源码复制到可写的临时目录，不修改共享 module 缓存。完整源码 SHA 记录在 `docs/compatibility.json` 中。

下面准备与 CI 相同的 `CGO_ENABLED=0` 测试环境。保持在同一个 shell 中执行，以便后续使用这些目录变量：

```sh
export GOTOOLCHAIN=local
export CGO_ENABLED=0
OC_INTEGRATION="$(mktemp -d)"
go mod download
go build -mod=readonly -trimpath -o "$OC_INTEGRATION/oc" .
otterio_source="$(go list -m -f '{{.Dir}}' github.com/soulteary/otterio)"
cp -R "$otterio_source" "$OC_INTEGRATION/otterio-source"
chmod -R u+w "$OC_INTEGRATION/otterio-source"
(
  cd "$OC_INTEGRATION/otterio-source"
  go build -mod=readonly -trimpath -o "$OC_INTEGRATION/otterio" .
)
python3 buildscripts/test-core-integration.py \
  --oc "$OC_INTEGRATION/oc" --otterio "$OC_INTEGRATION/otterio" \
  --report "$OC_INTEGRATION/core-integration.json" \
  --artifacts-dir "$OC_INTEGRATION/core-evidence"
```

默认运行覆盖迁移，以及脚本中的五种 HTTP、TLS 和端口配置。要加入临时四盘存储上的高级操作和 CI 使用的稳定性检查，执行：

```sh
python3 buildscripts/test-core-integration.py \
  --oc "$OC_INTEGRATION/oc" --otterio "$OC_INTEGRATION/otterio" \
  --extended --stability --soak-seconds 30 \
  --report "$OC_INTEGRATION/core-integration.json" \
  --artifacts-dir "$OC_INTEGRATION/core-evidence"
```

这项检查耗时较长，并需要本地磁盘和内存。失败报告包含部分结果；把本次运行作为验收依据前，逐项检查场景状态。先保存供审查的报告和已脱敏的证据，再清理本次创建的临时目录。脚本会回收服务器进程和场景数据，显式指定的报告与证据目录会保留。排查 cgo 相关问题时，使用 `CGO_ENABLED=1` 重新构建并运行。

服务端回归测试的具体命令，以及 Linux/macOS 的 cgo 矩阵，见 [Go 工作流](../../.github/workflows/go.yml)。[兼容说明](compatibility.md)介绍检查能证明的范围和尚未验证的部分。固定源码的通过结果不证明其他 OtterIO 版本已经兼容；旧补丁仅保留为历史 fixture。

## 理解 CI 的覆盖范围

Go 工作流在 Linux、macOS、Windows 上运行单元和竞态测试；Linux 另有 vet、lint 和交叉编译。服务端集成矩阵覆盖 Linux/macOS，并分别启用和关闭 cgo。此外还检查发行边界、编译依赖清单和可达漏洞。[CodeQL](../../.github/workflows/codeql.yml)另行构建 Go 程序进行分析。

获取依赖和漏洞数据需要网络，服务端测试使用本地临时服务。本地通过只说明当前环境的结果，不能代替平台 CI、干净构建的依赖清单或第三方 S3 验收。PR 中应说明相关失败和平台限制。

## 修改依赖、文档与发行内容

有意调整依赖时，一起审查 `go.mod` 和 `go.sum`，必要时同步更新兼容基线及其边界检查，并在 PR 中解释原因。只在依赖图需要变化时执行 `go mod tidy`，随后检查 diff。不要提交临时 `replace`，也不要为了让本地测试通过而悄悄升级固定的 OtterIO SDK。

命令参数、默认值、输出或数据行为变化时，同步更新帮助和中英文指南。迁移要求放在[迁移指南](migration.md)，运行注意事项放在[管理指南](administration.md)，诊断步骤放在[排错指南](troubleshooting.md)。`oc-phase-*` 文档保留设计和验证历史，面向用户的入口是按任务组织的指南。

[发布指南](releasing.md)说明时间戳标签、归档内容、不可覆盖的多平台镜像和 `latest` 提升。发行辅助程序的测试适合本地验证 PR；正式发布需要维护者凭据、已审查说明和同一提交的 main CI，不属于日常开发步骤。[维护者指南](MAINTAINERS.md)介绍审查和发行职责。
