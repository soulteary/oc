# 使用 OC 管理 OtterIO

[English](../administration.md) · [文档首页](README.md)

`oc admin` 使用 OtterIO 管理 API，需要具有相应权限的凭据。普通 S3 服务不一定提供这套管理接口。操作前先确认部署符合 [兼容性基线](compatibility.md)。

## 配置对象入口和管理入口

别名保存 S3 对象地址和凭据。OtterIO 可以在同一监听地址提供对象与管理接口，也可以使用独立的管理端口。下面的端口仅为示例，应替换为服务端实际配置。

以下命令会交互式询问 access key 和 secret key：

```sh
oc alias set --api s3v4 --path on \
  --admin-url https://admin.example.com:9001 \
  --admin-ca /path/to/admin-ca.pem \
  store https://s3.example.com:9000
```

`ls`、`cp`、`mirror` 等对象命令访问 S3 地址，`admin info` 等管理命令访问 `--admin-url`。未设置管理地址时，OC 回退到 S3 地址；这要求服务端也在该入口提供管理接口。两类请求使用同一个别名中的凭据，但管理操作仍须通过服务端权限检查。

管理地址必须是 HTTP 或 HTTPS 根地址，不接受内嵌凭据、查询参数、片段和路径前缀。使用反向代理时，应在该主机直接暴露 OtterIO 管理路由。OC 拒绝管理重定向，避免转发已签名请求或加密的配置、IAM 正文。

未显式指定管理 CA 时，管理请求使用系统根证书与配置目录 `certs/CAs/` 合成的共享信任池。指定 `--admin-ca` 后，管理请求改用系统根证书加该 PEM 文件，不再包含 `certs/CAs/` 中的额外证书；S3 信任设置不受影响。对象入口也使用私有证书时，将其 CA 放到 `certs/CAs/`。保持证书校验开启，具体说明见 [安全文档](security.md)。

`alias set --admin-ca` 保存绝对路径。临时参数和环境变量中的相对路径按执行时的工作目录解析。管理地址和 CA 分别取以下顺序中第一个非空值：

1. 显式 `--admin-url` 或 `--admin-ca`。
2. 别名环境变量 `OC_ADMIN_URL_store` 或 `OC_ADMIN_CA_store`。
3. 全局环境变量 `OC_ADMIN_URL` 或 `OC_ADMIN_CA`。
4. 别名保存的 `adminURL` 或 `adminCAFile`。
5. 仅管理地址：回退到别名的 S3 地址。

别名后缀区分大小写。临时覆盖不会改写配置文件：

```sh
oc --admin-url https://admin.example.com:9001 \
  --admin-ca /path/to/admin-ca.pem admin info store
```

对象入口的环境变量见 [配置说明](configuration.md)，已有配置的导入方法见 [迁移说明](migration.md)。

## 查看服务状态

先检查诊断信息和服务端信息：

```sh
oc --json doctor store
oc --json doctor --online store
oc admin info store
oc --json admin info store
```

`doctor` 默认离线，输出客户端、Go、SDK 版本、协议、管理入口是否独立、是否设置管理 CA，以及证书校验开关。它不输出入口主机、凭据和配置路径。`--online` 使用 15 秒期限发起只读管理 `ServerInfo` 请求。两种模式都不会验证 S3 读写权限。

`admin info` 展示服务端信息。其 JSON 错误路径返回非零退出码，并包含错误分类，可用时还会提供错误码。脚本应先检查退出码，再解析成功结果；各管理命令的业务结果结构并不完全相同。

生成本地健康报告：

```sh
oc admin subnet health --deadline 5m store
```

命令查询管理入口，将压缩 JSON 报告写入当前目录。检查可能包含磁盘和网络测试，执行时间应与服务运维人员协调。报告可能包含服务配置和敏感环境信息，分享前需要检查。使用 `--json` 时直接输出报告，不生成压缩文件。此模式下应检查报告的 `status`（`Success` 或 `Error`）及 `error`，不能仅凭退出码认定所有健康检查通过。

`subnet` 是保留的历史命令名，目前仅用于本地报告。上传功能已经禁用；显式传入 `--license` 或 `--dev`，包括空值或 false，会在连接服务端之前失败。旧的 `admin health` / `admin obd` 入口只提示改用 `admin subnet health`。

## 管理策略、用户和组

使用满足操作需要的最小权限管理身份。`admin user` 和 `admin group` 修改服务端身份；[客户端配置](configuration.md) 管理本机别名。

先查看已有配置：

```sh
oc admin policy list store
oc admin policy info store archive-reader
oc admin user list store
oc admin user info store analyst
oc admin group list store
oc admin group info store readers
```

