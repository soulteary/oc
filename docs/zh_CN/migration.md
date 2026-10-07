# 从 mc 配置迁移到 OC

[English](../migration.md) · [配置](configuration.md) · [兼容范围](compatibility.md)

先并行安装 OC 和原客户端。OC 在 Unix 使用 `.oc`，Windows 使用用户目录下的 `oc`，
不会自动加载 `.mc`，也不会修改 mc 文件。重命名可执行文件不会改变这个默认目录。

## 检查并备份源配置

导入接受含有 `"version": "10"` 和 `aliases` 对象的 JSON 配置。
旧格式先使用原客户端升级。保留一份私有的旧配置和证书目录备份；源文件和备份都含有密钥。

第一次迁移使用独立的目标目录：

```sh
oc --config-dir /path/to/oc-migration config import /path/to/mc/config.json
oc --config-dir /path/to/oc-migration alias list
oc --config-dir /path/to/oc-migration --json doctor store
```

Unix 常见的源文件是 `$HOME/.mc/config.json`，执行时按实际位置填写。
诊断命令中的 `store` 必须存在于导入文件中。别名列表会隐藏密钥，但地址和 CA 路径仍可能属于私有信息，
应在本机查看。

## 确认导入会替换哪些内容

`oc config import SOURCE` 在写入前检查 JSON、版本、别名、S3 和管理地址，以及桶寻址设置。
**导入会整体替换目标别名集合，不会合并既有别名。** 需要保留的别名应先完成整理，再导入日常使用的目录。

源文件保持不变。目标已有 `config.json` 时，OC 会把原始内容备份到同目录的
`config.json.backup-<UTC 时间戳>`。Unix 新配置与备份使用 `0600` 权限；
Windows 需要保持目录仅允许客户端账号访问。备份同步落盘后，
再写入同目录临时文件、同步并替换目标。旧备份不会被自动删除。

源与目标指向同一文件、JSON 损坏、版本不支持、缺少别名数据或地址非法时，导入会被拒绝。
源配置中的 S3 地址应是 HTTP/HTTPS 根地址，不能包含内嵌凭据、查询参数、片段或路径前缀；
凭据放在别名字段中。验证失败后，不应使用部分解析结果手工覆盖目标。

## 迁移证书并检查管理入口

导入包含别名数据，以及可选的 `adminURL`、`adminCAFile`。
相对管理 CA 路径会按**源配置文件所在目录**转成绝对路径。
导入不会复制 CA 文件，也不会在这一步确认其证书内容；管理客户端初始化时才读取它。
移动或删除源目录前，检查新配置中的路径。

旧 `certs/CAs/` 中的额外 S3 CA、复制会话、分享记录和分析文件不会随别名导入。
将需要的 S3 CA 明确复制到目标 `certs/CAs/`。双端口部署按[配置指南](configuration.md)
复核独立的管理地址与 CA。

## 测试后再切换任务

先用只读管理诊断检查连接，再用临时桶验证实际的数据路径和权限：

```sh
oc --config-dir /path/to/oc-migration --json doctor --online store
oc --config-dir /path/to/oc-migration ls store
```

在线 doctor 只查询管理 ServerInfo，不能证明上传、下载、镜像同步、对象锁或其他提供商兼容性。
按[复制示例](usage.md)上传并下载小文件、比较内容，再预览镜像筛选规则。
只在[兼容范围](compatibility.md)说明的条件下验证任务实际使用的功能。

同步修改脚本、容器中的可执行文件名、配置挂载和环境变量。新设置优先使用 `OC_*`。
OC 0.x 继续支持 `MC_*`，但已设置的 `OC_*` 会优先，环境别名也可能覆盖刚导入的文件。
导入结果与预期不一致时，同时检查两套变量。

Go 模块路径仍为 `github.com/soulteary/mc`，不会因此读取 mc 配置或使用 MinIO 更新通道。
自更新和 MinIO SUBNET 上传保持禁用，升级按[OC 安装指南](installation.md)校验并替换发布产物。

## 恢复旧配置

恢复前，停止使用目标目录的 OC 任务。保留失败配置用于本机检查，再把确认过的
`config.json.backup-<UTC 时间戳>` 恢复为 `config.json`，保持私有权限。
再次检查环境覆盖，然后运行离线 doctor。不要把当前 `config.json` 同时作为
`config import` 的源和目标。

原 mc 文件仍然保留，可以在测试 OC 时继续使用原客户端。
功能差异见[兼容范围](compatibility.md)，连接和导入故障见[排查问题](troubleshooting.md)。
