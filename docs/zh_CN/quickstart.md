# 在本机试用 OC 与 OtterIO

[文档目录](README.md) · [English](../quickstart.md)

本示例用 Docker 运行单节点测试服务，再用本机 OC 创建桶、上传和下载文件、查询服务信息。需要先[安装 OC](installation.md)，并准备 Docker、OpenSSL、`curl` 和 POSIX shell。9000、9001 端口应未被占用。OC 是客户端，安装它不会启动 OtterIO 服务。

已有部署时，跳过服务端启动步骤，按[配置指南](configuration.md)使用实际地址和凭据。服务端[快速开始](https://github.com/soulteary/otterio/blob/main/README_zh_CN.md#快速开始)和 [Docker 安全指南](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md)提供更详细的部署说明。

## 启动测试服务

在同一个 shell 中执行下面的命令。凭据只生成一次，妥善保存，连接和重启时沿用这些值：

```sh
export OTTERIO_ROOT_USER=otterio-admin
OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
export OTTERIO_ROOT_PASSWORD

docker run --detach --name otterio-oc-example \
  -p 127.0.0.1:9000:9000 -p 127.0.0.1:9001:9001 \
  -e OTTERIO_ROOT_USER -e OTTERIO_ROOT_PASSWORD \
  -v otterio-oc-example-data:/data \
  soulteary/otterio:latest server --console-address ":9001" /data
docker logs otterio-oc-example
curl --fail http://127.0.0.1:9000/otterio/health/ready
```

等待启动完成后再发送就绪请求。失败时查看日志，服务就绪后重试。HTTP 端口只绑定宿主回环地址，供本机评估；命名 Docker 卷会在容器停止后保留数据。`latest` 是会变化的服务端镜像标签，需要可重复部署时选择已审查的版本标签或摘要。本示例不表示 OC 的[已有兼容验收](compatibility.md)覆盖该镜像的所有版本。

9000 提供 S3 API，9001 提供 Web 控制台和管理 API。管理根地址为 `http://127.0.0.1:9001`，不附加控制台的 `/otterio/` 页面路径。单监听入口的服务端同时省略 `--console-address` 和客户端的 `--admin-url`。

## 配置 OC 并核对传输结果

继续使用同一个 shell，让 OC 复用启动服务时传入的凭据。示例使用独立的客户端配置目录：

```sh
mkdir -p ./oc-example-config
chmod 700 ./oc-example-config
export OC_CONFIG_DIR="$PWD/oc-example-config"

oc --version
oc alias set store http://127.0.0.1:9000 \
  "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/oc-example
printf 'Hello from OC\n' > hello.txt
oc cp hello.txt store/oc-example/hello.txt
oc stat store/oc-example/hello.txt
oc cp store/oc-example/hello.txt downloaded.txt
cmp hello.txt downloaded.txt
oc admin info store
```

下载内容一致时，`cmp` 成功退出。显式设置签名方式后，`alias set` 成功只表示已保存配置，后续 S3 和管理命令才核对实际访问。桶已存在时，再次执行 `mb` 可能报告桶已存在。

同 shell 的别名命令不会把凭据明文写进 shell 历史，但展开后的凭据仍可能出现在进程参数中。共享机器上省略两个凭据参数，按 OC 提示输入已保存的凭据。两种方式都会在 `config.json` 中保存凭据，应保护示例配置目录。配置客户端别名不会创建服务端账号。日常应用使用受限身份，不向使用方分发这个测试服务的 root 凭据。

## 停止示例并继续使用

停止测试服务，并清除示例中的 shell 覆盖值：

```sh
docker stop otterio-oc-example
unset OC_CONFIG_DIR OTTERIO_ROOT_USER OTTERIO_ROOT_PASSWORD
```

容器、命名卷、本地文件和示例配置仍会保留。准备通过 `docker start otterio-oc-example` 重启时，应保留已保存的凭据；重新生成 shell 密码不会改变已有容器的凭据。按是否还需要这些数据，决定保留或移除示例资源。

后续阅读[复制与同步](usage.md)、[TLS 与入口配置](configuration.md)或 [OtterIO 管理](administration.md)。Go 应用调用 S3 时使用 [OtterIO SDK](https://github.com/soulteary/otterio-sdk)；SDK、OC 和服务端各自发布版本。
