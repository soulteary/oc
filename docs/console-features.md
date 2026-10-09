# 控制台五项功能补全

[中文使用指南](zh_CN/console.md) · [English usage](console.md) · [迁移计划](console-migration.md)

本次实现位于独立的 `oc-console`，保留启动时选定的一个存储身份、本机回环监听、
本机会话和显式写开关。运行无需 Node 或新的前端依赖；默认 CLI 构建与既有 GHCR
镜像内容不变。

## 实现范围

1. 创建存储桶与删除空桶界面：可手填已知桶名，不要求列举全部桶；保留已选桶预填、写入开关与准确桶名确认。服务端检查空桶，不提供强制清空。
2. 用户、组与服务账号管理：原生 IAM 列表、创建、启停、成员、密钥轮换、服务账号限制策略与删除；用户/组命名策略绑定使用原子 revision 比较交换。
3. 对象历史版本分页与选择：指定版本下载、分享、归档；区分 `null` 和删除标记，拒绝隐式回退。
4. ZIP 下载：固定名单、条件读取、完整文件生成后一次下载，路径映射清单、体积上限、取消与会话清理。
5. 预签名 GET 分享：单独显式开关、接收者实际地址签名、过期时间与附件文件名。

实现边界由 `internal/consoleapi/features.go`、`internal/consoleapi/iam.go` 定义，
SDK 调用集中在 `internal/storageclient`，HTTP 与任务生命周期位于
`internal/console`，界面直接内嵌于 `internal/console/web`。

## 服务端协议

固定服务端来源为 `6f6d0835ddff68020f1491c403b958fade22841f`。在独立临时源码副本中
按顺序应用下列补丁，再构建五功能服务端。基础协议与生命周期存储实现分开，
五功能优先验证 `base + versions + IAM`，无需叠加 `lifecycle-storage.patch`。
随后在另一份源码与程序上验证 `base + storage + hardening + versions + IAM` 组合。
OC 不会修改相邻的 OtterIO 工作区。

```sh
git apply /path/to/oc/buildscripts/console-server-base.patch
git apply /path/to/oc/buildscripts/console-features-server.patch
git apply /path/to/oc/buildscripts/console-iam-bindings.patch
go build -mod=readonly -trimpath -o /path/to/otterio-console-features .
```

版本补丁分开授权当前与历史对象，GET、HEAD 与复制源使用规范化后的实际
`versionId`，并让 `s3:VersionId` 条件使用这个值。空值与纯空白仍表示当前对象；
显式 `null` 是历史版本值，不能隐式回退。
`X-Otterio-Version-Authorization: v1` 表示服务端具备此行为；缺少能力时控制台拒绝
历史读取和分享。版本列表、读取、公开签名链接均使用所选账号，不能代借 root 权限。

版本/对象标签条件已修正：`s3:ExistingObjectTag/<key>` 只取实际存储的所选对象/版本
标签，header、query 与 JWT claims 不能冒用该值。`RequestObjectTag` 按具体操作实际将写入的标签派生，详见本轮复核。
GET/HEAD 和复制源在读取实际元数据后复核授权，先于条件响应、对象元数据输出及
目标复制提交。已有 Get/Put/DeleteObjectTagging 继续使用原 S3 API 和动作；
PUT/DELETE 在对象写锁内再次按实际标签授权，覆盖并发标签修改。
这些修正属于版本/对象授权补丁，基础 P3 base 的范围不变。

