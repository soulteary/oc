# 复制、镜像同步与检查数据

[English](../usage.md) · [安装 OC](installation.md) · [配置](configuration.md)

OC 可以操作本地路径，也可以操作 `别名/桶/对象` 路径。下面使用 `store` 别名和测试桶；
执行远程命令前，先按[配置指南](configuration.md)设置地址和凭据。
对象操作需要服务端支持相应的 S3 API，并授予账号所需权限。
OtterIO 管理命令单独见[管理指南](administration.md)。

## 先操作本地文件

本地操作不需要存储服务，也不需要配置别名：

```sh
mkdir -p ./oc-example/source ./oc-example/download
printf 'hello OC\n' > ./oc-example/source/hello.txt
oc ls ./oc-example/source/
oc cp ./oc-example/source/hello.txt ./oc-example/download/hello.txt
oc stat ./oc-example/download/hello.txt
oc cat ./oc-example/download/hello.txt
```

本地目录名可能与别名重复时，使用 `./` 或绝对路径。包含空格的路径需要加引号。
指南中的命令采用 POSIX Shell 语法；使用 PowerShell 时，按其规则调整路径和环境变量赋值。

## 连接服务并复制对象

本地 OtterIO 使用 9000 和 9001 两个端口时，可以这样保存别名。
命令会提示输入 access key 和 secret key，避免把密钥直接写进命令历史：

```sh
oc alias set store http://127.0.0.1:9000 \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/oc-example
oc cp ./oc-example/source/hello.txt store/oc-example/hello.txt
oc stat store/oc-example/hello.txt
oc cp store/oc-example/hello.txt ./oc-example/download/from-store.txt
```

