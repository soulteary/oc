# OC administration guide

This historical filename remains for existing links. Administrative operations now use `oc admin` against OtterIO; the former MinIO installation, update and support instructions are not OC procedures.

[Current administration guide](administration.md) · [中文管理指南](zh_CN/administration.md) · [Documentation index](README.md)

- [Configure separate S3 and management endpoints, and CA trust](configuration.md).
- [Read server information, manage identities/policies and collect diagnostics](administration.md).
- [Check deployment prerequisites and validation limits](compatibility.md).
- [Troubleshoot authentication, permissions and endpoint failures](troubleshooting.md).
- [Handle credentials and report security issues](../SECURITY.md).

Use `oc admin --help` and `oc admin SUBCOMMAND --help` to inspect your installed version. OtterIO's management API is distinct from generic S3 operations, and there is no `oc admin update` command.
