# OC client reliability and release update

This update includes the changes after Git tag `RELEASE.2026-10-07T04-39-33Z`.
That tag is a source baseline; it does not establish that GitHub release assets
were published. The release uses OtterIO's UTC timestamp tag format.

## Changes

- Fix Windows notification decoding without weakening race/checkptr checks.
  Preserve asynchronous read buffers until cancellation completes, close idle
  handles, and keep old completions from removing replacement watches. Report
  genuine event loss so live mirrors reconcile their state.
- Handle Windows filename limits, OS-blocked staging-directory replacement and
  unsupported filesystem attributes. Synchronize periodic mirror recovery with
  scan/copy worker shutdown.
- Harden configuration import, endpoint validation and configuration cache access.
  Keep JSON errors on one line with stable error codes/categories. Correct live
  mirror filtering and event handling.
- Validate object-lock durations and calendar bounds before arithmetic or copying
  data. Reject out-of-range retention dates before data transfer; retain supported
  clear requests and public interfaces.
- Update the Go vulnerability scanner for Go 1.27 and use the existing transfer
  deadline when observing throttled multipart progress. Keep cancellation,
  throughput and residual-session assertions.
- Add timestamp-based binary publication with exact-source main CI checks,
  eleven platform archives, SHA-256 checksums, source identity and licenses.
  Upload to a stable draft and verify every downloaded asset before publication.

## Compatibility and upgrade

The module path remains `github.com/soulteary/mc`. The OtterIO SDK remains pinned
to `v0.0.0-20261004215341-be8596f0d69d`; the compatibility fixture applies the
three recorded server patches. This release does not claim compatibility with
all historical OtterIO versions or every third-party S3 service.
See [the compatibility manifest](https://github.com/soulteary/oc/blob/main/docs/compatibility.json) and
[validation scope](https://github.com/soulteary/oc/blob/main/docs/oc-phase-four.md).

OC uses its own configuration directory. Back up configuration before importing
from mc; import replaces destination aliases after validation and a private backup.
`OC_*` settings take precedence over supported legacy `MC_*` settings.
MinIO self-update and SUBNET upload remain disabled. Replace the executable using
verified release assets and check `oc --version`; the Windows archive contains
`oc.exe`. Test aliases, TLS, copy/mirror filters and object-lock operations before
rollout. Notifications have no durable replay cursor.

Source builds require Go 1.27.1. Cross-compilation is not runtime acceptance for
every architecture. The Windows notification implementation retains its MIT
license alongside OC's Apache-2.0 license and upstream notices. The manifest
records identity, not a signature or attestation.

---

# OC 客户端可靠性与发布更新

本次包含 Git 标签 `RELEASE.2026-10-07T04-39-33Z` 之后的变化。该标签是源码比较基线，
不能作为 GitHub 附件已经发布的证明。新版本沿用 OtterIO 的 UTC 时间戳标签格式。

## 本次变化

- 修复 Windows 通知解码、空闲句柄释放、异步取消缓冲区生命周期和重复监听。
  保留竞态及 checkptr 检查，真正的事件丢失仍会触发镜像同步恢复。
- 处理 Windows 长文件名、操作系统阻止的暂存目录替换和不支持的文件属性；
  周期镜像恢复等待扫描与复制工作结束后再更新共享状态。
- 加固配置导入、端点验证和配置缓存访问，保持单行 JSON 错误及稳定的错误代码/类别，
  修复实时镜像过滤与事件处理。
- 在日期运算和复制前验证对象锁保留期限，拒绝超出可表示日历范围的日期，
  保留清除保留配置的请求和公开接口。
- 更新 Go 漏洞扫描器，分片进度观察采用既有传输期限，继续保留取消、吞吐和残留会话检查。
- 新增时间戳标签二进制发布：要求同一源码的 main 检查成功，构建 11 个平台归档，
  附带 SHA-256 校验文件、源码身份清单与许可证；草稿附件逐个下载比对后再发布。

## 升级与兼容性

Go 模块路径仍为 `github.com/soulteary/mc`，OtterIO SDK 仍固定为
`v0.0.0-20261004215341-be8596f0d69d`，兼容性测试服务端应用已记录的三个补丁。
本次不扩大为所有历史 OtterIO 或第三方 S3 均兼容的承诺，具体范围见
[兼容性清单](https://github.com/soulteary/oc/blob/main/docs/compatibility.json)和[验收范围](https://github.com/soulteary/oc/blob/main/docs/oc-phase-four.md)。

OC 使用独立配置目录。导入 mc 配置前先备份；导入在验证和私有备份后替换目标别名。
`OC_*` 优先于继续支持的旧 `MC_*` 设置。MinIO 自更新与 SUBNET 上传保持禁用。
使用经过校验的发布附件替换可执行文件，并检查 `oc --version`；Windows 使用 `oc.exe`。
部署前验证别名、TLS、复制与镜像过滤、对象锁操作。通知没有持久化重放游标。

源码构建要求 Go 1.27.1；交叉编译不能代替全部架构的运行验收。
归档同时保留 OC 的 Apache-2.0 许可证、上游声明和 Windows 通知实现的 MIT 许可证。
身份清单不是签名或供应链证明。
