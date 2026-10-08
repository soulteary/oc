# 发布 OC

[文档目录](README.md) · [安装指南](installation.md) · [维护者流程](MAINTAINERS.md) · [English](../releasing.md)

OC 使用 UTC 的 `RELEASE.YYYY-MM-DDTHH-MM-SSZ` 标签，与 OtterIO 一致。

先合并源码和文档改动，等待 **Go** 和 **Code scanning - action** 在同一个 main 提交上通过，再创建新的标签。PR 检查通过不能代替该提交的 main 检查。

根目录 `RELEASE_NOTES.md` 提供 GitHub Release 正文。带日期的发布准备记录保存在 `docs/releases/`；[2026-10-08 准备记录](../releases/2026-10-08-release-review.md)记录本次源码范围，[2026-10-07 准备记录](../releases/2026-10-07-release-review.md)继续作为历史保存。两份记录都不会为后续发布预留标签。

## 准备标签

在干净的工作区中，准备 Git 和 Python 3：

```sh
git switch main &&
git fetch origin main --tags &&
git pull --ff-only origin main &&
TAG="RELEASE.$(date -u +%Y-%m-%dT%H-%M-%SZ)" &&
python3 buildscripts/release-preflight.py "$TAG" &&
git tag -a "$TAG" -m "OC $TAG" &&
git push origin "refs/tags/$TAG"
```

已配置签名密钥时，将注释标签命令换成 `git tag -s`，推送前用 `git verify-tag` 验证。已经发布或部分使用的标签不能复用、移动或删除。

只读预检会检查标签格式、工作区是否干净、main 是否与 origin/main 同步、本地标签是否被复用，以及发布说明是否非空。它不获取远程状态，也不证明远程 CI 已通过；Release 工作流负责执行这项门槛。不要另外手动创建正式 GitHub Release。

## 发布附件

工作流直接构建 `docs/compatibility.json` 中的 11 个目标：Linux amd64、arm64、arm（GOARM=7）、386、ppc64le、s390x；macOS amd64/arm64；FreeBSD amd64；Windows amd64/arm64。

每个归档包含 `oc` 或 `oc.exe`、LICENSE、NOTICE、CREDITS、通知组件的 MIT 许可证、两种语言的 README 和兼容清单。Windows 使用 ZIP，其余平台使用 tar.gz。

11 个归档加上 `release-manifest.json` 和 `checksums.txt`，共 13 个上传附件。SHA-256 文件覆盖所有归档和发布清单。清单记录标签、源码 SHA、SDK/工具链基线、归档哈希，以及实际发布的容器镜像摘要。程序版本参数来自已验证标签和实际源码提交，不要求 SemVer，也不改变 SDK 依赖。写入镜像摘要后，工作流会重新计算发布清单的校验值。

发布任务拒绝已经正式发布的版本和预发布草稿；它会重新检查远程标签，验证本地校验值，上传到稳定版本草稿，再下载并逐一比较附件名称和字节，最后正式发布。这个阶段不改变 GitHub 的 latest 标记。

新增的 `storage_sdk` 与 `otterio_kits` 分别记录独立 S3 客户端及已发布的 kits；旧字段 `otterio_sdk` 继续记录服务端 / 管理模块身份。

`.goreleaser.yml` 仍可用于本地快照或软件包构建，但发布功能关闭；这个工作流不会调用它。

## 容器镜像

启用容器发布的工作流会生成 `ghcr.io/soulteary/oc:RELEASE.YYYY-MM-DDTHH-MM-SSZ`，支持 `linux/amd64` 和 `linux/arm64`。镜像包含对应归档中的原始可执行文件、CA 证书，以及 LICENSE、NOTICE、CREDITS 和通知组件的 MIT 许可证。

镜像入口是 `oc`，客户端参数直接放在镜像名后。工作流按摘要验证已推送镜像，再正式发布 GitHub Release。较早的纯归档版本，包括 `RELEASE.2026-10-07T14-10-00Z`，清单没有 `images` 字段，不能据此推断镜像存在。按[容器指南](containers.md)选择清单中记录了镜像身份的版本。

