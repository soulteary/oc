# 使用容器运行 OC

[文档目录](README.md) · [English](../containers.md)

发布工作流使用与归档相同的可执行文件构建 Linux amd64、arm64 镜像。GHCR 是主要仓库，Docker Hub 为可选仓库。容器发布功能晚于纯二进制的 `RELEASE.2026-10-07T14-10-00Z` 版本；该旧版本清单没有 `images` 字段，不能用它演示已有镜像。

## 选择确实存在的镜像

从[已发布版本](https://github.com/soulteary/oc/releases)中选择 `release-manifest.json` 包含 `images` 的版本。每一项给出仓库名和已校验的镜像索引摘要。没有镜像条目时，安装该版本的[本机归档](installation.md)，或从源码构建本地镜像，不根据二进制标签猜测镜像地址。

已发布的 [`RELEASE.2026-10-07T17-07-26Z`](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-07T17-07-26Z) 清单记录了 GHCR 镜像，下面用该版本演示 CLI 的容器运行。换用其他版本前先检查其清单。这些镜像不包含实验性的 `oc-console` 或 main 上后续新增的功能。

```sh
TAG=RELEASE.2026-10-07T17-07-26Z
IMAGE="ghcr.io/soulteary/oc:$TAG"
docker run --rm "$IMAGE" --version
docker run --rm "$IMAGE" --help
```

自动化部署需要可重复结果时，将发布清单中同一镜像条目的 `repository` 和 `digest` 字段组合为 `repository@digest`，再将 `IMAGE` 设为这个引用，例如 `ghcr.io/soulteary/oc@sha256:...`。`latest` 跟随完成摘要提升的最新稳定版本，是会移动的别名；首次提升成功前也可能不存在。首次发布 GHCR 包时，需要设置合适的可见性或使用认证拉取，参见[发布配置](releasing.md#容器镜像)。

只有发布时同时配置两项 Docker Hub 凭据，才会生成 `DOCKERHUB_USERNAME/oc`。以清单中的实际仓库列表为准，不假定一定存在 Docker Hub 镜像。

## 持久化客户端配置

镜像入口是 `oc`，镜像名称后直接填写客户端参数。OC 容器不运行存储服务，也不需要发布服务端口。

为别名、证书和客户端状态创建私有宿主目录。发布镜像默认以 root 运行，配置目录为 `/root/.oc`：

```sh
mkdir -p "$HOME/.oc"
chmod 700 "$HOME/.oc"
docker run --rm -it \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  "$IMAGE" alias set store https://s3.example.com \
  --api s3v4 --path on --admin-url https://admin.example.com

docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  "$IMAGE" ls store
```

替换两个地址，并按提示输入部署中的凭据。默认 root 用户在 Linux 宿主上创建的文件可能归 root 所有。与本机 OC 共用配置时检查所有者和权限，不要为了可写而开放整个配置目录。

Linux 下需要非 root 运行时，指定可写目录和对应宿主 UID/GID：

```sh
mkdir -p ./oc-config
chmod 700 ./oc-config
docker run --rm --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$(pwd)/oc-config,dst=/oc-config" \
  "$IMAGE" --config-dir /oc-config --json doctor
```

后续命令沿用该挂载和 `--config-dir /oc-config`。切换用户不会自动修复已有 root 配置的写入权限。

## 挂载数据与证书

宿主路径只有通过挂载才会出现在容器内。从当前目录上传文件时，使用只读挂载：

```sh
docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  --mount "type=bind,src=$(pwd),dst=/work,readonly" \
  "$IMAGE" cp /work/hello.txt store/example/hello.txt
```

下载时，将目标目录挂载为可写：

```sh
mkdir -p ./downloads
docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  --mount "type=bind,src=$(pwd)/downloads,dst=/downloads" \
  "$IMAGE" cp store/example/hello.txt /downloads/hello.txt
```

S3 CA 放在所挂载配置目录的 `certs/CAs/` 下。保存的绝对 `adminCAFile` 路径会在容器内解析：将证书挂载到相同的容器路径，或用 `--admin-ca` 覆盖为实际挂载路径。导入宿主配置不会让任意宿主路径自动可见。详见 [TLS 配置](configuration.md)。

## 连接服务端

容器里的 `127.0.0.1` 指向容器自身。因此，为宿主 OC 配置的地址在 Docker 中可能需要调整。

- 连接其他容器时，让两个容器加入同一个用户定义网络，使用服务名或容器 DNS 名，以及内部 S3/管理端口。
- Docker Desktop 通常提供 `host.docker.internal` 访问宿主服务；服务仍需要监听容器可达的地址。
- Linux Docker Engine 在支持的环境中可添加 `--add-host=host.docker.internal:host-gateway`。这个映射不会让只监听宿主回环地址的服务变得可达。
- Linux 宿主网络模式是另一种选择，但会改变隔离方式，也依赖平台；已有可达地址时不必使用。

HTTPS 地址需要匹配证书中的主机名，网络配置不会改变 TLS 主机名校验。双端口 OtterIO 要同时配置 S3 和管理入口，职责说明见[管理指南](administration.md)。

Docker 的[网络概览](https://docs.docker.com/engine/network/)和 [`--add-host` 说明](https://docs.docker.com/reference/cli/docker/container/run/#add-host)介绍了网络名称及宿主网关映射。

## 在自动化中使用

`-it` 只用于交互配置。脚本不分配终端，检查退出码，并按具体命令的 JSON 结构处理输出：

```sh
docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  "$IMAGE" --json doctor store
```

不要在 issue 中贴出含凭据的完整配置，也不要把凭据写入镜像层。可以通过运行时 `OC_*` 环境变量配置，但能检查容器的管理员也能读取其环境。敏感输出与凭据处理见[安全说明](security.md)。

## 构建本地镜像

在 OC 的 Git 工作区中，准备 Docker Buildx：

```sh
docker buildx build --platform linux/amd64 --load -t oc:local .
docker run --rm oc:local --help
```

ARM64 机器使用 `linux/arm64`。这里的 `--load` 只加载一个平台供本地使用。源码构建采用开发版本元数据，设置镜像标签不会使它成为经过验证的正式发布。

`docker-buildx.sh` 默认构建 amd64/arm64，只有显式添加 `--push` 才会发布。`Dockerfile.release` 需要发布工作流准备的归档构建目录，不是单独从源码构建的入口。发布、版本标签保护和失败恢复见[发布指南](releasing.md)。
