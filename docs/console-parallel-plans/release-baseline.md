# 正式发行配对基线：2026-10-11

本记录属于计划 08 首批验收，不构成最终迁移验收或旧 Web 删除授权。

使用 GitHub 正式发行资产，在独立临时数据目录验证，无现有部署或 alias 被修改：

- OC `RELEASE.2026-10-10T11-42-18Z`，源码 `ee1846d587a6548db4e595d653ca4f54baea2b1a`。
- OtterIO `RELEASE.2026-10-10T16-38-05Z`，源码 `8cc518b31ac8b69c8f855e19b4bd027f44275c47`。
- macOS arm64；验收程序来自 OC 集成基线 `0a162c82`。
- 下载 OC 压缩包 SHA-256 `013b08e1c0d66f33202b8fe8e0705d95e0caa0358511ad924bae5b08ab00ca22`，服务端二进制 SHA-256 `48b673ad0e03723eeeedb2ab5773569093b426d8fa04e132d17183ac7afb6f9c`，均与发行校验文件一致。解包后的二进制信息与摘要见机器报告。

## 已验证

`single-http`、`dual-http`、`single-tls`、`dual-tls` 四种组合分别完成：

- 写入与设置：每种 33 组，共 132 组，见 [设置报告](release-pair-settings-results.json)。
- 存储功能与版本复制：每种 19 组，共 76 组，见 [功能报告](release-pair-features-results.json)。正式服务尚不支持条件 IAM 创建，该路径使用 `--features-iam-create-unsupported` 验证显式拒绝，不能计为 IAM 创建成功。

验收调用 `buildscripts/test-console-integration.py`，两次均指定 `--cli`、`--console`、`--server` 指向上述发行资产，以及 `--server-source` 指向服务端源码。设置报告使用 `--writes --settings`；功能报告使用 `--features --features-iam-create-unsupported --version-copy`；两者均指定四个场景。脚本自动创建并清理独立夹具。

## 仍需完成

此次没有验证其他操作系统/架构、Docker 运行时或多实例部署。没有将源码中新增的原生 IAM 登录、OIDC/STS、Range 与诊断能力视为已发行能力。后续必须用包含集成变更的新正式 OC 发行重新跑完整矩阵，补齐身份、权限、会话撤销、反向代理与稳定性验收。

OtterIO 当前只是首个包含旧 Web 弃用通知的正式发行；至少两个已通知发行周期与约定的回退窗口仍须满足。此次配对成功不改变旧 Web 默认值或入口。
