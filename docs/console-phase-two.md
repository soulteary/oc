# Web 控制台第二阶段实现、验收与问题修正

[完整迁移计划](console-migration.md) · [本机使用指南](zh_CN/console.md) · [English usage](console.md) · [第一阶段记录](console-phase-one.md)

日期：2026-10-08（本地时区）；结果文件使用 UTC。本文记录 P2 的对象写操作、权限一致性和任务生命周期。**P2 本机实现与验收已完成**：同一最终构建在当前服务端及固定五补丁基线上，各通过单 HTTP、双 HTTP、独立 CA 双 TLS 三场景，每场景 21 组验收，共 126 组。最终报告与实际控制台二进制、服务端补丁及验收脚本摘要一致。

现有 OtterIO Web 控制台继续保留，browser 默认值未改。本文记录 P2 完成时的范围；后续 P3 进展见[第三阶段记录](console-phase-three.md)，旧 UI 的完整替代版仍未交付。

## 实现范围与默认边界

P2 沿用 P1 的独立 `oc-console` 程序、本机回环监听、单操作员及启动时固定的一个 S3 别名。默认仍为只读；只有进程显式带 `--allow-writes` 才开放写接口，浏览器不能自行切换模式。默认每个文件上限 1 GiB，`--max-upload-size` 接受 1 字节至 5 GiB。

- [`internal/consoleapi`](../internal/consoleapi/types.go) 增加独立的 `MutationBackend`、上传结果和无凭据的任务 DTO；读取接口仍单独保留。
- [`internal/storageclient`](../internal/storageclient/mutations.go) 使用启动身份执行上传、扫描和删除，复用实例连接池，不导入 CLI 命令或替换为管理员身份。
- [`internal/console`](../internal/console/jobs.go) 管理任务归属、状态、单次确认、限额、取消和逐对象结果。
- [`internal/console/web`](../internal/console/web/app.js) 增加文件上传、覆盖确认、准确 key/已选对象/前缀删除、任务恢复和失败提示；HTML/CSS/JavaScript 直接嵌入 Go，无新增 Node/npm 构建依赖。
- [`cmd/oc-console`](../cmd/oc-console/main.go) 增加显式写模式和上传限额，关闭时先取消会话，再等待 HTTP 与后台清理。

存储密钥和临时 session token 只进入 OC 的协议客户端。浏览器使用 HttpOnly、SameSite=Strict 会话 cookie 和独立 CSRF token；不提供存储凭据输入、别名编辑或本地文件浏览，不在 localStorage 保存登录码、凭据、文件或删除确认。

## 上传：新 key、覆盖与提交结果

上传先用 `POST /api/uploads` 登记准确的桶、完整 key、文件长度和是否允许覆盖，再用 `PUT /api/uploads/{id}` 发送原始文件。PUT 固定为 `application/octet-stream`，实际长度必须与登记长度一致。空文件有效，未知长度和超限输入被拒绝。

创建新 key 是默认行为。单次 HEAD 存在性预检无法防止并发覆盖，因此 OC 还要求该 HEAD 响应宣告 `X-Otterio-Conditional-Writes: v1`，并对普通 PUT 和 multipart 完成发送 `If-None-Match: *`。缺少能力声明、无 HEAD 权限或检查失败时，默认上传明确拒绝，不降级为无条件写入。

新增的 [OtterIO 条件写兼容补丁](../buildscripts/otterio-conditional-writes-compat.patch) 在目标 namespace 写锁内检查当前对象，再提交普通 PUT 或完成 multipart，并将冲突映射为 HTTP 412。能力仅用于 FS 和单 pool erasure；gateway、多 pool、未初始化存储及写回缓存不宣告该保证。写回缓存中的尚未落盘对象及后来无条件回写会破坏原子检查，不能仅绕过当前请求的缓存就声称安全。条件写路径在支持的缓存配置下直接使用底层存储。

