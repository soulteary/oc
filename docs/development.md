# Developing OC

[中文开发指南](zh_CN/development.md) · [Contribution guide](../CONTRIBUTING.md)

This guide is for changing OC and reproducing its checks. For installing a published executable or image, use [installation](installation.md). For normal client operations, use [configuration](configuration.md), [usage](usage.md) and [administration](administration.md).

## Toolchain and source layout

Use the Go version declared in [go.mod](../go.mod), currently `1.27.1`. The [compatibility manifest](compatibility.json) records the same toolchain and the pinned OtterIO SDK. CI and the Makefile use `GOTOOLCHAIN=local`, so an older installed toolchain will fail instead of downloading a newer one automatically. Install the required version before running checks.

You also need Git and Python 3. The examples below use a POSIX shell on Linux or macOS. Makefile and cross-compilation targets need Bash; the server integration fixture additionally needs OpenSSL. Race tests and `CGO_ENABLED=1` runs require the platform's C compiler. Windows build/test commands are recorded in the [Go workflow](../.github/workflows/go.yml); use an `oc.exe` output when building natively there.

The checkout can live outside GOPATH. Its main areas are:

- `main.go`: executable entrypoint.
- `cmd/`: commands, flags, configuration, filesystem/S3 clients and most behavior tests.
- `pkg/`: client support packages and package tests.
- `internal/notify/`: the vendored notification implementation with its own MIT license.
- `buildscripts/`: dependency checks, integration fixtures, release packaging and image validation.
- `docs/compatibility.json`: reviewed SDK/toolchain, compile targets, server patches and test budgets.
- `.github/workflows/`: platform checks, CodeQL, publication and stable promotion.

The binary name is `oc`. The module path remains `github.com/soulteary/mc`; preserve it and existing copyright attribution unless a separate migration is agreed. OC does not use `govendor`.

## Build and check locally

From a Git checkout:

```sh
export GOTOOLCHAIN=local
go version
go mod download
CGO_ENABLED=0 go build -mod=readonly -tags kqueue -trimpath \
  -ldflags "$(go run buildscripts/gen-ldflags.go)" -o ./oc .
./oc --version
./oc --help
```

The metadata helper reads the current commit and its timestamp, so this stamped development build needs Git history. `make build` provides the repository's normal Unix build. A simple `GOTOOLCHAIN=local go build -o ./oc .` is useful during development but does not apply the same version flags as the stamped or release build. The Makefile's toolchain setting does not configure a separate shell invocation; keep it explicit for direct builds.

While iterating, run the tests nearest the change. For example, select the diagnostic regressions below; one case starts a local TLS test server:

```sh
go test ./cmd -run 'TestDoctorEffectiveAliasConfiguration|TestDiagnosticEndpointAllowlist' -count=1
```

Before submitting code, broaden to the ordinary local checks:

```sh
go test ./...
go vet ./...
go mod verify
PYTHONDONTWRITEBYTECODE=1 python3 buildscripts/test-maintenance.py
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s buildscripts -p 'test_release_*.py' -v
PYTHONDONTWRITEBYTECODE=1 python3 buildscripts/verify-release-boundaries.py
```

Format changed Go files with `gofmt -w PATH/TO/CHANGED.go` and review the diff. For concurrent code, cancellation, streams or file watching, also run:

```sh
go test -race --timeout 20m ./...
```

CI uses golangci-lint `v2.14.0` with [.golangci.yml](../.golangci.yml). With that version installed, run `golangci-lint run --timeout=5m`. `make getdeps` installs tools and may fetch packages; it is not necessary for the plain Go/Python checks above. The Python release tests use temporary fixtures and do not publish tags, releases or registry images.

`make test`, `make check` and `make verify` also invoke the inherited `functional-tests.sh`. That script defaults to the upstream public `play.min.io` server when `SERVER_ENDPOINT` is unset and performs object/bucket writes and deletes. Use the direct checks above and the temporary local integration fixture below for ordinary development. Run the legacy suite only against an explicitly configured disposable target after reviewing its environment and cleanup requirements; do not point it at a production deployment. `make crosscompile` checks the eleven targets in the compatibility manifest; it does not run the resulting binaries.

## Reproduce the server integration fixture

The integration test starts disposable local OtterIO processes with random credentials, temporary client configuration, local ports and temporary storage. It writes and deletes test objects and performs administration against those processes. It does not take the address of an existing deployment.

The SDK dependency remains pinned in `go.mod`. The corresponding server source needs the five patches listed in `docs/compatibility.json`. Copy the module source to a writable temporary directory; never apply these patches inside the shared Go module cache or against a production checkout.

