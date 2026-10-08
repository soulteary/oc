# OC 命令索引

[文档目录](README.md) · [English](../commands.md)

这里列出当前客户端注册的命令分类。不同版本的参数和服务端能力可能不同，具体参数以实际运行程序的帮助为准：

```sh
oc --help
oc cp --help
oc mirror --help
oc admin --help
oc admin user svcacct --help
```

OC 支持在命令前填写全局参数，许多子命令也会列出通用参数。脚本中将全局参数放在命令前更容易辨认。本地操作使用文件路径，S3 对象使用 `别名/桶/对象`。别名保存地址和凭据，不是文件系统挂载点。

## 配置与诊断

- `alias`：`set`、`list`、`remove`，用于管理保存的服务别名。`set` 整体替换条目，`list` 不展示环境覆盖，`remove` 不取消环境覆盖；没有独立的 `alias export` 命令。
- `config import`：导入版本 10 的 mc/OC 配置，写入前备份目标配置，详见[迁移指南](migration.md)。
- `doctor`：报告客户端和平台，不打印凭据；提供别名时检查解析后的协议与管理设置。`adminSDK` 表示 OtterIO 服务端/管理模块，不是独立 S3 SDK。`--online` 还会查询管理入口，必须提供别名。
- `update`：保留入口，用于明确说明自更新已禁用，不下载或替换 OC。

别名、环境优先级和 TLS 见[配置指南](configuration.md)，诊断结果解释见[故障排查](troubleshooting.md)。

## 查询与传输文件

- `ls`、`tree`、`find`：列出或查找桶、对象和本地文件。
- `stat`、`du`、`diff`：查看元数据、统计大小或比较对象名称、大小和日期。`diff` 不等于内容哈希校验。
- `cp`、`mv`、`mirror`：复制、移动和同步。`mv` 在传输成功后删除源；`mirror --remove` 可以删除只存在于目标的条目。
- `cat`、`head`：读取内容或对象开头的行。
- `pipe`：上传标准输入中的数据。
- `mb`、`rb`、`rm`：创建桶、删除桶或对象/文件；删除前先确认具体命令的参数。
- `share`：生成临时访问链接。签名 URL 在到期前属于敏感数据。
- `sql`：在服务端支持时执行 S3 Select 查询。

示例及 mirror 的筛选、删除规则见[常用操作](usage.md)。

## 对象与桶功能

- `version`、`undo`：管理桶版本，或撤销支持的 PUT/DELETE 操作。
- `retention`、`legalhold`：在支持对象锁的服务中管理保留期及法律保留；桶配置和权限仍需满足前提。
- `ilm`：通过 `add`、`edit`、`rm`、`ls`、`export`、`import` 管理生命周期规则。CLI 导入导出使用 JSON，控制台完整生命周期编辑器使用 XML。
- `encrypt`：管理服务端桶加密配置；外部 KMS 能力取决于部署。
- `tag`：管理桶和对象标签。
- `policy`：管理对象匿名访问；与管理身份策略的 `admin policy` 不同。
- `event`、`watch`：配置或监听对象通知。实时流不提供持久化事件重放。
- `replicate`：配置服务端桶复制；固定测试实例没有记录外部、多实例的复制验收。

命令存在不代表所有服务商和拓扑都支持。使用高级功能前，阅读[兼容范围](compatibility.md)和具体命令帮助。

## OtterIO 管理

`admin` 需要 OtterIO 管理接口及对应权限，不能当作通用 S3 管理接口。

- `admin info`：读取服务信息。
- `admin user`、`admin group`、`admin policy`：管理身份、组及策略；服务账号位于 `admin user svcacct`。
- `admin config`：读取、修改、导出和导入服务端配置；导出内容可能含有凭据。
- `admin service`：重启或停止服务，会影响部署。
- `admin bucket`：管理桶配额、远程目标等设置。修改 ILM 目标需要配套服务端的转换协议及管理权限，见[管理指南](administration.md#生命周期转换目标)。
- `admin prometheus`：生成监控抓取配置。
- `admin trace`、`admin console`：查看实时请求和日志流。
- `admin profile`、`admin subnet health`：收集诊断文件；保留历史 `subnet` 名称，但 SUBNET 上传已禁用。
- `admin top`、`admin kms`：依赖部署条件的锁查询、KMS 操作。
- `admin heal`：已经标注弃用，使用前核对帮助和部署前提。

当前 `admin` 分组不包含 `admin update`。[管理指南](administration.md)说明操作步骤、指标地址、权限和诊断文件处理。

修改 IAM 绑定前，先阅读[当前 `admin policy update` 限制](administration.md#管理策略用户和组)：
重复策略或空参数可能清空已有绑定，应改用 `admin policy set` 提交核对后的完整列表。
当前没有专用的 CLI 对象恢复命令，生命周期转换记录另行说明原生 S3 恢复请求。

## 通用参数

- `--config-dir`、`-C`：选择客户端配置目录。
- `--admin-url`、`--admin-ca`：覆盖管理地址或管理 CA 文件。
- `--json`：使用该命令的 JSON 输出模式。错误诊断为单行记录，成功或导出结构及排版取决于命令和输出模式，不应假定所有命令采用同一 JSONL 格式。
- `--quiet`、`-q`、`--no-color`：控制进度条和颜色。
- `--debug`：添加调试诊断，分享前检查是否包含敏感上下文。
- `--insecure`：关闭证书校验。正常部署应配置合适的 CA，不把该参数作为长期设置。
- `--help`、`-h`：查看命令帮助。客户端发布标签使用根命令 `oc --version` 或 `oc -v`。
- `--autocompletion`：安装 Shell 补全，可能修改 Shell 配置；这是显式设置步骤，不是只读诊断。

`mirror --fake`、`cp --recursive`、`watch --events` 和保留期等专用参数，以实际版本对应命令的帮助为准。
