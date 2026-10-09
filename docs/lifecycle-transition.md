# 生命周期转换执行与恢复

日期：2026-10-08。承接 [P3 桶设置](console-phase-three.md)，实现此前延期的当前/非当前版本转换、持久化目标、指定版本恢复及删除保护。旧 Web 仍保留；用户/组/服务账号管理、诊断和发行不属于本次交付。

## 接入方式

OC 继续使用固定的独立 SDK 和服务端依赖，没有修改 `go.mod`、`go.sum` 或模块缓存。固定服务端 `6f6d0835ddff68020f1491c403b958fade22841f` 尚不包含新运行时；可使用已包含 P3 的 OtterIO 当前源码 HEAD，或在固定版本的独立源码副本先应用
[基础协议](../buildscripts/console-server-base.patch)，再应用
[生命周期存储](../buildscripts/lifecycle-storage.patch)，再应用必带的
[生命周期存储加固](../buildscripts/lifecycle-storage-hardening.patch)。基础协议独立提供设置比较交换和自身改密；
存储core补丁追加转换、恢复、目标索引和删除保护；hardening保留HEAD80已合入的
12个storage-class snapshot、覆盖quorum和tier元数据保护源码/测试文件。
它让配置更新与Snapshot读取共享锁；普通旧元数据不足读quorum时仍可按写quorum覆盖，
但任何可读的partial tier引用都会阻止覆盖。RenameData按版本保留目标引用和删除intent，
允许同源恢复与pending→complete，并修复取消磁盘monitor和测试配置恢复。
旧 `console-server-p3.patch` 冻结为历史快照，
不再是当前部署入口。补丁可应用不代表协议已发布或已更新 OC 的服务端 pin。

在 CLI 注册有 label 的 ILM 目标，再在 OC 生命周期编辑器保存完整 XML。当前和非当前版本可以使用不同目标；同一桶内 label 不区分大小写且唯一。

```sh
oc admin bucket remote add store/source \
  'https://ACCESSKEY:SECRETKEY@tier.example/archive' \
  --service ilm --label ARCHIVE --path on
oc admin bucket remote ls store/source --service ilm
```

CLI 的 ILM 添加、编辑、移除会先发送签名的只读管理请求，要求服务端明确返回 `X-Otterio-Lifecycle-Transition: v1`，再执行修改；缺失、未知、重复标识、重定向和不完整回复均拒绝。此检查需要 `admin:GetBucketTarget` 权限，修改仍需原管理权限。默认配置 CA 与已有管理客户端一致，显式管理 CA 独立覆盖。复制目标管理保留原流程；固定的旧服务端没有原生目标列举路由，不能把旧服务端列举失败当作无目标的证明。

```xml
<LifecycleConfiguration>
  <Rule>
    <ID>archive-old-versions</ID>
    <Status>Enabled</Status>
    <Filter><Prefix>documents/</Prefix></Filter>
    <NoncurrentVersionTransition>
      <NoncurrentDays>30</NoncurrentDays>
      <StorageClass>ARCHIVE</StorageClass>
    </NoncurrentVersionTransition>
  </Rule>
</LifecycleConfiguration>
```

条件写入支持 `NoncurrentDays`、`StorageClass`，仍拒绝 `NewerNoncurrentVersions` 和未知扩展。两种已支持的生命周期 XML 根都可完整读取；错误体、多根、DTD、尾随文字及截断不能成为可信的配置或提交确认。

恢复通过原生 S3 `POST ?restore&versionId=...` 发起；普通请求使用 S3 namespace 的 `RestoreRequest` 与正整数 `Days`。首次返回 202，已完成且未到期的副本可延长期限并返回 200，重复进行中的请求返回冲突。OC 此前尚未交付历史版本浏览与恢复按钮，这里交付的是存储执行能力。

## 执行与安全语义

- 全部动作统一过滤前缀和解码后的实际标签。已到期的永久删除优先于转换；转换按最早到期动作选择目标，同时间保留文档顺序。非当前版本的等待起点是后继版本时间。
- 每个源版本先持久化远端 ARN、独立随机 key、远端 versionId 和 storage class，再上传。当前版本后来变成非当前、规则删除、缓存丢失或源服务重启，都继续使用该引用。
- 上传前重新检查源身份，上传与提交期间持有源对象锁；元数据达到写 quorum 后才删除本地内容。上传失败保留 pending 和源数据；确认丢失后的重试通过远端尺寸、专用归属标识及实际版本确认已上传对象。
- 已完成且应从远端读取的对象，不因不同磁盘残留的 inline 数据长度而丢失读取 quorum。等待转换和有效本地恢复副本仍要求本地数据一致；标签、目标引用等元数据冲突仍拒绝读取。
- 目标索引在 pending 前保存，目标移除、改地址或改 label 会检查启用规则和准确源版本引用；即使规则已删除，也不能切断存量数据。原地址的凭据轮换和其他不改变位置的更新可以继续，公开列举不返回 secret 或 session token。
- 恢复开始先持久化进行中状态和请求参数。失败或重启后扫描器继续处理，即使生命周期已移除。恢复保留源身份、原始元数据和逻辑多段布局，覆盖加密、压缩多段对象的完整与范围读取。
- 实际恢复提交成功后，由共同路径发送完成通知；API 后台任务与扫描器竞争不会重复通知。事件使用提交后的准确版本，过滤私有转换元数据，并保留原对象元数据。
- 恢复副本到期仅清除本地缓存，保留远端引用；不因对象保留策略把本地副本永久留下。永久删除仍重新检查实际保留策略。
- 单删和批量永久删除先清理准确远端版本，再移除源引用。远端不可达时返回失败并保留恢复依据。普通版本化删除及当前版本到期创建删除标记，保留原版本；暂停版本配置下替换 null 版本时先清理该 null 对应的远端内容。
- 永久删除在操作远端前持久化与准确目标绑定的删除意图。远端删除成功而源提交失败、重启或规则随后移除时，扫描器继续完成已开始的删除；损坏或错配的意图不会授权删除。已经开始的永久删除不能靠修改规则撤销。
- 排队的到期动作在执行前重新读取实际规则、版本配置和源身份，并与配置修改互斥。删规则、延长期限、改标签或覆盖源对象，不会继续执行旧队列的删除决定。

