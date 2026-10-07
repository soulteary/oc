# 配置地址、凭据与 TLS

[English](../configuration.md) · [常用操作](usage.md) · [从 mc 迁移](migration.md)

OC 在配置目录的 `config.json` 中保存别名和凭据。即使重命名可执行文件，也使用自己的默认目录，
不会自动读取或修改 mc 配置。需要迁移时，执行[显式导入](migration.md)。

## 选择配置目录

Unix 默认目录是 `$HOME/.oc`，Windows 是 `%USERPROFILE%\oc`。
任务和测试可以使用独立目录，避免影响平时的配置：

```sh
oc --config-dir /path/to/oc-config alias list
OC_CONFIG_DIR=/path/to/oc-config oc --json doctor
```

目录选择顺序为：`--config-dir` / `-C`、`OC_CONFIG_DIR`、`MC_CONFIG_DIR`、平台默认目录。
子命令可能初始化选中的目录，包括显示帮助时。即使可执行文件放在只读位置，正常运行仍需要可写的配置目录。
容器采用相同规则，见[容器使用](containers.md)。

配置目录还包含 `certs/CAs/`、`session/` 中的复制会话、`share/` 中的分享记录，
以及请求分析时使用的 `profile/`。配置和备份应保持私有权限。
`config.json` 含有凭据；普通别名状态与列表会隐藏密钥，但地址和 CA 路径仍可能暴露内部设施信息。

## 添加别名

别名把 S3 地址与凭据关联起来。下面的命令会提示输入 access key 和 secret key：

```sh
oc alias set store https://s3.example.com --api s3v4 --path on
oc alias list store
```

自动化任务使用完整语法 `oc alias set ALIAS URL ACCESSKEY SECRETKEY [FLAGS]`。
通过任务的密钥管理机制传入真实值；命令参数可能出现在历史或进程列表中。
输出隐藏密钥并不意味着配置文件不再敏感，相关要求见[安全说明](security.md)。

地址应指向服务根入口，不填写桶路径或反向代理路径前缀。
`--api s3v4` 明确选择签名方式，并跳过创建别名时的签名探测；保存成功不代表网络可达或账号有权限。
`--path on` 使用路径形式的桶地址，`off` 使用 DNS 形式，默认 `auto` 由客户端选择。
DNS 形式还需要相应的域名解析和 TLS 证书。按部署能力选择，帮助中出现某个提供商示例也不能代替
[针对该服务的验收](compatibility.md)。

不再使用的别名可以通过 `oc alias remove store` 删除。该命令只修改客户端配置，不删除远程桶或对象。

## 临时覆盖别名

可以通过环境变量提供包含凭据的 URL：

```sh
OC_HOST_store='https://ACCESS_KEY:SECRET_KEY@s3.example.com' oc ls store
```

这里的密钥是占位符，真实值应从密钥存储注入。环境解析器有自己的凭据分隔规则；
密钥包含 URL 分隔字符时，优先使用保存的别名，不要假设百分号编码的密钥会被解码。
环境变量同样敏感，不要把完整环境转储贴到 issue 中。

同一个别名的 `OC_HOST_<alias>` 优先于 `MC_HOST_<alias>`，环境别名优先于配置文件中的 S3 地址和凭据。
文件别名保存的独立管理地址与 CA 会继续继承，除非按下一节的规则覆盖。
别名后缀保留大小写，`store` 与 `Store` 对应不同的环境变量名。

环境别名使用 S3v4 和自动桶寻址，不继承文件别名的 `api`、`path` 设置。
需要明确寻址方式时，使用保存的别名。

`OC_REGION`、`OC_ENCRYPT`、`OC_ENCRYPT_KEY`、`OC_PROFILER` 优先于对应的 `MC_*` 名称。
健康诊断还接受 `OC_HEALTH_TEST` / `OC_OBD_TEST`、`OC_HEALTH_DEADLINE` / `OC_OBD_DEADLINE`；
显式健康命令参数优先。通过 OC/MC 环境查询读取的值，即使显式设为空，OC 值仍会阻止旧前缀回退。
想恢复旧配置时，应取消不需要的覆盖变量，而不是依靠空字符串。管理设置只使用非空值，优先级单独见下一节。

`MC_*` 在 OC 0.x 期间继续支持，移除前至少提前一个次版本公告。新任务使用 `OC_*`，
不要引入已弃用的 `MC_HOSTS_*` 格式。

## 配置独立的 OtterIO 管理入口

对象请求继续使用 S3 地址。双端口部署可以在别名中保存管理根地址和独立 CA：

```sh
oc alias set store https://s3.example.com --api s3v4 --path on \
  --admin-url https://admin.example.com --admin-ca /path/to/admin-ca.pem
```

对应字段是 `adminURL`、`adminCAFile`。管理地址和 CA 分别按以下顺序解析：

1. 命令中显式填写的 `--admin-url` 或 `--admin-ca`。
2. `OC_ADMIN_URL_<alias>` 或 `OC_ADMIN_CA_<alias>`。
3. 全局 `OC_ADMIN_URL` 或 `OC_ADMIN_CA`。
4. 别名保存的对应字段。

未设置管理地址时使用 S3 地址；未设置管理 CA 时使用共享信任池，
包括系统根证书和所选配置目录中的额外 CA 文件。
临时覆盖不会改写别名：

```sh
oc --admin-url https://admin.example.com --admin-ca /path/to/admin-ca.pem \
  admin info store
OC_ADMIN_URL_store=https://admin.example.com oc admin info store
```

管理地址只接受 HTTP/HTTPS 根地址，不接受内嵌凭据、查询参数、片段或路径前缀。
管理请求拒绝重定向；反向代理应直接暴露预期的管理根路由。
`alias set` 中的相对 CA 路径会转成绝对路径保存；命令和环境变量中的相对路径按执行时的工作目录解析。

## 信任对应入口的证书

使用公开可信 HTTPS 证书时，通常无需额外设置 CA。
私有 S3 CA 的 PEM 文件应放到所选配置目录的 `certs/CAs/`：

```sh
mkdir -p /path/to/oc-config/certs/CAs
cp /path/to/s3-ca.pem /path/to/oc-config/certs/CAs/s3-ca.pem
oc --config-dir /path/to/oc-config ls store
```

未设置独立管理 CA 时，管理请求与 S3 请求共用信任池，包括 `certs/CAs/` 中的额外文件。
非空的 `--admin-ca` 或同等管理 CA 设置会把管理信任池改为系统根证书加指定 PEM 中的证书，
不再包含那些额外的 S3 CA 文件。S3 请求的信任设置不变。两端使用不同证书时，要分别设置。

TLS 失败时，检查有效期、签发链和主机名。`--insecure` 会关闭证书校验，应修复信任设置，
不要把它长期留在任务中。离线 doctor 的 `certificateVerification: true` 只表示开启了校验，
不能证明服务端证书有效。

```sh
oc --json doctor store
oc --json doctor --online store
```

在线命令只在 15 秒期限内读取管理 ServerInfo，不检查全部 S3 操作和权限。
后续步骤见[排查问题](troubleshooting.md)和[管理指南](administration.md)。