选择允许替换后，页面另外确认准确的桶和完整 key。这允许替换执行时的当前对象，或在版本桶新增版本；没有 ETag 比较交换，也不承诺保留计划前的当前内容。只写用户可以手填目标并明确允许替换，避免被列桶或 HEAD 权限挡住有权执行的 PutObject。

浏览器经 OC 转发文件，不获取 S3 密钥或预签名地址。浏览器进度到 100% 只表示发送结束；只有任务收到完整的服务端提交确认并进入 `succeeded` 才表示成功。OC 最多并发两项写操作，每个 multipart 使用两个固定 16 MiB 缓冲区，将下一段的读取、校验与上一段发送重叠。上游最多一段同时发送，逐段确认后才复用对应缓冲；大文件缓冲固定为 32 MiB，不随文件长度增长。

最终提交和删除不自动重放，S3 客户端设置一次请求尝试。丢失提交响应、缺少有效确认或取消后的结果无法确认时，页面要求检查存储后再决定是否重试，不显示“已回滚”。清理只针对本次创建的 upload ID；失败会提示同时检查最终对象与未完成上传。

## 删除：明确范围、当前 key 与逐项结果

支持手填一个准确 key、选择当前已加载的真实对象，以及删除非空且以 `/` 结尾的前缀。前缀行不混入多对象选择，Load more 只增加已加载对象的可选范围，选择操作不隐含“全部分页对象”。

`POST /api/deletions/plan` 先返回固定名单，最多 1000 个 key。前缀扫描使用当前身份的 ListBucket 权限；无权限、分页不一致、重复 cursor 或超限都会使计划失败，尚未开始删除。准确 key 的计划不要求列举权限，真正的 DeleteObject 权限在执行时逐项检查。

确认页展示桶、前缀或完整 key、数量、准确名单和到期时间。`POST /api/deletions/{id}/execute` 只执行该计划；确认 token 属于当前会话和计划，正常执行只消费一次。之后的状态读取不会返回或重新生成 token。

名单固定的是 **key，而非版本**：计划后新增的其他 key 不会追加；名单中的同名对象若被替换，执行时的当前对象仍可能被删除。页面已明确说明这项范围。删除不发送 version ID、永久删除选项或对象锁绕过选项。版本桶通常产生 delete marker、保留旧版本；普通未版本化对象的删除不可由控制台撤销。

逐项结果区分 `pending`、`succeeded`、`failed` 和 `unknown`。`Completed` 统计成功对象；“已尝试”数量另外根据非 pending 的逐项状态计算。整批失败、部分失败、取消和无法确认的对象都显示在任务区，不承诺整批回滚，也不自动重试未知结果。

## 权限、任务恢复与取消

只读、只写、前缀条件、显式 deny、组策略和 STS 都走启动时选定的存储身份。桶根目录的 AccountInfo Read/Write 只是概览，不作为具体 key 或前缀的授权结论。管理接口不可用或被拒绝，不阻断 S3 操作。浏览器无法指定另一别名或请求 root 代查。

任务使用不可猜测的 ID，并绑定浏览器会话。服务器状态为 `waiting`、`planning`、`ready`、`running`、`succeeded`、`failed`、`partial`、`canceled`；结果和取消入口都检查归属，写请求还要求准确 Origin 与 CSRF。跨会话访问返回 404。

未开始的上传一分钟到期，ready 删除计划十分钟到期，运行任务受 30 分钟会话限制。完成记录最多保留十分钟，容量压力可提前回收最早的完成记录；每会话最多 16 项、每进程最多 64 项，活动任务不会为了新任务被驱逐。

`GET /api/jobs` 可恢复当前会话的状态，但不恢复确认 token 或重新发送文件。页面导航保留任务原来的桶和 key 范围。任务记录被回收时，客户端另设“记录已过期”标记，不伪造服务器 canceled/succeeded 状态；页面保留最后已知信息，提示核验存储、停止轮询和取消入口、允许清理，并清除未执行的删除确认。集合缺失只与请求发出前已有的任务比较，晚到响应不会使已过期记录复活。

