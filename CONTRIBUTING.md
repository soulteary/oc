# Contributing to OC

[中文贡献指南](docs/zh_CN/CONTRIBUTING.md)

Contributions can improve code, tests, documentation, diagnostics or reproducible bug reports. OC is the command-line client in this repository; OtterIO is the server project. Read the [development guide](docs/development.md) for the module layout, local checks and server integration fixtures.

## Choose where to start

For a small fix or documentation correction, open a focused pull request against `main`. For a new command, a dependency change or a behavior change that affects stored data, open an [OC issue](https://github.com/soulteary/oc/issues) first so the scope and compatibility impact can be discussed. Check existing issues and pull requests before starting overlapping work.

Installation and everyday questions belong with the [installation](docs/installation.md), [configuration](docs/configuration.md), [usage](docs/usage.md) and [troubleshooting](docs/troubleshooting.md) guides. If they do not answer your question, include the command and environment in an issue. Security reports follow [SECURITY.md](SECURITY.md); do not put credentials or an unreviewed exploit in a public issue.

Participation follows the [Code of Conduct](code_of_conduct.md). Its enforcement section describes the currently documented contact options. Do not post confidential incident details in a public thread.

## Report a reproducible problem

Include:

- The output of `oc --version`, your operating system and architecture, and how OC was installed.
- The command, flags, expected result and actual result, including exit status when relevant.
- A minimal reproduction using disposable files, objects or accounts. Describe which step changes or removes data.
- The server product and version, whether S3 and management use separate addresses, and whether TLS or a proxy is involved.
- Relevant redacted errors or output from `oc --json doctor` or `oc --json doctor ALIAS`. These diagnostics are offline by default; `--online` performs a management request.

Remove access keys, secret keys, session tokens, signed URLs and private configuration before sharing a report. Logs and command traces can contain details that are not present in the offline doctor output. A passing build or a similar upstream command does not establish compatibility; see the [support scope](docs/compatibility.md).

## Work on a branch

Fork [soulteary/oc](https://github.com/soulteary/oc/fork), clone your fork into any convenient directory, and create a branch from current `main`. Go modules handle dependency resolution; a checkout under `$GOPATH/src` is not required.

```sh
git clone https://github.com/YOUR-USERNAME/oc.git
cd oc
git switch -c fix/describe-the-change
go version
go mod download
```

Use the toolchain declared in `go.mod` and the documented [local build and test commands](docs/development.md#build-and-check-locally). The executable is `oc`, while the Go module path remains `github.com/soulteary/mc` for source compatibility. Do not rename import paths as part of an unrelated fix.

## Prepare the pull request

Keep one problem and its necessary changes together. Explain what triggers the problem, how behavior changes, and any migration or data impact. Add a regression test when the fix changes behavior; a spelling correction does not need a code test.

- Format changed Go files with `gofmt` and run the relevant package tests.
- Run the dependency and release checks described in the development guide. Report the commands and results, including checks you could not run.
- Update affected help text and English/Chinese guides together. If one translation is pending, say so in the pull request.
- Preserve source copyright notices, [LICENSE](LICENSE), [NOTICE](NOTICE), [CREDITS](CREDITS) and the notification library's MIT license. OC's upstream attribution does not imply endorsement by MinIO, Inc.
- Explain each intentional dependency change. Avoid broad upgrades, temporary `replace` directives and unrelated `go.sum` churn; the OtterIO baseline is checked by CI.
- Link the issue where one exists, and describe any intentionally unverified platform or service behavior.

Small, coherent commits make review easier; there is no requirement to squash every pull request into one commit before discussion. Address review comments with the implementation or a reasoned explanation, and rerun checks affected by subsequent edits. Never include credentials or private production data in test fixtures.

The [Go workflow](.github/workflows/go.yml) and [CodeQL workflow](.github/workflows/codeql.yml) define the repository checks. Local tests cover your machine; they do not replace the Linux, macOS, Windows and server integration matrix. A documentation-only pull request should still verify links, examples and claims against the current source.

## Releases and support

Maintainers publish timestamp tags through the [release workflow](docs/releasing.md). Contributors do not need to create a tag, push a container image or provision registry credentials to validate a pull request. Release helper tests are offline; production publication and `latest` promotion are separate operations.

Use [migration](docs/migration.md), [administration](docs/administration.md) and [compatibility](docs/compatibility.md) when a change affects existing users. Report bugs and propose improvements through this repository. Response time depends on maintainer availability; these documents do not promise a support SLA.