下面的策略允许读取已有 `archive` 桶。将其保存为 `archive-reader.json`，并按实际部署调整桶名和操作权限：

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::archive"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": ["arn:aws:s3:::archive/*"]
    }
  ]
}
```

以下命令会修改 IAM 配置。`user add store` 会询问新用户的 access key 和 secret key；后续示例假定新用户的 access key 为 `analyst`：

```sh
oc admin policy add store archive-reader ./archive-reader.json
oc admin user add store
oc admin policy set store archive-reader user=analyst
```

也可以选择通过用户组分配权限：

```sh
oc admin group add store readers analyst
oc admin policy set store archive-reader group=readers
```

`policy set` 替换该用户或组的策略绑定，`policy update` 在已有绑定中追加策略，`policy unset` 移除指定绑定，`policy remove` 删除策略定义。当前版本使用 `add`、`set`、`update`、`unset`；其他客户端的 `create` / `attach` / `detach` 示例不能直接套用，应以本版本 `--help` 为准。

用户还支持 `disable`、`enable`、`remove` 和 `policy`，组支持 `disable`、`enable` 和 `remove`。移除组成员的参数为 `ALIAS GROUP MEMBER...`；省略成员时，请求删除空组。操作能否执行由服务端权限和实际状态决定。变更后先用受限测试身份核对访问权限，再向使用方推广。

## 管理服务账号

服务账号位于 `admin user svcacct`。固定服务端基线会拒绝 root 执行部分服务账号操作，验收使用具有对应管理权限的普通用户。OC 不绕过这项限制。

为 `analyst` 创建服务账号，并用策略文件限制权限：

```sh
oc --json admin user svcacct add --policy ./archive-reader.json store analyst
```

JSON 结果包含生成的 access key 和 secret key。应将其保存到凭据管理系统，避免进入 CI 日志、issue 或聊天记录。显式指定凭据时，必须同时提供 `--access-key` 和 `--secret-key`；当前创建限制分别为 3–20 字节和 8–40 字节。命令行参数中的凭据可能被 shell 历史或进程列表记录。

使用服务账号的 access key 查询和调整状态：

```sh
oc admin user svcacct ls store analyst
oc admin user svcacct info store SERVICE_ACCESS_KEY
oc admin user svcacct disable store SERVICE_ACCESS_KEY
oc admin user svcacct enable store SERVICE_ACCESS_KEY
```

`svcacct set` 修改策略或 secret key；非空的新 secret 至少需要 8 字节。`svcacct rm ALIAS SERVICE_ACCESS_KEY` 删除账号。轮换密钥时，应与使用方协调，先核对新凭据可用，再确认旧凭据已被拒绝。可选参数见 `oc admin user svcacct set --help`。

## 指标、服务配置和服务控制

为对象监听入口生成 Prometheus 配置：

```sh
oc admin prometheus generate store
oc admin prometheus generate --metrics-type node store
oc admin prometheus generate --metrics-type legacy store
oc admin prometheus generate --metrics-ca /etc/prometheus/s3-ca.pem store
```

cluster、node、legacy 的路径分别为 `/otterio/v2/metrics/cluster`、`/otterio/v2/metrics/node`、`/otterio/prometheus/metrics`。OC 通过管理入口查询服务信息，但生成的抓取目标使用 S3 入口。`--metrics-ca` 指向 Prometheus 主机上的 S3 CA 文件，仅用于 HTTPS。只有服务端明确启用了公开指标时，才使用 `--public` 省略认证。默认配置包含基于静态凭据生成的 bearer token；临时会话凭据不能生成长期指标令牌。生成的配置应妥善保管。

`admin config` 的 `get`、`set`、`export`、`import`、`reset`、`history`、`restore` 操作服务端配置。导出内容可能包含密钥。修改前审核内容、保留受保护的备份，并遵循服务端的重启要求。`oc config import` 则导入客户端版本 10 的别名配置，两者用途不同。

服务控制影响目标部署，可能中断正在使用它的应用：

```sh
oc admin service restart --timeout 2m store
```

默认等待期限为一分钟。OC 只发送一次重启请求，随后等待原有全部节点恢复且启动时间发生变化。超时表示尚未确认恢复，不会再次发送重启。`oc admin service stop store` 停止服务，之后需要通过服务端进程管理方式重新启动。

## 确认能力和部署前提

CLI 还提供 `admin trace`、`admin console`、`admin profile`、`admin heal`、`admin top`、`admin kms` 和 `admin bucket`。当前帮助已将 `admin heal` 及其旧参数标为弃用。帮助中存在命令，不代表所有服务端或部署都支持它。例如，分布式锁查询需要分布式部署，KMS 操作需要配置外部 KMS，通知目标和复制需要相应的服务端配置及可访问的外部服务。

已有验收覆盖固定的单节点 OtterIO 基线上的部分操作；其源码已包含原有兼容修复，不需要另外应用补丁。heal 状态检查不能证明故障磁盘恢复；生命周期配置往返不能证明实际到期删除；实时事件流不能证明外部通知投递。边界见 [兼容性说明](compatibility.md)，不支持的操作和权限错误可按 [故障排查](troubleshooting.md) 处理。
