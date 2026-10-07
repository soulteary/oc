# OC 配置文件

保留这个历史文件名作为已有链接的入口。当前配置说明见[配置指南](configuration.md)，导入与备份步骤见[迁移指南](migration.md)。

[文档目录](README.md) · [English](../minio-client-configuration-files.md)

Unix 默认目录为 `~/.oc`，Windows 为用户目录下的 `oc`。OC 不会隐式读取或覆盖 mc 配置。`--config-dir` 优先于环境设置，`OC_*` 优先于对应的受支持 `MC_*` 变量。

使用 `oc alias set/list/remove` 管理别名。`oc config import` 支持版本 10 的 mc/OC 配置，校验后备份目标配置并整体替换别名。证书和已保存的会话需要单独迁移。

不要在公开 issue 中上传完整配置文件，其中含有凭据。[安全说明](security.md)和[故障排查](troubleshooting.md)提供更合适的诊断方式。
