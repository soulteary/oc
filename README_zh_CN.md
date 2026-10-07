# OC 客户端

OC 是 OtterIO 的命令行客户端，提供文件系统与 S3 对象操作，以及 OtterIO 专属管理命令。项目继承 MinIO Client 的 Apache 2.0 代码；原始归属信息保留在 LICENSE、NOTICE 和源文件中。

发布步骤与附件校验见[OC 发布指南](docs/releasing.md)。

## 构建

使用 `go.mod` 指定的 Go 工具链：

```sh
make build
./oc --help
```

Go module 路径暂保留 `github.com/soulteary/mc`，避免破坏既有源码引用。自更新和 MinIO SUBNET 上传已禁用。安装和发行边界见 [阶段一](docs/oc-phase-one.md)。

## 连接 OtterIO

```sh
oc alias set store http://127.0.0.1:9000 ACCESS_KEY SECRET_KEY \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/example
oc cp ./file.txt store/example/file.txt
oc admin info store
```

单端口部署省略 `--admin-url`。独立管理证书使用 `--admin-ca /path/to/ca.pem`；S3 额外 CA 放在 OC 配置目录的 `certs/CAs/`。详细说明见 [阶段二](docs/oc-phase-two.md)。

## 从 mc 迁移

OC 固定使用 `~/.oc`，Windows 使用用户目录下的 `oc`，不随二进制文件名变化。默认不会读取或修改 `~/.mc`。

```sh
oc config import ~/.mc/config.json
```

显式导入支持版本 10 配置，整体替换目标别名，并以私有权限备份原 OC 配置。源文件不变；相对管理 CA 路径转成相对于源配置目录的绝对路径。旧版配置先由原客户端迁移至版本 10。证书目录、会话和分享记录不自动复制；需要的 S3 CA 请单独复制到 OC 配置目录。

```sh
OC_CONFIG_DIR=/path/to/oc-config oc ls store
oc --config-dir /path/to/oc-config ls store
OC_HOST_store=http://ACCESS_KEY:SECRET_KEY@127.0.0.1:9000 oc ls store
```

`OC_*` 优先于对应 `MC_*`；`MC_*` 在整个 OC 0.x 系列保留，停止支持前至少提前一个次版本公告。显式 `--config-dir` 优先于环境变量，环境别名优先于文件别名。支持 `OC_HOST_<alias>`、`OC_REGION`、`OC_ENCRYPT`、`OC_ENCRYPT_KEY`、`OC_PROFILER` 和健康检查环境变量。配置目录环境变量兼容 `MC_CONFIG_DIR`。管理地址与 CA 使用独立的 `OC_ADMIN_*` 设置。

## 兼容范围与验证

[阶段三](docs/oc-phase-three.md) 记录管理、版本、生命周期、对象锁等功能的部署前提及测试结果。不能把普通服务器、单节点纠删码、分布式和网关模式的功能等同；KMS、通知目标及复制还需要对应服务端配置。

```sh
python3 buildscripts/test-core-integration.py \
  --oc /path/to/oc --otterio /path/to/otterio --extended \
  --report /tmp/oc-compatibility.json
```

脚本只使用临时实例和临时凭据；固定服务端版本需要 [兼容清单](docs/compatibility.json) 中的三项补丁，包含参数桥接、关闭与重启，以及 HTTP/对象路径修正。Darwin 的监督重启机制和完整平台验证记录见阶段三文档。输出 `--json` 的错误保留 `status`、`error.message`、`error.cause` 等字段，并新增 `error.code` 与 `error.category`，错误对象单行输出。错误退出码为 1，取消及信号退出码沿用既有约定。

## 稳定性与诊断

使用 `oc --json doctor [别名]` 查看不含凭据的离线诊断，添加 `--online` 进行只读管理连接检查。详见[阶段四稳定性与兼容边界](docs/oc-phase-four.md)，包括故障注入、内存预算、平台覆盖及二进制依赖清单。SDK 改造暂缓。