取消、登出、会话到期和进程停止都传播到对应传输及后台任务。离开页面会停止浏览器上传和轮询，但已确认的后台删除可继续执行；需要停止删除时，应明确取消任务或退出登录。取消会停止后续工作并尝试清理所属 multipart，已提交对象和已完成删除不会撤销。关闭路径分别给 HTTP 和后台清理五秒期限，最坏约十秒；正常取消的五秒预算需要实际测量，不能以最坏退出期限代替。

## 审查发现与已修正问题

本轮实施中先验证协议和 HTTP，再交叉检查前端、生命周期和验收脚本。以下修正均已由最终构建或直接回归覆盖：

1. **原子创建保证不足**：只有 HEAD 预检会出现检查后被其他写入抢占的竞态；补充服务端能力协商、目标锁内条件检查和 multipart 完成保护。并发写、空对象、multipart 冲突、写回缓存及不支持的拓扑分别设测试，不将能力扩展为任意第三方 S3 保证。
2. **取消与慢请求阻塞**：关闭浏览器请求体可能等待正在执行 Read 的锁；现在先设置 socket 读取期限再关闭。提前拒绝的请求也处理剩余 body，避免 handler 返回后的隐式 drain 阻塞。下载上游关闭不能解除下游 socket Write/Flush 阻塞，分别设置取消与空闲写期限。
3. **响应和完成确认边界**：元数据 XML 与 multipart 完成响应受整体期限约束；静态资源、JSON 和注销响应在有效期限内 flush。空下载明确 flush，未知长度下载保留写期限至 net/http 发完最终 chunk，再由服务器清除期限；真实连接测试覆盖阻塞、取消和连接复用。取消时的期限更新与写入同步，避免恢复一个已取消的请求。
4. **任务竞争和确认恢复**：计划取消/关闭后不再生成可执行确认；计时回调不让旧 waiting/ready timer 取消后来已启动的任务。空计划省略 `items` 时规范化为空列表。只有明确的 HTTP 429 `busy`、且服务端尚未消费确认的拒绝，才保留同计划供用户再次确认；网络和其他不确定错误不复用 token。
5. **前端权限与异步状态**：上传与准确 key 删除增加手填桶入口，覆盖只写用户无法列桶的情况；上传 PUT 固定 octet-stream；发送 100% 不显示保存成功。Refresh 可取消并重启慢读取，AccountInfo 文案不推断对象权限。pagehide 不因 XHR abort 回调重新启动轮询，返回页面只恢复状态、不重传文件。
6. **任务记录回收**：原页面只追加状态，丢失最终响应后被回收的任务可能永久显示 Running/Ready。新增客户端过期状态、缺失集合的请求前快照、晚响应保护、停止轮询与清理入口，并禁用已失效的删除确认。
7. **下载错误保留控制台**：原附件链接失败会把主页面导航到 JSON。现在独立标签打开下载，先核验本地会话；实际 GetObject 权限和对象不存在仍由新标签的真实 GET 报错。此预检不宣称已经验证上游下载授权。
8. **验收证据可靠性**：失败响应先检查凭据再输出，避免错误报告保存原始凭据；RSS 采样线程错误传回主线程，并要求单上传与双并发阶段都取得样本。前缀条件补允许侧成功计划，对保留期日期及历史版本内容补验证，取消耗时与后来核验/foreign 清理分开记录。CLI 超时异常隐藏原始命令参数与异常链；维护回归验证错误和 traceback 不含凭据。失败场景、已完成检查及采样失败前的性能数据也保留，报告记录脚本摘要。最终脚本已与最终二进制重新执行。

9. **上传吞吐未达门槛**：两次固定基线失败报告均保留。CLI 默认四 worker，原控制台在读取、MD5、SHA256 和分段发送之间串行等待。仅并行计算两个校验值不足；最终改为两个固定分段缓冲，让下一段读取和完整校验与上一段发送重叠。主流程始终独占读取请求体，上游最多一项 PUT，ACK 立即报告进度；所有返回先取消并等待发送 worker，再清理所属 upload ID。四项新增回归覆盖重叠、第三段缓冲复用、悬停读取时的 ACK、取消、清理顺序与上游错误保留。

