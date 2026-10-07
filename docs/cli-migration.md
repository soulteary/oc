# CLI framework migration

OC and its pinned OtterIO dependency use `github.com/urfave/cli/v3 v3.14.0`.
The exact module version and OtterIO source revision are recorded in
[compatibility.json](compatibility.json). The module path, Go toolchain,
configuration format and storage format remain unchanged.

## Preserved command behavior

The migration keeps the existing command names, positional arguments, help
templates, separate output streams and exit codes. Global options are declared
at each supported scope rather than inheriting v3's persistent flag behavior.
The existing `OC_*` then `MC_*` environment lookup keeps its first-present
semantics, including empty values. A present boolean option such as
`--json=false` or `--quiet=false` retains the existing application behavior.
Help and version options follow their parsed boolean values and only exit after
validating the options in their scope.

Ordinary repeated list options preserve literal commas. The hidden health
selector retains its own comma parsing and environment order. Integer lists use
decimal parsing. Root and group parsing stops at the first positional argument. Leaf parsing retains the old option reordering, quoted argument bytes, `--` boundaries and malformed-option errors through a small lexical adapter; native v3 still parses values and runs commands. Parent initialization still runs before parsing child options;
help and invalid commands retain the corresponding configuration side effects.
Completion preserves the previous flag names, aliases, visibility rules and the hidden terminal `--generate-bash-completion` protocol.
OC retains ownership of signal cancellation, cleanup and structured errors.

The one reviewed behavior correction is an invalid command-line health
selector: the old implementation panicked with exit code 2 while rendering
usage. OC now reports the validation error and exits with code 1. Its exact
output is recorded in [approved-deltas.json](../testdata/cli/approved-deltas.json).

`cmd/cli-support.go` contains only the application-specific scope, lifecycle and
presentation rules. Commands and flags use native v3 types. There is no vendored
framework, legacy CLI facade or `cli-kit` dependency. The separate `oc-console`
program keeps its standard-library flag parser.

## Reproduce the checks

The fixed old OC source is `446e018888bc17649a5d74b049d7006a471a9dd7`;
the old OtterIO source is `52e2d95b7d4fb58fb212f4a2ce3f351b891e0343`.
Build the old and current executable on the same platform, then run:

```sh
python3 buildscripts/cli_contract.py check \
  --binary /absolute/path/to/oc \
  --baseline-binary /absolute/path/to/oc-baseline \
  --output /absolute/path/to/cli-contract.json
```

The case catalogs cover 336 OC and 133 OtterIO invocations. Reports retain raw
stdout, stderr, exit codes, configuration effects and binary identity. Only
completion line ordering is normalized; duplicate candidates remain significant.
The approved health correction is checked explicitly. Catalog changes require
an old-binary baseline and cannot silently refresh expected results.

[CLI CI](../.github/workflows/cli-compat.yml) builds both revisions on Linux,
macOS and Windows. [Joint CI](../.github/workflows/go.yml) builds the exact remote
OtterIO module with no replacement or extra patches. It exercises startup
validation, server and NAS/S3 gateway file operations, explicit management
routing, TLS directories, signal shutdown, core/advanced operations and the
local console. OtterIO separately checks both standalone xl.meta tools and an
external Go gateway consumer.

Native Windows signals and runtime service acceptance retain their separate
platform checks. A cross-build or configured workflow is not evidence of a
successful native run. See [compatibility](compatibility.md) for recorded scope
and the latest validation report linked there.

## OtterIO Go API consumers

OC's exported `cmd/ilm.GetLifecycleOptions` now receives `*cli.Command` instead
of `*cli.Context`; Go consumers must rebuild against the v3 type as well.
The exported `cmd.GetHealthDataTypeSlice` and
`cmd.GetGlobalHealthDataTypeSlice` functions also receive `*cli.Command`.
The former `cmd.HealthDataTypeFlag` type is replaced by the native
`cli.GenericFlag{Value: &cmd.HealthDataTypeSlice{}}`; consumers must update
their flag declarations when rebuilding.

Gateway plugins now register `func() *cli.Command` factories, and their actions
receive `context.Context` and `*cli.Command`. The factory must return fresh
commands and flags on every call. `GlobalFlags()` and `ServerFlags()` return new
flag sets. The backend action calls
`StartGateway(context.Context, *cli.Command, Gateway) error`.
Old integer-list gateway flags must explicitly set
`Config: cli.IntegerConfig{Base: 10}` to retain decimal parsing.
See [OtterIO's API migration guide](https://github.com/soulteary/otterio/blob/af83288d39ef62a44f62f8fff69a5aae93023d99/docs/cli-migration.md)
before rebuilding an external gateway plugin.

---

# CLI 框架迁移

OC 和固定版本的 OtterIO 统一采用 `urfave/cli/v3 v3.14.0`。准确模块版本及服务端
提交记录在 [compatibility.json](compatibility.json)，Go 版本、模块路径、配置和
存储格式保持原状。

迁移保留参数作用域、布尔参数存在性、环境变量空值、列表逗号规则、十进制整数、
父级初始化时机、帮助格式、输出流、补全候选及信号清理。help/version 按实际布尔值
处理，先完成本级参数校验；同级使用同一参数的两种别名仍报错。普通列表参数不拆逗号，
隐藏健康参数继续使用自己的拆分规则。

唯一批准的行为修正是非法命令行健康参数：原版渲染帮助时 panic 并退出 2，现改为
正常参数错误并退出 1。此差异有独立精确快照，其他差异不能借此放行。

冻结基线覆盖 OC 336 项、OtterIO 133 项。CI 在同一平台构建旧、新程序，核对 stdout、
stderr、退出码及配置副作用，再使用可下载的固定 OtterIO 模块进行联合验证。
历史补丁保留作为旧报告的证据，当前构建无需应用。公共 gateway Go API 的工厂签名
需要外部插件重新编译。`ilm.GetLifecycleOptions`、`GetHealthDataTypeSlice` 和
`GetGlobalHealthDataTypeSlice` 的参数改为 `*cli.Command`；原 `HealthDataTypeFlag`
改用 `cli.GenericFlag{Value: &cmd.HealthDataTypeSlice{}}`。普通 CLI 用户无需改写已有命令。完整测试范围及已知验收边界
见[兼容性说明](zh_CN/compatibility.md)。
外部 gateway 的旧整数列表参数还需显式设置 `cli.IntegerConfig{Base: 10}`，保留十进制规则。