修正前的 OtterIO `docs/console-version-copy-proof.json` 记录 554 个原生 HTTP 场景，
对应旧版本补丁和程序身份，仅作为历史证据。它不能证明本轮 XML/COPY 请求标签、
缓存、Range/tier 错误顺序和能力门控修正。最新范围与报告入口见[本轮授权复核](#本轮授权复核)。
目的 `versionId` 不能授权复制源读取；源权限拒绝或已有目的参数解析拒绝均不得产生
目标对象或分片。

IAM 绑定补丁提供签名的 `/otterio/admin/v3/iam-policy-bindings` GET/PUT，
在 IAM 存储锁内验证目标、策略存在性和 revision，再执行整体替换。
revision 包含使用进程随机密钥生成的身份与成员关系指纹，密钥、状态或成员发生变化
后需重新读取；重启服务端也会使旧 revision 失效。secret 不作为公开口令校验值返回。
原生 SDK 的无条件 `SetPolicy` 不用于此界面。旧协议不开放绑定修改。

同一补丁的 `/otterio/admin/v3/iam-create` 提供条件新建用户/组，
`X-Otterio-IAM-Create: v1` 宣告锁内重名检查。并发创建只允许一个请求成功，
冲突不修改已有 secret、组成员或策略；旧协议不将创建静默替换成改密或成员编辑。

`/otterio/admin/v3/iam-user-secret` 是独立的用户密钥轮换协议，
`X-Otterio-IAM-User-Secret: v1` 宣告锁内仅更新已有原生用户的 secret。
它保留实时状态、策略与组关系，拒绝不存在、root、STS 和服务账号目标，
避免无条件用户 upsert 重新启用或重建账号。旧服务端拒绝此管理改密操作。

## 本机启动

```sh
make build-console
./oc-console --alias store --address 127.0.0.1:9090 \
  --allow-writes --allow-sharing --share-url http://127.0.0.1:9000
```

`store` 需先通过 `oc alias set` 配置；管理 API 有独立地址时另设 `--admin-url`。
上述分享地址用于同一台机器，跨机器分享应填写接收者能访问的真实 S3 根地址。
macOS/Podman 下可以在 macOS 原生运行 `oc-console`，连接已映射到宿主机的 S3/Admin
端口。上述控制台使用 9090，避免占用原有 Admin 的 9001 映射。已有 CLI 镜像不包含本程序，新增功能不会自动出现在旧镜像中。

ZIP 默认最多 1000 个对象、5 GiB 源内容与一个活动/待下载任务；
`--max-archive-size` 可调低，`--archive-dir` 可指定私有临时存储位置。
每会话最多保留 16 条归档记录，完成和取消记录也计入配额，十分钟到期后释放；
连续准备小归档达到配额时，需要等待记录到期再创建。
当前对象保持当前读取权限并以 ETag 固定字节，只有显式历史选择使用版本权限。
刷新页面会从 `GET /api/archives` 恢复当前会话的活动归档；创建或取消响应丢失时
先查询任务，不重复准备或取消提交。其他会话不能查看该列表或下载其中任务。

## 本轮授权复核

本轮服务端版本补丁为 `buildscripts/console-features-server.patch`，SHA-256：
`ea620befaad0c09abc0f91396c04c1d7666e527ec7ed24e5916fb14bedcc1d1c`。base、storage、hardening 与 IAM 补丁均未改。
旧版本补丁 `c938ec24d3bd48b0eed7633abe876df9dda3cee6b5facd08833aa49a6e772d4f` 原样保存在
[修正前补丁](console-features-evidence/console-features-server-before-review.patch)。


本次测试辅助函数 lint 跟进只调整 context 参数顺序及未使用的 bucket 参数，完整 lint 为 0 issues，受影响的 198 个 HTTP/race 用例通过，见 OtterIO [lint 跟进记录](https://github.com/soulteary/otterio/blob/main/docs/console-authorization-lint-results.json)。下列完整运行报告仍对应修正前 `259313` 补丁及原程序；该补丁原样保存在[lint 前补丁](console-features-evidence/console-features-server-before-lint.patch)。当前补丁应用顺序及源码等价另有 OtterIO [静态记录](https://github.com/soulteary/otterio/blob/main/docs/console-authorization-lint-static-results.json)。

本轮按实际读写的数据重新绑定标签条件：

- PUT ObjectTagging 的 `RequestObjectTag` 来自解析后的 XML；授权与最终写入使用同一组标签。
- CopyObject 的 COPY/默认指令使用所选源对象的实际有效标签；REPLACE 使用解析后将写入的请求标签。复制源的 `ExistingObjectTag` 始终取所选源版本的存储标签。
- UploadPartCopy 不写入对象标签，不能把未写入的 tagging header 当作请求标签参与授权。
- 缓存 GET 在授权前读取后端权威元数据，以实际标签和完整时间精度核对快照；HEAD、显式版本、分片和 Range 读取直达后端，避免缓存 key 缺少这些身份信息。
- Range、删除标记与远端 tier 引用错误先按选中的元数据授权；回调已写出 `PreConditionFailed` 响应后立即结束并关闭读取，不再尝试副本代理。
- 版本能力只由受支持且非空的 FS/erasure 与已知缓存实现宣告；gateway、未知实现和空值不宣告 GET/HEAD 或版本列表能力。

受保护的 HTTP GET 即使命中缓存也要先做权威 metadata stat。后端元数据不可用时
没有离线缓存回退。该保证以本次 stat 的快照为边界：之后的标签变化不会撤销
已经开始的读取，也不承诺在整个响应期间持有对象锁。增加的 metadata 请求与
HEAD/版本/分片/Range 绕过缓存属于本次修正的代价。

控制台删除空桶现支持没有 `ListAllMyBuckets` 权限的用户输入已知桶名，仍需精确
确认并开启写入。ZIP 验收新增 current-only/history-only 的当前/指定历史读取正反例，
混合可读与不可读引用必须整包失败、拒绝下载并释放任务槽；Go 用例还验证已经
写入部分 ZIP 后再被拒绝时的临时文件清理。

最终原生构建与 race 验收全部通过：current 和完整组合各覆盖 788 个 HTTP 叶子
用例，base + versions + IAM 覆盖 776 个；base 明确跳过依赖生命周期存储的
12 个 tier 用例，未将其计入通过数量。Web 行为 61/61、ZIP 局部 Go race 及以下
五份真实服务验收也全部通过，结论限定于报告记录的来源、补丁和程序：

- [独立 base + versions + IAM 五功能](console-review-base-features-results.json)：单 HTTP、双 HTTP、双 TLS 各 19 组通过；每个场景含 ZIP 权限 4 例、版本复制 124 例。
- [完整五层五功能](console-review-combined-features-results.json)：相同三个场景各 19 组通过；每个场景含 ZIP 权限 4 例、版本复制 124 例。
- [实际 current 版本/复制](console-review-current-version-results.json)：三个场景各 12 组通过，每个场景含版本复制 124 例；实际 current 未安装独立 IAM 扩展。
- [完整五层设置/对象](console-review-combined-settings-object-results.json)：三个场景各 32 组通过。
- [完整五层生命周期](console-review-combined-lifecycle-results.json)：单 HTTP、双 TLS 各 9 组通过。
- OtterIO [本轮完整汇总](https://github.com/soulteary/otterio/blob/main/docs/console-authorization-review-results.json)与[最新验收入口](https://github.com/soulteary/otterio/blob/main/docs/console-server-acceptance-results.json)。

静态导出已核对 `base + storage + hardening + versions + IAM` 与
`base + versions + IAM + storage + hardening` 两种顺序，并与独立 current + IAM
副本比较：1,031 个 Go/根 module 路径逐字一致。base 可独立应用，模块文件不变；
静态结论限定于上述 Go/根 module 源码等价；构建与运行通过由前述新报告另行支持。
实际 current 与含 IAM 的组合仍是不同来源，嵌套
Mint module 的既有差异也不属于这个 Go/根 module 等价范围。

## 验证入口

```sh
make test-console
go test -mod=readonly -race ./internal/clienttransport ./internal/storageclient \
  ./internal/console/... ./cmd/oc-console
go vet -mod=readonly ./internal/clienttransport ./internal/storageclient \
  ./internal/console/... ./cmd/oc-console
python3 buildscripts/test-console-integration.py \
  --cli /path/to/oc --console /path/to/oc-console \
  --server /path/to/otterio-console-features \
  --server-source 6f6d0835ddff68020f1491c403b958fade22841f \
  --server-patch buildscripts/console-server-base.patch \
  --server-patch buildscripts/console-features-server.patch \
  --server-patch buildscripts/console-iam-bindings.patch \
  --features --version-copy --output /path/to/console-review-base-features-results.json
```

验收使用独立临时配置、凭据、存储目录与本机进程，不读取用户配置或接触现有桶。
结果只适用于报告记录的源码、补丁和程序摘要。

单元与并发用例覆盖异常/截断读取、对象条件变化、ZIP 临时文件与槽释放、
被阻塞源取消、慢速下行、会话隔离、精确确认、跨站请求拒绝、密钥清理以及
丢失响应不重放。界面行为测试当前共 61 组。用户列表明确区分已知空组关系和
未返回组关系，删除前另用用户详情接口验证，不把未知当作没有关联。

以下为 2026-10-09 旧 monolithic P3 组合的本地 macOS arm64 历史验收，
不能用来证明本次拆分后的 base 独立构建或组合构建已通过：

- [配套服务端验收](console-features-results.json)：单端口 HTTP、双端口 HTTP、双端口 TLS，每个场景 15 组通过。
- [旧协议兼容验收](console-features-legacy-results.json)：相同三个场景，每个场景 14 组通过；历史版本与受保护 IAM 操作拒绝缺少协议的服务端。
- OC 控制台相关 Go 并发测试、静态检查及 60 组模拟 DOM/fetch 行为测试通过；临时服务端的版本授权与原生 IAM 协议并发测试通过。
- 该历史运行对应源码完成 Linux arm64 与 Windows amd64 编译检查；实际运行使用 macOS arm64，不能据此声明新程序完成跨平台验证。

两份报告使用同一份 CGO 关闭的 `oc-console`，其 SHA-256 为
`cff76bbedb99c38f2a7175f642506c67eddb646e1adf77073957e96f1b5489c5`。报告同时记录 SDK pin、补丁及验收脚本摘要；
服务端来自声明的固定模块源码副本，没有可从程序中验证的 Git revision，报告明确保留此区别。
报告保留原名与原内容。当前 CI 已切换到拆分补丁和独立构建；上述通过结论只属于历史运行。

## 修正前拆分验收记录

`console-server-base.patch` 提供桶配置比较交换、自身 IAM 密钥轮换和必要的元数据事务。
生命周期部署必须包含base、`lifecycle-storage.patch` 与 `lifecycle-storage-hardening.patch`，
追加转换、恢复、目标索引、删除保护，并保留HEAD80已合入的12个存储加固源码/测试文件。
base条件生命周期仅接受前缀到期和非当前到期，拒绝标签过滤、转换与到期删除标记；
已有配置GET保留全文，普通无条件S3路径沿用pin行为。旧 `console-server-p3.patch`
冻结为历史快照，当前部署和CI不再应用它。

以下 2026-10-09 macOS arm64 报告均产生于本轮授权修正之前，保留原始来源、补丁、程序与传递 helper 摘要。其 passed 只属于当时的运行，不能替代上列新报告：

- base独立设置：[报告](console-base-settings-results.json)的HTTP、双HTTP、双TLS各20组通过；[独立对象报告](console-base-object-results.json)相同三个场景各21组通过，含字节、五组配对吞吐、RSS和取消门槛。
- base + versions + IAM：[五功能与复制报告](console-base-features-results.json)三个场景各18组通过，独立于生命周期存储。
- base + storage + hardening：[生命周期报告](lifecycle-storage-integration-results.json)的HTTP/TLS各9组通过，使用独立四盘源端与归档端。
- 五层完整组合：[五功能与复制报告](console-combined-features-results.json)三个场景各18组通过；[设置/对象报告](console-combined-settings-object-results.json)各32组通过，包含既有性能门槛。[完整组合生命周期报告](console-combined-lifecycle-results.json)的HTTP/TLS各9组通过。
- 实际OtterIO HEAD80 modified：[版本复制报告](console-version-copy-results.json)三个场景各12组通过；它未应用独立IAM补丁，报告没有重复声明补丁应用。

三份真实复制报告在每个场景各发出124次CopyObject/UploadPartCopy请求：50次确认准确
复制字节，74次拒绝且不生成目标对象/分片。真实进程采用SigV4和匿名桶策略；
当时的 V2、V4 预签名和已有标签写锁回归由 OtterIO 的修正前 554 场景 native/race 记录覆盖，不能据此宣称本轮新增分支通过。
[验收器清理证明](console-harness-cleanup-results.json)验证缺少程序会替换旧passed报告，
并在观察到OC console及独立服务启动后发送SIGTERM；所有观察到的子进程退出，临时
目录移除，未强制清理。早期仅观察server reexec的证明保留为superseded。

OtterIO `docs/console-server-split-results.json` 证明固定pin的base + storage core
与冻结P3逐字等价、module不变及base对象路径边界；旧[core-only runtime](lifecycle-storage-core-results.json)
按原二进制身份保留。`docs/console-server-hardening-results.json` 则独立构建必带hardening
的lifecycle，并通过cmd、完整lifecycle与storageclass包的native race。

该历史完整组合真实构建顺序为base、storage、versions、IAM、hardening，报告的 `serverPatches`
保留该实际顺序；CI先应用hardening再追加versions/IAM。OtterIO
`docs/console-version-copy-proof.json` 证明两种顺序内容相同，
`docs/console-hardening-equivalence.json` 证明当时组合与独立“current + IAM”projection
的1028个Go/根module路径逐字一致。实际current仍缺独立IAM，嵌套Mint的两个module
文件仍不同，不能宣称整个current checkout与完整组合全等。

模块副本构建的服务端仍是声明来源而非可从程序验证的 Git revision；历史 actual current
报告验证 HEAD80 基线并明确 `vcs.modified=true`。原汇总已保存在 OtterIO
[修正前验收汇总](https://github.com/soulteary/otterio/blob/main/docs/console-server-acceptance-before-review-results.json)。
旧 JSON 保留原摘要、身份和范围。完整复现见[开发指南](development.md#reproduce-the-optional-console-protocol-fixture)。

## 前轮界面遗漏复查（历史）

此前独立复查修正了五个实际问题：

- 历史权限与对象读取对空白 `versionId` 的解析不一致；现在动作选择、读取和版本条件共用同一个规范化值，新增 GET/HEAD 与 V4/V2/预签名回归。
- 无桶列举权限时历史界面不可达；现在可直接打开并手填桶和 key。
- 首次能力查询失败后 Refresh 无法恢复；现在 Refresh 会重试能力查询。
- IAM 创建同时返回新凭据并要求停止连接时，新 secret 被丢弃；现在停止后的独立只读窗口仍显示本次新凭据一次，关闭或离开即清空。
- 多行字段给只读的 `HTMLTextAreaElement.type` 赋值，导致组成员、策略绑定及服务账号策略表单中断；现在仅单行输入框设置类型，行为测试也模拟真实 textarea 属性限制。

服务账号验收增加了实际签名访问，验证禁用、旧/新 secret、策略内外对象及删除后的权限效果。
[修正前真实浏览器记录](console-features-browser-results.json)使用其报告记录的程序，确认多行表单与真实绑定读取、历史版本、分享生成、ZIP 准备和下载状态、桶创建及退出清理，未捕获 JavaScript 错误。
记录明确区分浏览器表单检查与后台实际写入验收；该历史浏览器运行未检查保存文件的字节或剪贴板权限，也没有验证本轮新删除界面。

原始 **GHCR 镜像 + macOS Podman Compose 控制台部署尚未交付**：发布镜像仍仅包含 CLI，
没有独立控制台镜像或 Compose 控制台示例。原 Compose 的 `OC_HOST_<alias>` 不能替代
控制台配置文件，且只监听回环地址的程序不能通过普通容器端口映射访问。
当前可用部署方式是本指南的 macOS 原生程序；镜像与 Compose 需要单独补齐。

## 保留的边界

控制台仍供一位操作员使用，所有浏览器会话共享启动时选定的存储身份。
root/目录身份的服务账号操作受现有服务端限制；服务端拒绝时界面直接报错。
FS 不支持版本桶；版本验收采用四盘单 pool erasure。
OIDC、多人身份隔离、集中 HTTPS 部署、控制台 Range 下载入口、历史版本恢复/永久删除、独立镜像发布
和旧 Web 入口弃用仍需单独实施。

停止或回退控制台不会撤销已提交的桶/IAM 修改，也不会使已签发分享链接失效。
删除、轮换或禁用操作不会自动重放，结果未知时应先用管理客户端核实。