交叉审查最后一轮在所检查的契约内未提出新增具体问题。这只描述本轮检查结果，不等于整个产品或所有部署场景不存在缺陷。

## 已保存的真实服务报告

两份报告都来自 macOS arm64、临时账号和临时单节点四盘 erasure 存储；包含平台、服务端来源、SDK pin、补丁名称及摘要、实际二进制 SHA-256。它们不是分布式或生产部署验收，也不是未打补丁的原始 server commit 结果。

- [当前服务端报告](console-phase-two-results.json)：来源 `c8a09caf06c56483e1d21a1c9cdedc3dc3b76381`，加 AccountInfo 和条件写补丁；`single-http`、`dual-http`、独立 CA 的 `dual-tls` 三场景均记录为 passed。
- [固定补丁基线报告](console-phase-two-pinned-results.json)：来源 `be8596f0d69d530586f35366fb2d5c79bdc54399`，加 core、runtime、HTTP API、AccountInfo、条件写五项兼容补丁；最终完整运行 status 为 passed，三场景均完成。失败版本另存，不能将它们与最终通过版本混用。

报告记录的功能覆盖包括默认只读、匿名/Origin/CSRF 拒绝、限额和任务归属、空文件/特殊 key/multipart 内容一致、显式覆盖、条件 PUT 冲突保留当前内容、固定前缀名单、新 key 保留、确认不泄露及重放拒绝、部分失败、STS 会话策略交集及无效 token、组策略继承/撤销、只写身份直接操作、版本 marker 与旧内容保留、对象锁桶的当前 key 删除、慢 multipart 取消与 foreign upload 保留，以及活动写入时停止进程的清理。

连续双斜杠 key 的客户端/HTTP/UI 保真由模拟合同覆盖；当前 OtterIO 对此对象名的限制不因 UI 原样编码而消失。65 MiB 内容一致和两个并发上传是短时单机回归，不能推断长期吞吐或所有 key 兼容。

### 最终性能证据与取舍

预算保持不变：65 MiB 单上传至少 5 MiB/s，且至少达到同机同服务端 CLI 的 50%；每次传输及双并发阶段不超过 120 秒，控制台采样峰值 RSS 不超过 512 MiB，取消及活动写关闭不超过 5 秒。为减少亚秒级测量波动，按 CLI→控制台交替三次，保存全部原始耗时并用中位数判定；采样贯穿两条路径，单上传每一轮和双并发阶段都必须有样本。不会取最佳一次或降低门槛。

- 当前服务端：single-http 382.3 MiB/s（CLI 526.8，72.6%）；dual-http 383.2 MiB/s（CLI 470.3，81.5%）；dual-tls 367.2 MiB/s（CLI 585.8，62.7%）。
- 固定五补丁基线：single-http 357.4 MiB/s（CLI 447.9，79.8%）；dual-http 389.2 MiB/s（CLI 548.8，70.9%）；dual-tls 365.3 MiB/s（CLI 548.9，66.6%）。

六场景采样峰值 RSS 为 83.6–108.5 MiB；取消 0.051–0.056 秒，活动写关闭 0.008–0.009 秒。所有上传/下载内容按字节比较，两项并发上传成功。

优化的代价是每次 multipart 缓冲从 16 MiB 增至固定 32 MiB，以及有界的校验/发送 worker；保留 MD5、SHA256 和顺序提交语义。上游分段失败若恰遇浏览器 Read 已阻塞，错误报告需等该读取释放，仍受 30 秒空闲期限约束；用户取消、登出及关闭通过父 context 立即中断读取。

历史失败证据：[原串行路径](console-phase-two-performance-before.json)约 41.7% CLI；[仅并行校验的中间版本](console-phase-two-performance-intermediate.json)约 42.7%，包含三次耗时与已完成检查。最终通过依赖代码优化和有证据的重复测量，两份失败不会被覆盖成成功。0.1 秒 RSS 采样不保证捕获瞬时峰值；短时本机结果不是生产吞吐或长期稳定性承诺。

