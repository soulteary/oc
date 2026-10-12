# OC Web 控制台

[English](../console.md) · [文档目录](README.md) · [完整迁移计划](../console-migration.md)

`oc-console` 随 OC 分发，也可从源码构建。本机 alias 模式使用一个共享身份；当前源码还提供直接 HTTPS 的独立 native IAM 与 OIDC 会话。默认只读，写入、分享及受保护操作需显式开启、服务端协议和当前用户权限。固定 OC `RELEASE.2026-10-10T11-42-18Z` 与旧 Compose 配方保持原版本范围，不会自动获得新功能。选择删除版 OtterIO 配对前，请阅读[候选验证](../console-migration-candidate.md)和[迁移门槛](../console-release-migration.md)。

## 原生 IAM 用户登录与 HTTPS（需从源码构建）

原生模式让多个浏览器用户访问管理员指定的同一组存储端点。每个用户通过 HTTPS 提交自己的 IAM Access Key 和 Secret Key；OC 先签名请求 `GET /otterio/admin/v3/self-credentials`，仅在服务端明确返回 `X-Otterio-Self-Credentials: v1`、`kind: iam`、`status: enabled` 后创建会话。登录不要求列举桶的权限。root、STS、服务账户、目录账户及旧协议的“身份未知”响应均不接受。各会话有独立存储客户端，Cookie 使用 host-only、Secure、HttpOnly、SameSite=Lax。提交或页面挂起时清空凭据表单，OC 不将 Secret Key 保存到浏览器存储、Cookie、CLI 配置或偏好文件中。

需要从包含此功能的源码构建；现有固定版本的发行包和 Compose 示例不会自动获得新功能：

```sh
make build-console
./oc-console --auth-mode native \
  --s3-url https://s3.example.com --admin-url https://admin.example.com \
  --address 0.0.0.0:9090 --public-url https://console.example.com:9090 \
  --tls-cert /etc/oc/public.crt --tls-key /etc/oc/private.key \
  --data-dir /var/lib/oc-console
```

OC 直接处理浏览器 TLS，S3 和管理端点也必须使用 HTTPS；私有 CA 分别通过 `--s3-ca` 和 `--admin-ca` 指定。不接受请求重定向或浏览器指定的后端端点，转发头不能替代真实 HTTPS 连接。首阶段使用单个 OC 实例：会话保存在内存中，30 分钟过期，重启后重新登录；全局最多 16 个会话，同一原生用户最多四个，全实例每分钟最多 30 次登录尝试，沿用现有全局 API、下载和归档资源上限。不指定 `--data-dir` 时，偏好只保留到该用户最后一个活动会话退出。持久偏好文件名由固定端点、身份类型和 Access Key 的哈希生成，用户之间不共享，修改 Secret Key 不改变偏好身份。

原生模式默认只读，添加 `--allow-writes` 开启自身权限内的存储和 IAM 操作。当前支持用户创建/启停/改密/删除、空组创建及条件成员/启停/删除、用户与组策略绑定，以及服务账号创建/启停/改密/限制策略/删除。创建不覆盖已有身份；服务账号创建还绑定权威父身份关系，拒绝 key 冲突。所有操作要求实际服务端能力和关系协议，旧后端不降级到不安全写入路径。

关系变更使用一次性确认计划，绑定操作员会话、准确目标、revision 和受影响身份。冲突时重新读取并确认；已确认或结果不明时撤销捕获范围的会话及待登录，服务端提供的父用户、service/STS 子身份范围也需匹配，明确拒绝则保留会话。用户停用/删除通过服务端耦合生命周期协议协调子身份及成员关系；重新启用或同名同密钥重建不能恢复旧 cookie。停用/删除自身先确认响应再返回登录，其他用户继续可用。父过滤列表不能独自证明已删除子账号 key 不存在。

