# 安装与升级 OC

[文档目录](README.md) · [English](../installation.md)

需要本机可执行文件时下载发布归档，需要隔离运行环境时使用[容器](containers.md)，开发时从源码构建。运行发布二进制不需要安装 Go。目前没有通过 Homebrew、APT 或 RPM 仓库发布 OC 安装包；本地 GoReleaser 配置不代表已有公共包渠道。

## 选择版本和平台

从 [GitHub Releases](https://github.com/soulteary/oc/releases) 选择版本。标签采用 UTC 的 `RELEASE.YYYY-MM-DDTHH-MM-SSZ`，不使用语义化版本号。下面以已发布的 `RELEASE.2026-10-07T17-07-26Z` 演示归档安装；升级时改成需要的已发布标签。归档提供该标签的 CLI，不包含当前 main 的全部功能；实验性的[本机控制台](console.md)需要单独从源码构建。

归档中的平台名为：

- Linux：`linux-amd64`、`linux-arm64`、`linux-arm`（ARMv7）、`linux-386`、`linux-ppc64le`、`linux-s390x`。
- macOS：Intel 使用 `darwin-amd64`，Apple Silicon 使用 `darwin-arm64`。
- FreeBSD：`freebsd-amd64`。
- Windows：`windows-amd64`、`windows-arm64`。

`uname -m` 中的 `x86_64` 对应 amd64，`aarch64` 或 `arm64` 通常对应 arm64。构建目标与已有运行验收范围不同，详见[兼容说明](compatibility.md)。

每个版本包含 11 个归档、`checksums.txt` 和 `release-manifest.json`。Windows 使用 ZIP，其余平台使用 tar.gz。归档附带可执行文件、许可证与归属说明、两种语言的 README 和兼容清单。校验值用于检测损坏及核对附件，不是独立签名；从项目发布页通过 HTTPS 获取归档和校验文件。

## Linux、macOS 和 FreeBSD

以下示例使用 Bash、`curl`、`awk` 和 `tar`。下载前明确设置本机的 `PLATFORM`，不要在其他架构上保留 `linux-amd64`。

```bash
set -euo pipefail
TAG=RELEASE.2026-10-07T17-07-26Z
PLATFORM=linux-amd64
ARCHIVE="oc-$TAG-$PLATFORM.tar.gz"
WORK_DIR="$(mktemp -d)"
BASE_URL="https://github.com/soulteary/oc/releases/download/$TAG"
curl --fail --location "$BASE_URL/$ARCHIVE" --output "$WORK_DIR/$ARCHIVE"
curl --fail --location "$BASE_URL/checksums.txt" --output "$WORK_DIR/checksums.txt"
awk -v name="$ARCHIVE" '$2 == name { print }' "$WORK_DIR/checksums.txt" > "$WORK_DIR/selected-checksum.txt"
test "$(wc -l < "$WORK_DIR/selected-checksum.txt" | tr -d ' ')" = 1
```

解压前验证选中的归档。完整校验文件还包含其他平台附件；没有下载全部文件时，只校验对应行。

Linux 使用：

```sh
(cd "$WORK_DIR" && sha256sum -c selected-checksum.txt)
```

macOS，以及安装了 Perl `shasum` 的 FreeBSD，使用：

```sh
(cd "$WORK_DIR" && shasum -a 256 -c selected-checksum.txt)
```

校验成功后：

```sh
tar -xzf "$WORK_DIR/$ARCHIVE" -C "$WORK_DIR"
mkdir -p "$HOME/.local/bin"
install -m 0755 "$WORK_DIR/oc-$TAG-$PLATFORM/oc" "$HOME/.local/bin/oc"
"$HOME/.local/bin/oc" --version
```

如果 `$HOME/.local/bin` 尚未加入 `PATH`，先在 Shell 配置中添加，再执行 `oc --help`。再分发时保留解压出的许可证和归属说明；安装完成后可以删除临时下载目录。

macOS 下载前设置 `PLATFORM=darwin-arm64` 或 `darwin-amd64`。系统阻止运行下载的程序时，核对来源和校验值后，通过系统的正常批准机制处理，不要全局关闭安全检查。项目没有承诺代码签名或公证。

已安装 GitHub CLI 时，可以改用 `gh release download "$TAG" --repo soulteary/oc --pattern "$ARCHIVE" --pattern checksums.txt --dir "$WORK_DIR"` 下载，之后仍按同样方式校验。

## Windows PowerShell

使用带有 `Invoke-WebRequest`、`Get-FileHash` 和 `Expand-Archive` 的 PowerShell。ARM64 机器将平台改为 `windows-arm64`。

```powershell
$ErrorActionPreference = 'Stop'
$Tag = 'RELEASE.2026-10-07T17-07-26Z'
$Platform = 'windows-amd64'
$Archive = "oc-$Tag-$Platform.zip"
$WorkDir = Join-Path ([IO.Path]::GetTempPath()) ([Guid]::NewGuid().ToString())
New-Item -ItemType Directory -Path $WorkDir | Out-Null
$BaseUrl = "https://github.com/soulteary/oc/releases/download/$Tag"
Invoke-WebRequest "$BaseUrl/$Archive" -OutFile (Join-Path $WorkDir $Archive)
Invoke-WebRequest "$BaseUrl/checksums.txt" -OutFile (Join-Path $WorkDir 'checksums.txt')
$Lines = @(Get-Content (Join-Path $WorkDir 'checksums.txt') | Where-Object {
    $_ -match ('\s{2}' + [Regex]::Escape($Archive) + '$')
})
if ($Lines.Count -ne 1) { throw 'Expected exactly one archive checksum' }
$Expected = ($Lines[0] -split '\s+')[0]
$Actual = (Get-FileHash (Join-Path $WorkDir $Archive) -Algorithm SHA256).Hash.ToLowerInvariant()
if ($Actual -ne $Expected) { throw 'Archive checksum mismatch' }
Expand-Archive -LiteralPath (Join-Path $WorkDir $Archive) -DestinationPath $WorkDir
$InstallDir = Join-Path $HOME 'bin'
New-Item -ItemType Directory -Force -Path $InstallDir | Out-Null
Copy-Item (Join-Path $WorkDir "oc-$Tag-$Platform/oc.exe") (Join-Path $InstallDir 'oc.exe')
& (Join-Path $InstallDir 'oc.exe') --version
```

将 `$InstallDir` 加入用户 `PATH` 后，就可以在其他目录调用 `oc`。默认配置位于用户目录下的 `oc`，不是 `.mc`。覆盖配置目录的方法见[配置指南](configuration.md)。

## 从源码构建

准备 Git，以及 `go.mod` 声明的 Go 工具链（本基线为 `1.27.1`）。Make 和 CI 配置使用 `GOTOOLCHAIN=local`，应先安装所需 Go 版本。安装步骤可参考 [Go 官方说明](https://go.dev/doc/install)。

Unix 环境安装 Make 后：

```sh
git clone https://github.com/soulteary/oc.git
cd oc
make build
./oc --help
```

Unix 环境没有 Make 时，可以直接进行开发构建：

```sh
GOTOOLCHAIN=local go build -trimpath -o oc .
```

Windows PowerShell 使用：

```powershell
$env:GOTOOLCHAIN = 'local'
go build -trimpath -o oc.exe .
```

直接 `go build` 使用开发元数据，`make build` 则根据 Git 提交生成开发元数据；两者都不是正式时间戳发布构建。正式版本标记和干净工作区打包流程见[发布指南](releasing.md)。

仓库名称是 `soulteary/oc`，Go module 路径暂保留 `github.com/soulteary/mc`。不要把历史 module 名称或 MinIO 下载地址作为 OC 安装源。[开发指南](development.md)说明检查方法和集成测试实例。

## 升级与回退

`oc update` 已禁用。下载并校验指定版本，保留已有配置，然后手动替换可执行文件。`oc --version` 显示发布标签，`release-manifest.json` 记录完整源码提交。旧程序未必理解后续配置变化，回退时同时保留合适的配置备份和旧程序。

上线前验证部署中的别名、TLS、复制、mirror 和保留期操作。从 mc 导入时阅读[迁移指南](migration.md)。容器升级则替换固定标签或摘要，并保留配置和数据挂载。