如需同时推送 Docker Hub，在仓库 Actions secrets 中同时设置 `DOCKERHUB_USERNAME` 和 `DOCKERHUB_TOKEN`。镜像名为 `DOCKERHUB_USERNAME/oc`，使用相同的时间戳标签和平台。两项都未设置时跳过 Docker Hub；只设置一项会因凭据配置错误导致发布失败。GHCR 使用工作流的 GitHub token，不需要这两项凭据。

首次发布 GHCR 包时，需要在 GitHub 包设置中将 `oc` 的可见性设为 **Public**，才能进行 README 中演示的匿名拉取。新包默认私有，关联公开源码仓库不能代替包可见性设置。详见 [GitHub 容器仓库指南](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry)。

版本镜像标签不可覆盖。推送前，只要任一启用的镜像仓库已存在该版本标签，工作流就会拒绝继续。早先尝试只推送了部分镜像时，按下面的恢复规则处理。

自动推送标签会在 GitHub Release 正式发布后请求更新 `latest`。独立的 **Stable release promotion** 工作流仅在该标签是最新已发布稳定版本时，才把已经验证的镜像摘要提升为 `latest`。

发布任务与整个提升工作流共用 `oc-stable-promotion` 并发组。修改流程时保留这个共享锁；不要把同一锁放到可复用工作流的调用者上，否则调用者持锁时，提升工作流无法获得锁。发布任务必须先结束并释放锁，再请求提升。

手动运行 **Release** 时，`promote_latest` 默认为 `false`；需要同时请求提升时再启用。提升不重新构建程序，也不改变版本标签。所有镜像别名验证通过后，最后一步才将该 GitHub Release 标记为 latest。

需要固定部署结果时，使用时间戳标签，或 `release-manifest.json` 记录的摘要。`latest` 是会移动的别名。下面的占位标签应替换为清单中确实包含镜像的已发布版本：

```sh
TAG="RELEASE.YYYY-MM-DDTHH-MM-SSZ"
docker run --rm "ghcr.io/soulteary/oc:$TAG" --version
docker run --rm -v "$HOME/.oc:/root/.oc" "ghcr.io/soulteary/oc:$TAG" --help
```

挂载 `/root/.oc` 可以在多次运行之间保留别名、证书和其他客户端配置。

## 校验与恢复

将全部 13 个附件下载到同一目录，然后运行：

```sh
shasum -a 256 -c checksums.txt
```

解压本机平台归档，将 `oc --version` 输出的标签与 `release-manifest.json` 核对。清单记录完整源码提交，`--version` 不会显示这个提交值。

容器需要核对镜像仓库摘要与清单是否一致，再按摘要运行 `--version` 和 `--help`，检查显示的发布标签。替换生产程序或镜像前，还应验证普通操作，以及部署中的 TLS、mirror 和保留期设置。

校验文件和发布清单记录身份与完整性，不是签名。交叉编译也不证明每个目标都已经通过运行验收。

如果任务在任何版本镜像推送前失败，先检查草稿，再重新运行原工作流，或在该版本仍为稳定草稿时，用相同的已有标签手动运行 **Release**。多余或过时的草稿附件会导致文件名比较失败；查明原因后，只删除错误的草稿附件，再重试。

一旦任何版本镜像已经推送，等待 main 检查通过后使用新的时间戳标签，即使 GitHub Release 仍是草稿，或另一个镜像仓库的推送失败也一样。镜像检查会有意阻止重试这个部分使用的标签；不要删除或覆盖镜像绕过它。已经正式发布的 GitHub Release 同样不可覆盖。

正式发布成功但 `latest` 提升失败时，保留已发布版本和版本镜像。针对该标签手动运行 **Stable release promotion**，按清单中的摘要重试提升。工作流仍要求它是最新已发布稳定版本，较早版本不能把 `latest` 回退。镜像别名验证通过后，提升也会更新 GitHub latest 标记。

项目没有启用自动自更新渠道。
