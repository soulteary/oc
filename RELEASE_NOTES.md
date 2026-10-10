# OC Console distribution and object management update

Changes since [RELEASE.2026-10-09T17-33-03Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-09T17-33-03Z)
(source `99c6728ee75d10aaa7b6932a0ec07764258aa4ca`). This preparation
uses UTC timestamp tags and does not reserve a tag or publish artifacts.

## Changes

- Distribute `oc-console` alongside `oc` in all eleven platform archives and
  Linux amd64/arm64 release images. Both programs report the release tag;
  packaging checks verify executable bytes, licenses and checksums. The image
  defaults to `oc`; start the console with `--entrypoint oc-console`.
- Improve console object management, navigation and localization. Add persistent
  preferences and recent bucket visits, with platform-appropriate preference
  file permission checks on Windows.
- Add a Docker Compose example for the local single-identity console. It sets
  `OTTERIO_BROWSER=off` while retaining the internal Admin listener. The example
  pins OtterIO `RELEASE.2026-10-09T15-22-07Z` and currently builds OC locally.
- Fix console formatting and error-string lint findings.

## Compatibility and upgrade

Go source builds require 1.27.2. This release retains the existing dependency
pins: S3 SDK `github.com/soulteary/otterio-sdk/v7 v7.3.2`, server/admin module
`github.com/soulteary/otterio v0.0.0-20261009091443-bcc238bc4c2c`,
`crc64nvme v1.1.3`, `md5-simd v1.2.0` and urfave/cli v3.14.0.
These build identities do not identify a remote server or establish compatibility
with every historical OtterIO or third-party S3 service.

**Known policy-update limitation:** `oc admin policy update` can clear a user's
or group's policy assignments when passed an already assigned policy or an empty
policy argument. This release does not fix that defect. Read the existing
assignments with `oc admin user info` or `oc admin group info`, then use
`oc admin policy set` with the complete desired comma-separated policy list and
verify the result. See [administration guidance](docs/administration.md).

Existing CLI configuration and `OC_*` / supported `MC_*` precedence remain.
Back up configuration and pin release images or manifest digests before upgrading.
Older archives and images are not retroactively updated with the console.

The console uses one configured alias identity for all local browser sessions.
Writes and sharing require explicit opt-in; protected operations also require
matching server capabilities and permissions. Independent-user/OIDC login and
Range downloads remain outside this scope. See the [console guide](docs/console.md)
and [migration and rollback gates](docs/console-release-migration.md).
OtterIO's browser default remains unchanged. Restoring `OTTERIO_BROWSER=on`
does not undo object writes, settings, secret rotation or lifecycle transitions.

Release from a clean synchronized main only after Go, Code scanning and the
separate CLI compatibility checks pass on that exact source. Verify thirteen
published assets and immutable image digests before rollout. Eleven cross-built
archives and two image architectures do not prove native execution on every
platform. See the [preparation record](docs/releases/2026-10-10-release-review.md)
and [release procedure](docs/releasing.md).

---

# OC Console 分发与对象管理更新

本次变化以 [RELEASE.2026-10-09T17-33-03Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-09T17-33-03Z)
为基线，源码为 `99c6728ee75d10aaa7b6932a0ec07764258aa4ca`。
继续使用 UTC 时间戳标签；本次准备不预留标签或发布附件。

## 本次变化

- 11 个平台归档和 Linux amd64/arm64 发行镜像同时包含 `oc` 与 `oc-console`。
  两个程序显示发行标签，打包检查核对程序字节、许可证和校验值。
  镜像默认入口仍为 `oc`，使用 `--entrypoint oc-console` 启动控制台。
- 完善控制台对象管理、导航和多语言界面，持久保存偏好及最近访问的桶；
  Windows 偏好文件检查按该平台的权限语义执行。
- 提供本机单身份控制台的 Docker Compose 示例，以 `OTTERIO_BROWSER=off`
  关闭旧 Web，同时保留内部 Admin 监听。示例固定 OtterIO
  `RELEASE.2026-10-09T15-22-07Z`，OC 当前仍使用本地构建镜像。
- 修复控制台格式与错误字符串的 lint 问题。

## 升级与兼容性

源码构建要求 Go 1.27.2。依赖继续固定为 S3 SDK v7.3.2、服务端 / 管理模块
`v0.0.0-20261009091443-bcc238bc4c2c`、crc64nvme v1.1.3、md5-simd v1.2.0
及 urfave/cli v3.14.0；它们不是远程服务端身份或历史版本、第三方 S3 的通用兼容证明。

**已知策略更新限制：** `oc admin policy update` 遇到已绑定的策略或空策略参数时，
可能清空用户或组的策略绑定，本次发布尚未修复。先用 `oc admin user info` 或
`oc admin group info` 读取现有绑定，再用 `oc admin policy set` 提交期望保留的
完整策略列表（多个策略以逗号分隔），并核对结果，见[管理说明](docs/zh_CN/administration.md)。

保留现有 CLI 配置和 `OC_*` 优先于受支持 `MC_*` 的规则。升级前备份配置，
固定版本或清单中的镜像摘要。已发布的旧归档和镜像不会补入控制台。
所有本机浏览器会话共用启动时的别名身份；写操作与分享需显式开启，受保护操作
还需服务端能力和当前身份权限。独立用户 / OIDC 登录及 Range 下载仍不在范围内。
见[控制台指南](docs/zh_CN/console.md)及[迁移与回退条件](docs/console-release-migration.md)。
OtterIO 的浏览器默认值不变。恢复 `OTTERIO_BROWSER=on` 不会撤销已完成的写入、
配置、改密或生命周期转换。

最终发布需从干净且同步的 main 开始，同一提交的 Go、代码扫描和独立 CLI 兼容
检查全部通过。上线前核验 13 个附件和不可变镜像摘要。交叉编译和双架构镜像
不能代替各平台原生运行验收，见[准备记录](docs/releases/2026-10-10-release-review.md)
与[发行流程](docs/zh_CN/releasing.md)。
