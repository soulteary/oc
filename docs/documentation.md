# Maintaining OC documentation

[Documentation index](README.md) · [简体中文](zh_CN/documentation.md)

## Organize around a reader's task

Keep the root README short enough to explain the client, install it, connect once and find the next guide. User guides cover installation, containers, configuration, transfers, migration, administration and troubleshooting. The command index helps readers discover commands; the installed client's `--help` gives flags for that build. Contributor, maintainer and release instructions belong in their own pages.

The organization takes inspiration from [GitHub CLI's README and contribution entry points](https://github.com/cli/cli), [rclone's installation/configuration/usage navigation](https://rclone.org/docs/), and [restic's task-oriented manual](https://restic.readthedocs.io/en/stable/). These references inform structure. OC behavior and examples must be checked against OC's own source, command help and recorded tests.

## Keep technical facts reviewable

- Check flags against `oc COMMAND --help` using a temporary `--config-dir`; do not rely on a stale executable in the checkout.
- Test examples with local temporary files or disposable server fixtures. Do not use existing user aliases, real credentials or production buckets for documentation checks.
- Distinguish a command being present, a successful cross-compile, CI coverage and recorded runtime acceptance.
- Use `compatibility.json`, `go.mod` and the release workflow for target/toolchain/publication details. Do not copy a server version or support claim from another project.
- Link source and validation records when a behavior has an important limit, such as stream replay, mirror deletion, management endpoint precedence or object-lock support.
- Preserve dated reports and their hashes. Add current navigation or corrections with context rather than changing the outcome of an old run.

## Write examples readers can use

State the shell and prerequisites when they matter. Use a real published timestamp tag for archive examples and explain how to choose another release. For containers, use a real tag only when its manifest records images; otherwise show an explicitly marked placeholder or a local source build. Keep placeholders obvious for endpoints, paths, account names and credentials. Prefer interactive credential input for setup examples. Place permissions and destructive effects next to the command concerned, especially `mirror --remove`, bucket removal, service control and configuration import.

Do not advertise disabled self-update, upstream download endpoints, unavailable OC package-manager formulas, an undocumented support SLA or untested provider compatibility. Review debug output, exported configuration and generated links before including them in an issue.

## Maintain both languages

English guides live directly under `docs/`; Simplified Chinese guides live under `docs/zh_CN/`. Keep paired guides aligned on examples, capabilities, prerequisites and limitations. Link the counterpart near the top. Write clear Chinese sentences rather than translating English word order literally.

The dated phase records are retained in their original language. Keep the maintainer and release procedures aligned across both languages as well. Old guide filenames remain navigation pages so existing links still lead readers to current instructions.

## Review a documentation change

Read the rendered Markdown, check local links and heading anchors, and verify commands/flags against the current client. Check both language indexes and README entry points. Re-run only the examples affected by a change; a documentation check is not a new claim that every integration, platform or provider passed acceptance.