这里的 HTTP 地址用于本地测试。部署环境使用 HTTPS，并按[TLS 配置](configuration.md#信任对应入口的证书)
设置 CA。单端口 OtterIO 省略 `--admin-url`。

复制整个目录或对象前缀时，加上 `--recursive`：

```sh
oc cp --recursive ./oc-example/source/ store/oc-example/upload/
oc cp --recursive store/oc-example/upload/ ./oc-example/download/
oc ls --recursive store/oc-example/upload/
```

`cp` 复制选中的对象，不删除目标中多出的对象。上传到已有对象名时，可能替换当前内容；
桶开启版本控制后，也可能产生新版本。需要保留本地旧文件时，下载到单独的目录。
下载后比较内容或哈希，确认数据完整；ETag 不能一律当作内容校验和。

## 镜像同步目录或前缀

`mirror SOURCE TARGET` 递归处理目录或对象前缀，比较可用的大小、时间和元数据。
普通同步不会完整读取每个未变化对象的内容。目标缺失的对象会被复制；目标已有对象与源不同时，
需要 `--overwrite`，否则 OC 报告冲突。

先预览，再执行单向同步：

```sh
oc mirror --fake --overwrite ./oc-example/source/ store/oc-example/mirror/
oc mirror --overwrite ./oc-example/source/ store/oc-example/mirror/
```

`--fake` 列出候选工作，不传输或删除数据。它可能在未指定 `--remove` 时也列出目标多余对象的删除候选；
实际删除仍需要 `--remove`。预览不会锁定两端状态；源或目标发生变化后，需要重新检查。

目标中多出的对象默认保留。**`--remove` 会删除目标数据。** 建议使用单独的目标前缀，
并带上计划执行的全部选项预览：

```sh
oc mirror --fake --overwrite --remove \
  --exclude '*.tmp' ./oc-example/source/ store/oc-example/mirror/
oc mirror --overwrite --remove \
  --exclude '*.tmp' ./oc-example/source/ store/oc-example/mirror/
```

为 `--exclude` 模式加引号，防止 Shell 提前展开。多个模式可以重复指定。
模式匹配相对于同步根目录的名称；匹配的源和目标条目都不参与比较，因此也能保护目标中的对应条目免于删除。

按时间选择源对象：

```sh
oc mirror --overwrite --newer-than 7d ./oc-example/source/ store/oc-example/recent/
oc cp --recursive --older-than 30d store/oc-example/upload/ ./oc-example/older/
```

时间筛选使用源的修改时间。`--newer-than 7d` 选择不足七天的源对象，
`--older-than 30d` 选择至少三十天的源对象，也接受 `7d10h30m` 这样的组合。
**时间筛选不会保护仅存在于目标中的对象免于 `mirror --remove` 删除。**
需要限定删除范围时，使用排除模式或独立的目标前缀。

## 跟随变化

监听桶或本地目录：

```sh
oc watch --events put,delete store/oc-example
oc watch --recursive ./oc-example/source/
```

通知没有持久化重放游标。连接中断、重启或监听队列过载都可能丢失事件。
独立的 `watch` 或 `find --watch` 遇到订阅意外关闭、本地事件丢失时，会报告错误并返回非零退出码。
重新启动命令不能补回缺失的历史；需要完整审计记录时，应使用持久化审计系统。

本地源的单向持续同步可以重新核对当前状态：

```sh
oc mirror --watch --overwrite --watch-rescan-interval 1m \
  ./oc-example/source/ store/oc-example/live/
```

本地源默认每分钟重扫；收到事件丢失或订阅意外关闭的错误后，会重新注册监听并核对两端状态。
恢复轮次可能重传源中现存的文件，包括大小和时间相同的文件。常规重扫只比较元数据。

需要检查未变化对象的内容时，加上 `--watch-verify-interval 1h`。
到期后，下一次重扫会流式读取两端数据并比较 SHA-256，因此需要目标读取权限，也会增加流量。
深度校验默认是 `0`，表示关闭；开启后的校验周期和重扫周期都不能小于 `1s`。
删除仍需要 `--remove`，筛选规则继续生效，时间筛选的删除限制见上一节。

重新核对只能恢复当前状态，不能恢复观察间隙中创建后又删除的文件。
上述恢复行为针对本地源，不表示远程 S3 事件流或 active-active 部署具有持久化事件保证。

## 使用版本控制与保留期

版本控制和对象锁需要服务端支持及相应权限，不适用于本地文件系统。
先在支持这些功能的临时环境验证，再给生产数据设置策略：

```sh
oc version enable store/oc-example
oc version info store/oc-example
oc ls --versions store/oc-example
```

对象保留期还需要开启对象锁的桶，普通版本控制不能代替这个条件：

```sh
oc mb --with-lock store/oc-locked-example
oc cp --retention-mode governance --retention-duration 1d \
  ./oc-example/source/hello.txt store/oc-locked-example/hello.txt
oc retention info store/oc-locked-example/hello.txt
oc retention set --default governance 30d store/oc-locked-example
```

保留期限使用正整数天数或年数，例如 `1d`、`1y`；非法或超出可表示范围的日期会被拒绝。
桶默认策略用于后续符合条件的写入，不会自动给全部既有版本补上保留期。
保留期保护符合条件的对象版本，可能阻止覆盖或删除该版本；向同一个 key 上传仍可能创建新的当前版本。
governance 与 compliance 的限制由服务端执行。
使用前查看[兼容范围](compatibility.md)中验证过的部署和操作；这里不提供 bypass 或破坏性清理示例。

## 在脚本中处理输出

```sh
oc --json ls store/oc-example > objects.jsonl
oc --json doctor store > doctor.json
```

`--json` 用于机器可读输出。普通操作结果通常逐行输出 JSON；别名记录、显式导出等命令可能采用多行格式。
按实际命令解析，不要假设所有命令共享一种成功结构。统一错误包含 `status`、`error.message`、
`error.cause`、`error.category`，已知错误还会包含 `error.code`。缺少 code 不表示操作成功。
解析 JSON 时，不要混入 `--debug` 输出。

同时检查退出码：正常成功是 `0`，普通错误是 `1`；收到 SIGINT、SIGTERM 后通常分别返回 `130`、`143`。
强制终止或清理失败可能返回其他非零状态。在执行下一条 Shell 命令前保存退出码。
诊断方法和反馈所需信息见[排查问题](troubleshooting.md)。
