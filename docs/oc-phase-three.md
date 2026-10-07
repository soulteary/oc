# 阶段三 常用功能与迁移

> 阶段实现与验证记录：本文保留编写时的源码背景、验证结果和限制。当前安装、容器与操作步骤见[文档目录](zh_CN/README.md)，支持范围见[兼容指南](zh_CN/compatibility.md)。早期阶段的待办状态不代表当前项目状态。

本阶段实现 P1 的客户端命名和配置迁移、环境变量兼容、错误输出修复，以及真实 OtterIO 常用管理与高级对象操作的持续验证。基线继续使用 `v0.0.0-20261004215341-be8596f0d69d`，服务端须包含阶段二的管理查询参数桥接补丁及本阶段的关闭超时和 Darwin 重启修复。Go module 路径暂不修改。

## 命名和配置迁移

帮助中的产品名称固定为 `oc`，不随二进制文件名变化。默认配置固定为 Unix 用户目录下的 `.oc`，Windows 用户目录下的 `oc`，不会自动加载 `.mc`。补全安装仍按实际可执行文件名生成，因此重命名的可执行文件可正常补全。

```sh
oc config import ~/.mc/config.json
oc --config-dir /path/to/new-config config import /path/to/old-config.json
```

导入只支持版本 10；旧格式先通过原客户端升级。目标配置的别名整体替换，原有配置备份为 `config.json.backup-<UTC时间>`。源配置保持不变，不自动合并冲突别名。解析、版本、别名、S3/管理地址和桶寻址设置检查完成后才写入；S3 地址严格拒绝非法主机、端口、凭据、查询、片段和路径前缀；错误信息不包含源配置正文。

导入文件和备份使用私有权限，备份先同步落盘，再写同目录临时文件、同步并替换目标文件。导入后的相对管理 CA 路径按源配置目录转换为绝对路径。证书目录、会话和分享记录不自动迁移；需要的 S3 CA 应明确复制到新目录的 `certs/CAs/`。配置备份保留原内容，可手动恢复。

源与目标相同、指向同一文件、非法 JSON、旧版格式、无别名字段和非法管理地址均拒绝导入。默认初始化和既有配置升级行为仍适用于显式指定的配置目录。

## 环境变量与优先级

`OC_HOST_<alias>` 优先于 `MC_HOST_<alias>`；环境别名优先于文件中的 S3 地址和凭据，同时继承文件别名的独立管理地址与 CA。别名后缀大小写保持不变。

- 配置目录：显式 `--config-dir` > `OC_CONFIG_DIR` > `MC_CONFIG_DIR` > 默认目录。
- 区域、加密、分析器：`OC_REGION`、`OC_ENCRYPT`、`OC_ENCRYPT_KEY`、`OC_PROFILER` 优先于对应 `MC_*`。
- 健康检查参数：`OC_HEALTH_TEST` / `OC_OBD_TEST` 和 `OC_HEALTH_DEADLINE` / `OC_OBD_DEADLINE` 优先于旧前缀，显式参数仍优先。
- 管理地址与 CA：沿用阶段二的显式参数、别名环境变量、全局环境变量、文件配置顺序。

对于直接读取的环境变量，显式空 `OC_*` 值也优先，防止静默回退到旧凭据或密钥。`MC_*` 在 OC 0.x 系列内继续支持，移除前至少提前一个次版本公告。旧 `MC_HOSTS_*` 仍按原有弃用规则处理，新增前缀不使其成为推荐接口。

## 管理权限与输出

用户、组、策略、服务账号、配置、配额、健康、heal、profile、服务控制使用 OtterIO 的管理 SDK。双端口部署继续走独立管理入口。

服务账号创建时显式 access/secret 必须同时提供，长度分别为 3–20 和 8–40 字节；省略两者时由服务端生成。编辑时非空 secret 至少 8 字节。创建的长度边界来自当前 OtterIO 凭据生成实现，不能套用普通用户凭据的规则。非法创建参数在发请求前拒绝，避免服务端把校验失败映射成 500 后反复重试。密钥轮换、禁用、启用和删除均有真实权限验证。

当前服务端拒绝 root 执行部分服务账号管理操作。测试采用持有管理权限的普通用户；客户端不绕过服务端权限。`admin console --type otterio` 使用 `OTTERIO` 日志类型，旧 `--type minio` 映射到同一类型。

统一错误诊断输出保留 `status`、`error.message`、`error.cause` 等字段，新增 `error.code` 和 `error.category`。分类包括 authentication、permission、unsupported、not_found、endpoint、canceled、timeout、network、other；保留 SDK 错误码，未知错误不推断成权限失败。诊断对象单行输出，便于 JSONL 处理。

服务重启支持 `--timeout`，默认一分钟；超时表示未确认恢复，不会再次发送重启。轮询使用可取消的定时器，并修复初始化状态分支检查了错误变量的问题。发送前记录各节点 uptime，发送后要求原有全部节点在线且启动时间区间发生变化；旧实例的在线响应、部分节点重启或成员变化均不能作为成功。uptime 以秒计，刚启动的实例会先等待两秒，避免新旧启动时间区间重叠。

`admin info --json` 的旧实现遇到服务端失败仅输出 `status:error`，却返回退出码 0。本阶段修复为退出码 1，并统一成上述错误对象。依赖旧 `error` 字符串字段的脚本需要读取 `error.cause.message`。成功响应结构保持原有约定；其他命令的业务结果结构不在本阶段整体重写。

## 已验证的兼容范围

核心连接矩阵覆盖单端口 HTTP、双端口 HTTP、单端口 HTTPS、双端口独立 HTTPS 证书、双端口公开监控。沿用阶段二的 253 项检查，包括大对象、特殊字符名称、下载哈希、mirror、分享、权限及三种指标路径。

