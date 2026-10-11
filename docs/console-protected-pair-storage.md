# 固定开发配对的存储路径验收

本轮使用最终固定开发配对，三种验收各执行一次，全部四种入口首次通过；没有失败重跑，也没有修改运行代码、验收代码或预期。结果不改变旧 Web 的发布/删除门禁。

## 程序与来源

- OC CLI / Console source：`a7ebefb03f8669cb93eb927a96f320a6e20229d4`。
- OtterIO source：`f2bdc23403ee0c4b35f9ce4092c68b80074349e1`，tree `302d29de473df997a704c0e58ab3e26419d44251`。
- 验收脚本来自独立 worktree `next-storage-validation` 的 OC `a7ebefb0`；本分支仅增加报告和说明。
- `/private/tmp/oc-cli-protected-pair`：SHA256 `202d29371a0d4bc3dfc84594225ff81b5dcaddff1096dee9fc405d34a7b6af89`。
- `/private/tmp/oc-console-protected-pair`：SHA256 `635b4b0703129fdb2d923e8e3fcd3d65cf34a703df5b26d5e42a5de2f9b9c400`。
- `/private/tmp/otterio-next-protocols-f2bdc23`：SHA256 `5937d44f4f54a7ae4c789c62e42e84e45012da1cfaed67b296af0be9ecc9eb6d`。

报告保存完整 Go BuildInfo；另逐份检查 CLI/Console revision 均为 `a7ebefb0`、server revision 均为 `f2bdc234`，全部 `vcs.modified=false`。harness 通过构建元数据校验 SDK pin 与 server revision。三份报告的程序 SHA256 相同，均对应固定开发配对，而不是较早的 `cd770aa` / `4b2ac9c` 资产。

## 范围与结果

- [writes 原始报告](console-protected-pair-storage-writes.json)：`passed`；single-http 22 groups, dual-http 22 groups, single-tls 22 groups, dual-tls 22 groups；启动记录 UTC `2026-10-11T20:22:11.633305+00:00`。
- [settings 原始报告](console-protected-pair-storage-settings.json)：`passed`；single-http 21 groups, dual-http 21 groups, single-tls 21 groups, dual-tls 21 groups；启动记录 UTC `2026-10-11T20:22:23.514426+00:00`。
- [version-copy 原始报告](console-protected-pair-storage-version-copy.json)：`passed`；single-http 13 groups, dual-http 13 groups, single-tls 13 groups, dual-tls 13 groups；启动记录 UTC `2026-10-11T20:23:53.478919+00:00`。

`--writes` 验证 opt-in 上传/删除、精确字节与 SHA256、组策略授权/撤销、版本删除标记、object-lock 保留、慢速 multipart 取消、归属上传清理及不相关上传保留；还执行 65 MiB、双并发、RSS 采样和既定五个反序测试块的吞吐门槛。原始计时和阈值全部保留在 JSON，不因并行夹具调整门槛。

`--settings` 验证 bucket CAS、policy/versioning/lifecycle 持久化内容与独立 revision、冲突零覆盖、Unicode/tagged lifecycle、unsupported 字段拒绝、普通用户/组策略/显式 deny/STS 的授权边界，以及自身密钥轮换响应顺序、旧密钥拒绝和 alias 不变。

`--version-copy` 使用真实 signed S3 CopyObject / UploadPartCopy 验证当前与历史源版本、版本与已存 tags 条件、null/空白处理、伪造 request tags/query 条件拒绝、授权先于源 precondition、目标隔离及复制字节；另验证 anonymous bucket policy 的当前/历史源区分。

每种模式同时运行基础浏览器登录/Origin/Host/CSRF、分页、特殊 key 下载散列、选定管理入口与 CA、只读和受限用户、logout 撤销及有界退出检查。

## 调用与隔离

三次调用仅 profile flag 和报告路径不同；每次 `--scenarios single-http,dual-http,single-tls,dual-tls`，使用以下固定参数：

```text
python3 buildscripts/test-console-integration.py
  --cli /private/tmp/oc-cli-protected-pair
  --console /private/tmp/oc-console-protected-pair
  --server /private/tmp/otterio-next-protocols-f2bdc23
  --server-source f2bdc23403ee0c4b35f9ce4092c68b80074349e1
  --scenarios single-http,dual-http,single-tls,dual-tls
  <一次选择 --writes / --settings / --version-copy>
  --output docs/console-protected-pair-storage-<profile>.json
```

writes 与 settings 最初并行；settings 完成后才启动 version-copy，因此至多两套夹具同时运行。每个场景使用独立 TemporaryDirectory、四个临时 erasure 数据目录、临时凭据/CLI 配置、随机 localhost 端口和临时证书。双监听 TLS 分别使用 S3 与 Admin 入口和对应 CA。无用户现有配置或持久数据参与。

## 清理证据

全部 12 个场景的 `processCleanup.status` 为 `passed`，`errors` 为空，记录的 writer/restricted writer/restricted console/console/server 均停止。每场景还验证 logout 会话拒绝、CLI fixture config 字节不变及 console 有界退出。

数据/证书/配置/log 文件由 `TemporaryDirectory` context 在场景返回前删除；场景最终 `passed` 表示该 context 正常退出。harness 不将临时目录名或 PID 写入报告，因此这里不声称存在逐目录/PID 独立清点；原始 JSON 给出进程清理结果，数据删除证据来自该既有 harness 的退出路径。

## 验收脚本身份

- `test-console-integration.py`：SHA256 `0a30f19c6354749ea00daa3bab171323b652d82aa8dfa8db17576d72cb27ea91`。
- `console_write_acceptance.py`：SHA256 `67e326e7f3e1ee484c1b2dc7e58bbb3583373ef29519bbe0680d48337e721c8c`。
- `console_acceptance_protocol.py`：SHA256 `2fad03751d8c3f9344bf28680b099c1661cb7296a2762fe5f4fd0777464a78e1`。
- `local_http.py`：SHA256 `8d9ae2287186726711458bff030de76f2d805dfa1a0be32d5354c41f721fe24f`。
- `console_settings_acceptance.py`：SHA256 `4aed1c4534aa6f8b823105879d91dffd78f1517a3c852d0e04435efdf00c9f21`。
- `version_copy_acceptance.py`：SHA256 `736d0f178c154781d17ce598f120bd49aa078218e95d5f1456cdf049c739756d`。
