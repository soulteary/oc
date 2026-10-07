# OC configuration files

This historical filename is retained for existing links. OC configuration is documented in [the configuration guide](configuration.md) and [the migration guide](migration.md).

[Documentation index](README.md) · [简体中文](zh_CN/minio-client-configuration-files.md)

OC defaults to `~/.oc` on Unix and `oc` under the user profile on Windows. It does not implicitly read or overwrite mc configuration. `--config-dir` overrides environment settings; `OC_*` variables take precedence over supported `MC_*` variables.

Use `oc alias set/list/remove` to manage aliases. `oc config import` accepts version 10 mc/OC files, backs up destination configuration and replaces its aliases after validation. Certificates and saved sessions require separate migration.

Do not post complete configuration files in public issues: they contain credentials. [Security guidance](../SECURITY.md) and [troubleshooting](troubleshooting.md) explain safer diagnostic output.