The following prepares a `CGO_ENABLED=0` fixture matching the CI setup. Keep the same shell open so the paths remain available:

```sh
export GOTOOLCHAIN=local
export CGO_ENABLED=0
OC_SOURCE="$(pwd)"
OC_INTEGRATION="$(mktemp -d)"
go mod download
go build -mod=readonly -trimpath -o "$OC_INTEGRATION/oc" .
otterio_source="$(go list -m -f '{{.Dir}}' github.com/soulteary/otterio)"
cp -R "$otterio_source" "$OC_INTEGRATION/otterio-source"
chmod -R u+w "$OC_INTEGRATION/otterio-source"
git -C "$OC_INTEGRATION/otterio-source" apply "$OC_SOURCE/buildscripts/otterio-core-compat.patch"
git -C "$OC_INTEGRATION/otterio-source" apply "$OC_SOURCE/buildscripts/otterio-runtime-compat.patch"
git -C "$OC_INTEGRATION/otterio-source" apply "$OC_SOURCE/buildscripts/otterio-http-api-compat.patch"
git -C "$OC_INTEGRATION/otterio-source" apply "$OC_SOURCE/buildscripts/otterio-account-info-compat.patch"
git -C "$OC_INTEGRATION/otterio-source" apply "$OC_SOURCE/buildscripts/otterio-conditional-writes-compat.patch"
(
  cd "$OC_INTEGRATION/otterio-source"
  go build -mod=readonly -trimpath -o "$OC_INTEGRATION/otterio" .
)
python3 buildscripts/test-core-integration.py \
  --oc "$OC_INTEGRATION/oc" --otterio "$OC_INTEGRATION/otterio" \
  --report "$OC_INTEGRATION/core-integration.json" \
  --artifacts-dir "$OC_INTEGRATION/core-evidence"
```

The default run covers migration and the five HTTP/TLS/port configurations in the script. To include advanced operations on temporary four-drive storage and the stability checks used by CI, run:

```sh
python3 buildscripts/test-core-integration.py \
  --oc "$OC_INTEGRATION/oc" --otterio "$OC_INTEGRATION/otterio" \
  --extended --stability --soak-seconds 30 \
  --report "$OC_INTEGRATION/core-integration.json" \
  --artifacts-dir "$OC_INTEGRATION/core-evidence"
```

This is a longer check with local disk and memory requirements. A failure report contains partial results; check every scenario's status before treating the run as acceptance. Save the report and relevant redacted evidence for review, then remove only the temporary directory you created. The script cleans up its servers and scenario storage; the supplied report/evidence directory remains for inspection. Repeat with `CGO_ENABLED=1` when investigating a cgo-specific issue.

The exact server regression tests and the Linux/macOS cgo matrix live in the [Go workflow](../.github/workflows/go.yml). [Compatibility](compatibility.md) describes what these checks establish and what remains unverified. Server fixture patches do not change the OC SDK dependency or certify another OtterIO version.

## Understand the CI scope

The Go workflow runs platform unit/race tests on Linux, macOS and Windows, Linux vet/lint/cross-compilation, and Linux/macOS server integration with cgo both disabled and enabled. It also checks release boundaries, compiled-module inventory and reachable vulnerabilities. [CodeQL](../.github/workflows/codeql.yml) builds the Go code separately for analysis.

Some checks need network access to fetch dependencies or vulnerability data; server fixture tests use local temporary services. A local pass covers one environment. It does not substitute for platform CI, a clean-build inventory, or acceptance on an external S3 provider. Report relevant failures and platform limits in the pull request.

## Change dependencies, documentation or releases

For an intentional dependency change, review `go.mod` and `go.sum` together, update the compatibility baseline and its boundary checks when appropriate, and explain the reason in the pull request. Run `go mod tidy` only when the dependency graph needs adjustment, then inspect the diff. Do not commit a temporary `replace` or silently upgrade the pinned OtterIO SDK to make a local test pass.

Update command help and the English/Chinese user guides for changed flags, defaults, output or data behavior. Link migration requirements from [migration](migration.md), operational caveats from [administration](administration.md), and diagnostics from [troubleshooting](troubleshooting.md). The `oc-phase-*` documents retain design and verification history; the task guides are the user-facing starting point.

The [release guide](releasing.md) explains timestamp tags, archive contents, immutable multi-platform images and `latest` promotion. Release helper tests are suitable for a local PR check. Actual publication requires maintainer credentials, reviewed notes and successful main CI for the exact commit; it is not part of the development loop. [Maintainer guidance](MAINTAINERS.md) covers review and publication responsibilities.