自身改密仅适用于启用的 native IAM 用户与匹配的安全协议；撤销相关本机会话不等于改密自动撤销所有独立签发凭据。身份变更提交时协调活动写入，不自动重放结果不明的请求。创建生成的 secret 只显示一次，关闭、切页或退出时清空。

原生模式不使用启动 alias 或登录码。已登录 API 请求（退出除外）执行前重新签名验证自身身份；明确拒绝撤销对应会话并取消任务，临时故障或协议缺失返回 503，不执行也不撤销。校验最长五秒、最多八个并行。空闲会话在下次请求时检查，已开始操作不持续轮询；后端授权始终有效。外部变化主动通知和多实例会话仍不在本部署范围。

[TLS 验收记录](../console-native-https-results.json)注明了实际测试的二进制、SDK 固定版本和服务端补丁组合，不能作为其他发行版的兼容性证明。

## OIDC 独立用户与 HTTPS 部署

当前源码支持 `--auth-mode oidc`，固定 HTTPS issuer/client、S3/Admin 和浏览器地址，登记精确的 `PUBLIC_URL/api/oidc/callback`。Authorization Code、S256 PKCE、RS256 换取各用户自己的 STS 权限，刷新替换客户端但不延长固定会话期限。confidential client 的 secret 通过私有普通文件提供，不写命令或环境变量；OIDC IAM 写入与 Console 改密禁用。详见 [OIDC 配置](../console-oidc-login.md)及[native/OIDC HTTPS 配方](../../deploy/README.console-https.md#中文说明)。配方要求对应候选/正式镜像摘要核验，不宣称新发行镜像或平台运行已经通过。

## 界面导航

当前源码提供“服务诊断”，固定旧发行版与 Compose 不会自动获得。当前会话按 `admin:ServerInfo` 权限手动读取节点状态、版本、uptime、堆内存及驱动数量；403 不影响其他功能，也不借用管理员。离开页面取消读取，退出或到期清空快照。平台/运行时和集群总容量仍不可用，账户可见桶容量不是全局容量。

登录后默认进入概览：展示当前身份可访问桶的容量摘要、列出的桶数量、当前会话上传与删除任务数量，以及最近访问的桶。容量来自账户管理接口，统计范围及刷新周期由服务端决定；接口无权限或不可用时显示 `—`，不会通过全桶扫描估算。月度流量、请求趋势及告警尚未接入。

左侧导航提供概览、存储桶列表、任务中心和账户与权限入口，顶部按钮可折叠导航。存储桶列表以表格显示桶名、权限摘要、容量和创建时间，支持按名称筛选、刷新及创建桶。点击桶名进入文件列表，通过返回按钮回到桶表格；配置管理入口使用所选桶名。对象浏览、上传、下载、删除、ZIP 和桶设置位于文件页面；上传与删除结果集中在任务中心，ZIP 准备状态仍从对象浏览器的 ZIP task 查看。页面切换保留已加载的位置和任务。最近访问记录按存储身份保存，退出或重启后保留。窄屏下导航调整为横向按钮，概览调整为单栏。

文件行支持悬停或键盘聚焦显示复制链接与收藏操作。详情显示列表返回的路径、大小、修改时间和 ETag；复制的是需要本机会话的下载链接，对外分享须显式启用预签名分享。收藏的桶与对象路径按存储身份保存，重新登录后恢复。更多菜单按能力提供历史版本、分享与删除；复制对象和重命名尚未接入。

图片预览在点击后读取文件，支持 JPEG、PNG、GIF 和 WebP，最多 20 MiB，可缩放与关闭；关闭时取消读取并释放图片数据。详情的“读取对象信息”获取当前对象真实大小、ETag、修改时间及 Content-Type，不依赖列举权限。对象权限、加密、存储类型、标签及自定义 Header 编辑尚未接入。

## 构建与启动

使用 `go.mod` 声明的 Go 版本，无需 Node 或安装前端依赖。

```sh
make build-console
./oc-console --alias store
# 开启写操作，默认每个文件最多 1 GiB
./oc-console --alias store --allow-writes
# 调整字节上限，允许范围为 1 字节至 5 GiB
./oc-console --alias store --allow-writes --max-upload-size 268435456
# 同时开启分享；使用接收者实际可访问的 S3 根地址
./oc-console --alias store --allow-writes --allow-sharing \
  --share-url http://127.0.0.1:9000 --max-archive-size 5368709120
```

先通过 `oc alias set` 配置 `store`；省略凭据时使用 OC 的终端输入提示。打开启动时打印的完整 URL，输入同一处打印的登录码。该码可以访问选定别名，应妥善保管。浏览器会话 30 分钟到期；退出登录会取消该会话的进行中请求和任务。进程停止前登录码仍有效，到期后可重新登录。

默认监听 `127.0.0.1:9090`，只接受回环 IP，也支持 `[::1]:9090`。本机模式只有一个存储身份，不适用于多人共享；不要通过端口转发、隧道或反向代理对外暴露。

```sh
./oc-console --config-dir /path/to/oc-config --alias store \
  --address 127.0.0.1:9091 \
  --s3-ca /path/to/s3-ca.pem \
  --admin-url https://admin.example:9001 \
  --admin-ca /path/to/admin-ca.pem
```

## 正式 Docker 镜像

`deploy/compose.console.yaml` 固定 OC `RELEASE.2026-10-10T11-42-18Z`
及其已发布的多平台镜像摘要。初始化与控制台使用同一镜像，无需本地构建。
保留已有数据与配置目录；初始化会更新 `store` 别名。示例端口不覆盖现有
S3/Admin 地址配置。

固定 OtterIO 发行版未提供控制台创建用户／组所需的条件 IAM 创建协议，
完整 IAM 功能验收仍受阻。见[正式版本组合验收](../console-released-pair.md)。

## 可选的本地 Docker 镜像

仓库根目录的 `Dockerfile` 同时构建 `oc` 与 `oc-console`，镜像默认入口仍为 `oc`。`Dockerfile.release` 也包含两个程序；`Dockerfile.dev` 仍用于 CLI 镜像。

```sh
docker build -t soulteary/oc:local-console .
docker run --rm --entrypoint oc-console soulteary/oc:local-console --help
docker run --rm -it --network host \
  -v "$HOME/.oc:/config:ro" \
  -v "$PWD/oc-console-data:/app-data" \
  --entrypoint oc-console soulteary/oc:local-console \
  --config-dir /config --data-dir /app-data --alias store --address 127.0.0.1:9090
```

配置目录需已存在 `store` 别名。需要写入或分享时分别添加对应启动参数。控制台默认只监听回环地址，普通 `-p` 映射不适用；bridge 网络请使用下方部署配置；Linux 使用 host 网络，macOS Docker Desktop 需启用 host networking。私有 CA 文件需另行挂载，配置中的路径应在容器内有效。

## 配置与证书

- local alias 模式只读加载版本 10 的 `config.json`，不会创建或修改配置。必须选择已存在的 S3 别名，不提供本地文件访问。
- 配置目录优先级：`--config-dir`、`OC_CONFIG_DIR`、`MC_CONFIG_DIR`，最后使用 Unix 的 `~/.oc` 或 Windows 用户目录下的 `oc`。
- 管理地址优先级：`--admin-url`、`OC_ADMIN_URL_<alias>`、`OC_ADMIN_URL`、配置中的 `adminURL`、S3 地址。管理地址必须是根 URL，重定向会被拒绝。
- 管理 CA 优先级：`--admin-ca`、`OC_ADMIN_CA_<alias>`、`OC_ADMIN_CA`、配置中的 `adminCAFile`，否则复用本次 S3 信任池。显式管理 CA 使用系统根证书加该 PEM，与 S3 私有 CA 分开。
- S3 使用系统根证书加 `<config-dir>/certs/CAs`。`--s3-ca` 用系统根证书加指定 PEM 替换该自定义目录的信任。证书校验始终启用。
- 相对 CA 路径按当前工作目录解析，与 CLI 加载器一致；建议使用绝对路径。
- 当前程序拒绝 `OC_HOST_<alias>`/`MC_HOST_<alias>` 覆盖，需将别名写入配置。读取配置中的临时 session token，但不自动续期。

启动别名的凭据留在 OC 进程内，浏览器通过 HttpOnly、SameSite=Strict 会话 cookie 和独立 CSRF token 访问控制台。IAM 创建操作新生成的凭据只显示一次；已有凭据不回传。不提供别名编辑，不在浏览器持久保存凭据或登录码。别名或 CA 修改后重启程序。

## 上传和覆盖

上传默认只创建新 key。服务端必须在 HEAD 响应中宣告 `X-Otterio-Conditional-Writes: v1`，并在目标写锁内检查普通 PUT 和分段完成的 `If-None-Match: *`。固定版本服务端已包含该保证；仅做 HEAD 存在性预检不能保证并发时不覆盖。没有此能力的服务端会明确拒绝默认上传。

目前 OtterIO 的 FS 和单 pool erasure 支持这项能力；gateway、多 pool 和写回缓存不宣告能力并拒绝条件写请求。该保护没有宣称任意第三方 S3 兼容。

选择允许替换后需要第二次确认准确的桶和完整 key。这允许替换执行时的当前对象，版本桶则可能新增版本；没有 ETag 比较交换保护。只写用户可能没有 HEAD 权限，可以手填目标并明确允许替换；OC 不借用管理员身份预检。

上传以原始文件发送，通过 OC 转发。浏览器进度 100% 仅代表发送完成，收到存储完成确认后才显示成功。最多同时执行两项写操作；每次分段上传使用两个固定 16 MiB 缓冲，将下一段的读取、校验与上一段发送重叠，上游最多一段同时发送，确认后才复用对应缓冲。只终止本次创建的 upload ID。文件长度必须明确，空文件有效，超过上限会拒绝。

取消、页面离开、登出或进程停止会解除阻塞读取并尝试清理本次分段。已经提交的对象不会自动回滚。若最终响应丢失，任务显示结果未确认；若清理失败，会明确提示检查未完成上传。重试前先检查对象，程序不自动重复最终提交或删除。

## 新增的五项功能

**存储桶。** 写模式显示 **Create bucket** 和 **Delete empty bucket**。删除前必须输入准确桶名；服务端负责原子空桶检查，非空桶会返回冲突。此操作不会清空对象、历史版本、删除标记或未完成上传。

**历史版本。** 对象的 **Versions** 显示版本 ID、时间、大小、是否最新和删除标记，支持分页与指定版本下载、分享、ZIP。删除标记不能下载；`null` 是明确的版本值。版本读取要求服务端宣告 `X-Otterio-Version-Authorization: v1`，缺失时拒绝操作，绝不回退到当前对象。服务端须分别检查 `s3:ListBucketVersions` 与 `s3:GetObjectVersion`，使用实际 `versionId` 检查条件。FS 不支持启用版本；需要单 pool erasure 和版本桶。这里提供版本选择，不提供恢复覆盖或永久删除历史版本。

**ZIP。** **ZIP selected** 可归档已选对象，**ZIP prefix** 可归档当前前缀；只读模式也可使用。计划会完整扫描前缀，最多 1000 个对象，默认源数据总量最多 5 GiB，`--max-archive-size` 可调低。当前对象用 ETag 条件读取固定内容，显式历史选择保留版本 ID；内容发生变化则整项失败。先在本机生成完整 ZIP，完成后才开放一次下载。压缩包使用安全、唯一的文件名，`manifest.json` 保存原始桶、key、版本、ETag 与归档路径映射。

一次只保留一个准备中或待下载的归档，归档十分钟到期；下载完成、取消、登出和正常退出均删除临时文件。**Cancel** 会关闭正在读取的源对象。关闭对话框可稍后继续查看任务，刷新页面会恢复当前会话的活动归档；创建或取消响应丢失时先查询任务，不重放提交。任务归属于当前会话。`--archive-dir` 指定临时目录，文件权限为 0600。异常杀死进程可能留下临时文件，应按本机临时文件策略清理。

**预签名分享。** 启动时加入 `--allow-sharing --share-url <S3 根 URL>`，也可用 `OC_SHARE_URL_<alias>` 或 `OC_SHARE_URL` 设置地址，优先级为命令行、别名环境变量、全局环境变量。**Share** 为一个对象或明确版本生成 GET 下载链接，默认一小时，范围一秒至七天，可指定下载文件名。URL 使用接收者地址直接签名，不能生成后再替换主机名；客户端不连接该公开地址。链接不写入任务记录或浏览器持久存储。持有链接即可下载，控制台退出不会使已签发链接失效；到期时间还受源账号和临时凭据有效期限制。

**用户、组与服务账号。** 列表和操作受实际权限约束。native 使用上述关系条件与单次计划；OIDC IAM 写入和 Console 改密禁用。local alias 保留启动身份及旧预检：用户有列出的组或 service 子账号时拒绝删除，影响启动凭据或结果不明的修改可能停止本机 OC；native 则撤销捕获身份范围，其他用户继续可用。删除组要求为空，禁用组不清除直接或其他组权限。现有 secret 不回传；自填服务 secret 必须同时填 key，二者留空才生成并一次显示完整凭据。

策略绑定使用读取 revision 后的确认条件替换；native 还绑定权威关系和受影响身份。服务端原子检查策略存在、授权及关系 revision，冲突需重新读取确认。旧服务端只读或明确拒绝，写请求不自动重放；结果不明时先核实准确目标。

历史五功能 fixture 使用 console base、版本授权、条件 IAM 和生命周期补丁，见[原 feature 记录](../console-features.md)。这些是各自准确程序的历史证据，不是当前 OtterIO main 的安装步骤；已经包含协议的服务端不能重复应用导出补丁。源码构建 OC 不升级后端，当前配对需按[候选验证](../console-migration-candidate.md)独立检查能力、权限及条件响应。

## 删除与任务

支持手填准确 key、选择已加载的对象，以及非空且以 `/` 结尾的前缀。前缀计划先用当前身份完整列举固定名单，最多 1000 个对象；无列举权限、分页异常或超限时不会开始删除。只写用户仍可直接计划有权限的准确 key。

确认页面列出桶、前缀或完整 key、数量和准确名单。确认 token 属于当前会话和计划，只能消费一次。计划不会追加后来创建的新 key，但删除执行时的当前对象；同名对象在计划后被替换，也可能被删除。这个确认不是版本快照。

删除不传 version ID，不永久删除历史版本，也不绕过对象锁。在启用版本的桶中，删除当前 key 通常产生 delete marker，旧版本仍保留；普通未版本化对象删除不可由控制台撤销。批量操作分别显示成功、失败、结果未知和未执行，不宣称整批回滚。

未开始的上传一分钟到期，删除计划十分钟到期；进行中的任务受会话期限约束。状态只对所属浏览器会话可见，刷新可恢复任务状态，但不能恢复确认 token 或已离开页面的文件传输。完成记录最多保留十分钟；容量压力下会提前清理最早的完成记录。每会话最多 16 项任务、进程最多 64 项；活动任务不会为新任务被驱逐。

离开页面或使用浏览器重新加载页面会中断上传，页面内的 Refresh 按钮不中断上传。已确认的后台删除可继续执行；需要停止删除时，应取消任务或退出登录。页面导航不是撤销操作。

## 桶配置与自身账户

默认只读模式可以读取配置。修改配置和自身改密必须重启并带上 `--allow-writes`，
同时具备当前身份的实际权限和配套服务端协议；开启开关不会授予权限或增加服务端能力。

修改配置的操作顺序：

1. 打开 **Bucket settings**，输入准确桶名，选择策略、版本或生命周期，再点击 **Read setting**。
2. 编辑返回的完整文档，点击 **Review change**，核对已加载的桶、配置类型及全文后点击 **Apply this setting**。已有策略或生命周期可以通过 **Review removal** 单独复核并确认删除。
3. 检查存储确认后返回的文档。发生冲突或结果不确定时，先复制要保留的编辑，再读取当前配置，重新复核后提交。

设置入口支持手填准确桶名；没有桶列举权限时，仍由当前身份尝试读取该桶的配置。策略使用完整 JSON，版本和生命周期使用完整 XML；编辑器保留服务端返回的文档，不把高级字段转换成简化表单；服务端验证支持字段并可能规范化格式。受保护写入拒绝未实现字段，不能用页面填写来开启服务端尚未支持的功能。当前版本设置只实现 Status，不支持 MFADelete 或排除前缀。三个配置分别读取、授权和报错。

受保护保存同时需要读取权限和修改权限，因为 OC 在提交前读取能力与 revision，提交后
再次读取并核实实际文档。策略需要 `s3:GetBucketPolicy`，以及保存的 `s3:PutBucketPolicy`
或删除的 `s3:DeleteBucketPolicy`；版本需要 `s3:GetBucketVersioning` 与
`s3:PutBucketVersioning`；生命周期需要 `s3:GetLifecycleConfiguration`，保存和删除均
使用 `s3:PutLifecycleConfiguration`。若新策略撤销了自身读取权限，配置可能已提交，
但读回验证失败，页面仍会报告结果未确认。此时先用有读取权限的身份核实实际配置，
再决定后续修改，不重放已提交的写请求。

读取覆盖未保存草稿前需确认，取消或失败保留草稿及准确保存范围。实际服务端必须提供受保护配置及生命周期语义；[历史实现边界](../lifecycle-transition.md)中的 base/lifecycle-storage 只认证记录的程序和补丁，不等于当前配对通过。`NewerNoncurrentVersions` 等未支持字段、标签与删除标记不兼容组合须拒绝而非丢弃。非法 UTF-8 或孤立 UTF-16 在修改配置、secret 前拒绝。

配置写入需要服务端返回 `X-Otterio-Bucket-Config: v1`、64 位配置 revision 和 exists 标志。请求用 SigV4 签名 `X-Otterio-Config-If-Match`，服务端在完整元数据事务锁内比较版本再保存；其他客户端已经修改时，返回冲突。界面保留草稿但禁用继续保存，需要复制要保留的编辑并重新读取。结果不确定时先读取真实配置，不自动重放。

保存或删除需要再次确认准确的桶、配置类型和完整内容。策略修改会影响访问权限；生命周期可以过期删除数据；版本启用后通常只能暂停而不能回到从未启用。只允许删除策略与生命周期，不提供删除版本配置的操作。每份文档最多 1 MiB，服务端原有更小限制仍适用。FS 支持策略和生命周期保护写入，单 pool erasure 还支持版本设置；gateway、多 pool 或不具备能力的服务端只读降级。

ILM 目标注册、凭据更新和移除在终端使用 `oc admin bucket remote`，操作与限制见
[转换指南](../lifecycle-transition.md)。Web 编辑器修改生命周期规则，不提供远端目标
管理或历史版本恢复按钮。转换协议仅支持单节点、单 pool erasure。

账户页面只显示身份类别、状态、是否允许改密及现有容量/权限概览，不回传 access key、secret 或 session token。自身改密只对已启用的普通 IAM 用户开放，遵守显式 `admin:CreateUser` deny；root、STS、服务账号、目录身份及分布式、etcd、外部 OPA 授权场景禁用。新 secret 限 8–128 个 UTF-8 字节，不能包含 NUL、回车、换行。输入在提交、取消、关闭及登出时清空，不持久保存。

local alias 模式中，符合条件的账户先完成或取消活动写入，点击 **Account** 打开 **Current account**，填写 **New secret key**，
点击 **Review secret rotation**，再确认 **Rotate secret and stop console**。
随后在终端核实哪组凭据有效，用 `oc alias set` 更新别名，重新启动 `oc-console`，
打开新打印的 URL 并使用新的登录码。更新时保留别名原有的 S3、管理入口、寻址与证书设置。

local alias 模式改密前应完成或取消所有活动写入；进程会拒绝与上传、删除、配置写入及分段清理并发改密。成功或提交结果不确定时，撤销该进程所有会话和待执行任务，返回重启提示后停止。OC 不自动更新别名文件；在终端核实新凭据并更新别名，再启动控制台并重新加载页面。确定的权限拒绝不会自动停止连接。这里撤销的是该 OC 进程的本机会话，不宣称撤销用户已签发的 STS 或服务账号；这些独立身份仍需单独管理。

当前依赖以 `compatibility.json` 为准，模块 pin 不是远程服务端身份。旧 P3/base/lifecycle fixture 保留历史程序范围，复现见[开发指南](development.md#复现可选控制台协议环境)，不能作为 current main 应用补丁步骤或新发行兼容证明。使用准确候选配对与运行时能力检查，缺少受保护设置/身份协议时继续拒绝。

停止 OC 或退回原 UI 时继续使用当前服务端，不会撤销配置修改、已轮换的 secret 或
转换对象。新的转换引用需要服务端能理解该引用；服务端降级前必须另行验证存储兼容性。

## 浏览、权限与限制

列举、下载和写操作均遵循当前身份的实际 S3 权限。桶根目录 Read/Write 只作概览，不替代具体对象或前缀授权。固定版本服务端已支持 root AccountInfo；概览接口不可用或被拒绝时，S3 操作继续工作。

下载通过 OC 转发到浏览器，支持已认证 GET/HEAD、条件完整响应和单段字节 Range，包括 If-Range 与固定真实对象/版本。多段 Range 不支持；上游分段响应校验后才接受字节。失败信息单独显示，控制台保留。见[Range 实现边界](../console-range/backend.md)。OIDC 已在当前源码提供，代理 TLS 终止和多实例部署不由本指南认证。

进程最多 16 个会话、8 个并发普通存储请求、2 个下载、2 个前缀计划和 2 个写操作。S3 元数据与删除请求有 15 秒整体期限，普通控制台元数据请求额外受 30 秒期限约束，前缀扫描最多 60 秒。JSON 读取最多 10 秒；上传读取和下载读写采用 30 秒空闲期限，持续有进展的上传不受原先固定 10 秒整次请求限制。

关闭时先取消会话，再给 HTTP 和后台分段清理各五秒期限，最坏关闭路径最多约十秒；正常取消的五秒预算以实际验收结果为准。这些期限不是吞吐保证。Host/Origin 与跨站校验要求使用启动时打印的地址，不自行换成 `localhost`。key 按 S3 原值处理，不作文件路径清理。

## 验证与迁移状态

```sh
make test-console
```

当前源码配对 CI 与真实跨版本升级要求见[候选验证](../console-migration-candidate.md)。[五功能](../console-features.md)、[第三阶段](../console-phase-three.md)、[转换](../lifecycle-transition-verification.json)、[native IAM](../console-relationship-iam-acceptance.md)和[真实 OIDC 浏览器](../console-oidc-browser-acceptance.md)报告保留准确程序、脚本和失败范围，不自动认证新发行。开发前端测试需 Node，运行不需要。`make build` 构建 CLI，`make build-console` 构建 Console，发行包包含二者。正式程序/镜像配对、支持的升级路径和迁移政策仍单独验证。

## Docker bridge 网络

使用 `deploy/compose.console.yaml`，将它复制到部署目录作为 `docker-compose.yaml`，并在同目录设置 `.env` 中的 `OTTERIO_ROOT_USER` 和 `OTTERIO_ROOT_PASSWORD`。先运行 `docker compose pull` 拉取固定发行镜像，再启动 OtterIO，等待服务就绪，再运行 `docker compose run --rm oc-init` 和 `docker compose up -d --no-deps oc`。

`--container-listen --address 0.0.0.0:9090 --public-url http://127.0.0.1:9090` 显式启用容器监听。`--public-url` 仍须为 HTTP 本机回环 URL，用于严格校验浏览器 Host 与 Origin；访问时应使用完全相同的地址。宿主机仅将控制台端口映射到回环地址。OtterIO 管理接口通过内部 `http://otterio:9001` 访问，无需映射到宿主机。默认启动方式仍只允许回环监听。

## 语言与用户数据

右上角切换中英文，语言、收藏及最近访问存于私有身份文件，不含凭据或会话。local 使用 S3/Access Key 身份；native 绑定固定 S3/Admin、身份类型与 key；OIDC 绑定固定端点、issuer 和已验证 subject。native 改密不改变身份，独立用户不继承共享 alias 偏好。

`--data-dir` 指定可写偏好目录。local 默认 `<config-dir>/console-data`，native 未指定时仅临时保存，OIDC 使用独立选出的目录。新 [HTTPS 配方](../../deploy/README.console-https.md#中文说明)明确持久挂载 `/app-data`。只读存储模式仍可保存偏好，失败会提示；重启需重新登录，持久偏好保留。

## 版本功能自动检测

选择存储桶后，OC 使用当前身份执行 `GET ?versions&max-keys=0` 的只读探测，不返回对象版本列表，也不修改桶配置。仅当后端响应有效并提供 `X-Otterio-Version-Authorization: v1` 时显示历史版本和版本控制配置。FS 后端的 `NotImplemented`、旧服务器缺少授权标记、权限拒绝及连接失败均保持入口隐藏；刷新或切换桶会重新检测。没有历史版本或尚未开启版本控制不代表服务器不支持版本功能。

### 对象重命名

文件行悬停或获得键盘焦点后，复制图标左侧会显示重命名入口。启用写入后，输入新文件名并确认；目录保持不变，已存在的目标名称会被拒绝。重命名通过服务端复制后删除原对象完成，最大支持 5 GiB，不是原子操作，请避免同时修改相关对象。复制成功但删除失败时，界面提示检查两个名称，不自动重复操作。

## 以 OC 为主要界面，关闭旧 Web

Compose 示例设置 `OTTERIO_BROWSER=off`，只关闭 OtterIO 内置网页及其 Web RPC、上传、下载和 ZIP 入口。S3 在 9000，内部 Admin API 保留在 `--console-address :9001`，OC 通过 `--admin-url http://otterio:9001` 访问它；浏览器使用 `http://127.0.0.1:9090`。不要随旧网页删除管理监听器。本次只改变示例部署，不改变 OtterIO 服务端默认值。

迁移前备份部署配置，保留当前 OtterIO 镜像版本，并拉取 Compose 固定的正式 OC 镜像（开发时也可显式使用本地构建）。启动后检查旧网页不可用，并在 OC 中验证浏览、上传下载、删除、ZIP、桶设置和 IAM；受保护写入及历史版本仍受服务端能力和当前身份权限限制。重启 OC 后确认语言、收藏和最近访问保留。

旧固定 bridge 仍是单身份本机控制台。当前 native/OIDC 可通过[直接 HTTPS 配方](../../deploy/README.console-https.md#中文说明)提供独立用户，但需核验最终程序、服务端协议、实际升级和公开迁移门槛。保留前一版本及配置；旧 bridge 的同版本回退不是删除版服务端的兼容证明。

仅对该 bridge 固定的旧服务端，设置 `OTTERIO_BROWSER=on` 并同版本重建、按需发布回环 9001 可恢复旧页面；不撤销任何写入、IAM 或配置变更。删除版显式 `on` 会失败，不能恢复内置 Web。回退旧服务端必须单独验证跨版本数据/配置兼容，见[迁移门槛](../console-release-migration.md)。