索引限 4 MiB、10000 项。容量接近上限时分批核对真实源版本，回收已删除历史项；活动引用和无法确认的项不被驱逐。失败时采用保留源数据和引用的策略，而非猜测其他目标。

## 边界与部署代价

新转换协议限定单节点、单 pool erasure。FS、gateway、多 pool、分布式和未知实现不宣告该保证；不支持的 ILM 管理拒绝修改。普通 SELECT restore 的结果持久化尚未实现，明确返回 501，不再伪造成功。外部 S3 提供者的版本分配、保留策略与异常重试尚未完成互操作验收。

历史转换记录缺少持久化目标时，只允许从仍存在且无歧义的规则推导；旧目标没有索引时保守固定，不能自动证明历史引用已清空。原配置已漂移的历史数据需要人工核对。非版本对象和 null 版本转换后，覆盖会明确拒绝；需先显式删除再上传，以免丢失旧远端引用。

代价包括每版本引用及桶索引、转换期间同 key 写入等待、配置修改与到期任务短时互斥，以及目标不可达时删除失败。实际磁盘格式的既有 FileInfo tuple 未变，但旧服务端不理解新的远端位置：存在新转换引用时不能直接降级服务端。停止 OC 或退回原 UI 可继续使用当前服务端；软件回退不会撤销已保存规则或转换数据。

## 验证与重现

旧 monolithic 构建、补丁等价性和测试结果见 [历史验证记录](lifecycle-transition-verification.json)。原 [独立进程验收](lifecycle-transition-integration-results.json)使用临时四盘源端和独立四盘归档端，实际扫描器完成转换；覆盖 HTTP 与独立管理端口 TLS、规则删除、重启、指定版本恢复、远端不可用、精确删除、目标保护及凭据轮换。加密/压缩多段与故障注入由真实 erasure 的定向 race 测试覆盖。

```sh
python3 buildscripts/test-lifecycle-transition-integration.py \
  --cli /path/to/oc --console /path/to/oc-console \
  --server /path/to/otterio-with-lifecycle-storage \
  --server-source 6f6d0835ddff68020f1491c403b958fade22841f \
  --server-patch buildscripts/console-server-base.patch \
  --server-patch buildscripts/lifecycle-storage.patch \
  --server-patch buildscripts/lifecycle-storage-hardening.patch \
  --output /path/to/lifecycle-transition-integration-results.json
```

当前工作流分别构建base、base + versions + IAM、base + storage + hardening和
五层完整组合；两个生命周期程序各自调用同一独立源端/归档端harness并单独归档。
2026-10-09 macOS arm64的[硬化三层进程报告](lifecycle-storage-integration-results.json)
在HTTP/TLS各9组通过。旧[core-only报告](lifecycle-storage-core-results.json)保留原
二进制的HTTP/TLS各9组通过身份；[完整五层组合报告](console-combined-lifecycle-results.json)同样在独立归档端的HTTP/TLS各9组通过。

OtterIO `docs/console-server-split-results.json` 的逐字等价只比较固定pin的base + storage
core与冻结P3。`docs/console-server-hardening-results.json` 另验证必带hardening后的
独立build及cmd、lifecycle、storageclass native race；最终组合对“current + IAM”
projection的Go/根module等价见 `docs/console-hardening-equivalence.json`，不包含嵌套
Mint module差异，也不把缺独立IAM的实际current称为完整组合。

runtime报告的 `serverPatches` 保存真实应用顺序。三层部署采用base、storage、hardening；
完整真实程序采用base、storage、versions、IAM、hardening，而CI先加hardening。版本
native证明核对两顺序内容相同。所有runtime记录程序buildInfo和传递helper摘要；模块
副本服务端来源仍是声明值，不能由缺VCS的程序推导Git revision。最终汇总为OtterIO
`docs/console-server-acceptance-results.json`，五个独立profile均已通过。

历史P3、core-only报告和原中间失败保留原二进制与补丁摘要，不能移作当前三层部署
的通过证据。完整OtterIO测试、分布式、外部提供者、磁盘耗尽、长期soak及远程CI
仍需独立结果；交叉编译不代表目标平台原生运行通过。