新增配置迁移和参数检查的 22 项命令行检查，覆盖环境目录优先级、显式目录覆盖、备份内容、源配置不变、相对 CA、别名替换、非法导入保护和非法重启超时拒绝。

高级矩阵使用单节点四盘纠删码和双端口独立 TLS，至少 180 项检查覆盖（trace 订阅准备阶段可能增加一次上传检查）：

- 用户查询、列表、禁用和启用；组查询、列表、状态、成员及策略绑定；独立用户仅通过组获得权限，验证禁用、启用和解绑后的实际访问变化。
- 服务账号创建、查询、列表、禁用和启用、密钥轮换及删除；读权限、写拒绝及旧凭据拒绝。
- 配置获取、修改、导出和重新导入，比较恢复后的导出内容；桶配额精确核对字节数、类型和清除后的状态。
- 权限错误的 JSON 分类和非零退出码；单节点不支持分布式锁查询、未配置 KMS 的拒绝。
- 对象锁桶、版本列举、桶及对象标签、法律保留、governance 保留期和带 bypass 的版本清理。
- 生命周期规则创建、查询与删除；CSV Select 的返回内容。
- SSE-C 上传、下载内容校验、无密钥及错误密钥拒绝；通过 `OC_ENCRYPT_KEY` 传入客户密钥。
- 实际对象事件监听、`admin console` 的 otterio/minio 类型真实日志、`admin trace` 的对象请求事件；profile ZIP 完整性及 goroutine 内容、健康 gzip/JSON 内容和服务器信息、后台 heal 状态；临时实例重启后的对象内容与配置保留及服务停止。
- `OC_HOST` 覆盖旧 `MC_HOST` 时保留独立管理配置。

本地最终结果见 [oc-phase-three-results.json](oc-phase-three-results.json)：Linux arm64 的 CGO=0 完整矩阵已通过；Darwin arm64 同时复验 CGO=0 和 CGO=1。每套包含迁移 22、核心 253、高级至少 180 项检查。全部采用默认异步抢占配置，不设置 `GODEBUG=asyncpreemptoff=1`。OC 的完整竞态测试、go vet、golangci-lint 及发行边界检查通过；OtterIO HTTP 包竞态测试和 Darwin 监督进程的重复重启、稳定公开 PID、停止信号转发及监督进程被强制终止后的工作进程退出测试通过。

此前 macOS 的完整场景重启停在 `runtime_BeforeExec`。线程固定和临时收敛调度器都未能稳定解决，最终实现已移除这些尝试。Darwin 现在由轻量监督进程维持公开 PID，工作进程关闭 HTTP 和存储后以专用退出码请求替换；监督进程保持参数、环境、工作目录和标准输入输出，解析原始二进制路径以支持更新，并转发 INT/TERM/QUIT。工作进程通过继承的存活管道监测监督进程，后者被强制终止时也会请求自身停止，避免遗留孤儿服务。工作进程实际 PID 会改变，进程监控应跟踪公开的监督 PID；不会形成嵌套监督进程。其他平台继续使用原有 exec 路径。

本阶段验证单节点服务和单节点纠删码。分布式拓扑、网关、外部 KMS 加密、Webhook 通知目标、远程目标及跨实例复制仍需独立环境验收，不计入已验证承诺。KMS 拒绝测试不代表 KMS 加密通过；事件流测试不代表第三方通知投递通过。纠删码 heal 状态测试不模拟盘故障修复；生命周期测试验证配置往返，不等待真实到期删除。

## 持续验证与发布

```sh
go test -race ./...
go vet ./...
golangci-lint run --timeout=5m --max-same-issues=100000
python3 buildscripts/verify-release-boundaries.py
python3 buildscripts/test-core-integration.py \
  --oc /path/to/oc --otterio /path/to/otterio --extended \
  --report /tmp/oc-phase-three-results.json
```

`--extended-only` 运行迁移与高级场景。报告记录平台及是否关闭异步抢占；最终通过结果均为默认抢占配置。日志测试用临时签名的非法加密请求触发确定的服务端错误，不依赖平台启动日志恰好有某条消息。测试使用随机端口、临时凭据和目录，不读取用户别名，不连接用户部署。超时诊断会脱敏后写入临时目录；失败结果仍保存已完成场景和失败类型。

CI 在 main 分支推送、PR、v 开头标签、手动触发以及每周一 UTC 02:00 执行，固定 OtterIO 依赖并应用阶段二及阶段三服务端补丁，运行完整兼容矩阵并保存结果。Go 单元测试覆盖 Linux、macOS、Windows；真实服务集成覆盖 Linux/macOS 及 CGO=0/1。远程 CI 尚未由本地执行确认，不将工作流配置等同于远程通过。

## 服务端关闭超时修复

检查服务重启流程时发现独立的关闭期限缺陷：HTTP 包虽然配置了 ShutdownTimeout，却先调用无超时的 Fiber Shutdown，因而阻塞请求可能让流程永远到不了后续的超时检查。HTTP drain 期限修复与上述 Darwin 监督重启分别解决两个独立问题。本阶段将 Fiber drain 本身改为 ShutdownWithTimeout；到期明确返回错误，由原有服务控制流程记录，继续关闭存储并执行重启或退出。

修改位于 OtterIO 的 `cmd/http/server.go`。回归测试阻塞一个真实 Fiber 请求，验证关闭在配置期限内返回 deadline 错误，随后释放请求并确认监听器退出。CI 对固定基线额外应用 `buildscripts/otterio-runtime-compat.patch`；该补丁同时包含 HTTP 回归测试、Darwin 监督重启实现及对应测试。新服务端发布后应升级版本并移除过渡补丁。