## 最终局部回归与浏览器证据

[最终检查汇总](console-phase-two-verification.json)记录本轮通过项、最终二进制摘要及未运行范围。

OC 全量普通回归、新增传输/存储/HTTP/启动入口的 race、vet、golangci-lint、维护测试、格式/发布边界及工作流 YAML 检查完成。Linux amd64 与 Windows amd64 最终交叉构建完成；没有原生执行这两个平台。OtterIO 的条件写、AccountInfo、普通 PUT/multipart 完成和缓存相关定向回归通过，固定补丁应用及新增服务端测试通过；没有宣称整套 OtterIO 测试或远程 CI 已运行。

前端语法、静态资源嵌入和 DOM/XHR 模拟覆盖只写入口、原始 File、发送进度、覆盖确认、空计划、明确 busy、未知结果不重试、pagehide、任务 404/回收、并发新任务及晚响应保护。

[真实浏览器记录](console-phase-two-browser-results.json)验证了 57 字节合成文件上传、中文/空格/加号/百分号 key 显示、准确删除名单、关闭确认后零对象执行、浏览器重新加载后的任务恢复与注销。实际删除和大上传取消由真实 HTTP 矩阵执行；本次浏览器没有重复这些操作。前端资源与最终构建相同，浏览器记录保存资源与截图摘要。P1 已有下载器字节验证，P2 的上传/下载字节一致另由最终矩阵覆盖。

两个最终报告保存实际二进制及补丁 SHA-256、SDK pin、平台、三次耗时和验收脚本 SHA-256。远程工作流已配置但本轮未远程运行；无正式发行、分布式、第三方 S3 或长期 soak 的通过声明。

## 重现入口

源码构建和局部检查使用 `make build-console`、`make test-console`。真实写验收入口为 [`test-console-integration.py`](../buildscripts/test-console-integration.py) 的 `--writes`，写流程由 [`console_write_acceptance.py`](../buildscripts/console_write_acceptance.py) 执行；需要临时可写四盘服务、Python 标准库及 OpenSSL。

```sh
python3 buildscripts/test-console-integration.py \
  --cli /path/to/oc --console /path/to/oc-console \
  --server /path/to/otterio \
  --server-source VERIFIED_SOURCE_REVISION \
  --server-patch buildscripts/otterio-account-info-compat.patch \
  --server-patch buildscripts/otterio-conditional-writes-compat.patch \
  --writes --output /path/to/console-phase-two-results.json
```

固定 SDK 基线需先按[开发指南](development.md#reproduce-the-server-integration-fixture)应用 core、runtime、HTTP API 补丁，并在报告中额外传入这三项 `--server-patch`。`--server-source` 必须填写实际验证的源码来源；报告同时记录实际运行二进制摘要。preview 留住临时服务供人工浏览器测试，不计为自动验收完成。

## 后续范围与回退

本记录冻结时 P3 尚未实施；自身改密和桶设置的后续交付见[第三阶段记录](console-phase-three.md)，用户/组/服务账号管理和诊断仍待完成。P4 的多人身份隔离、OIDC/STS 自动续期、可信代理、集中 HTTPS 部署、原生平台矩阵和独立发行附件未完成；P5 的旧 UI 弃用、browser 默认关闭和旧资源/专用协议移除未开始。

本阶段也不提供版本选择 UI、Range、ZIP、预签名共享或跨 alias 切换。对象锁和版本行为的针对性回归不等于已具备完整的保留期、历史版本管理能力；单机通过不等于第三方 S3、多 pool、gateway、生产代理或长期并发验收。

遇到问题可以关闭 `oc-console`，继续使用原 OtterIO Web 或 CLI。程序回退不会恢复已覆盖的数据或已删除对象；操作恢复取决于存储的版本和保留设置，应与软件回退分开处理。P2 通过后仍须按后续阶段门槛推进，不能据此删除旧控制台。
