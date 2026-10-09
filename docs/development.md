# Developing OC

[中文开发指南](zh_CN/development.md) · [Contribution guide](../CONTRIBUTING.md)

This guide is for changing OC and reproducing its checks. For installing a published executable or image, use [installation](installation.md). For normal client operations, use [configuration](configuration.md), [usage](usage.md) and [administration](administration.md).

## Toolchain and source layout

Use the Go version declared in [go.mod](../go.mod), currently `1.27.2`. The [compatibility manifest](compatibility.json) records the same toolchain, the independent S3 SDK and kits, and the separate server/admin source pin. CI and the Makefile use `GOTOOLCHAIN=local`, so an older installed toolchain will fail instead of downloading a newer one automatically. Install the required version before running checks.

You also need Git and Python 3.11 or newer (the fixtures use `hashlib.file_digest`). Console frontend behavior checks require Node.js; building and running `oc-console` does not. The examples below use a POSIX shell on Linux or macOS. Makefile and cross-compilation targets need Bash; the Make dependency check also uses Perl, and the server integration fixture needs OpenSSL with `req -addext` support. Race tests and `CGO_ENABLED=1` runs require the platform's C compiler. Windows build/test commands are recorded in the [Go workflow](../.github/workflows/go.yml); use an `oc.exe` output when building natively there.

The checkout can live outside GOPATH. Its main areas are:

- `main.go`: executable entrypoint.
- `cmd/`: commands, flags, configuration, filesystem/S3 clients and most behavior tests.
- `pkg/`: client support packages and package tests.
- `internal/notify/`: the vendored notification implementation with its own MIT license.
- `cmd/oc-console/` and `internal/console/`: the optional local console entrypoint, server and embedded frontend.
- `internal/clienttransport/` and `internal/storageclient/`: shared transport and console storage operations.
- `buildscripts/`: dependency checks, integration fixtures, release packaging and image validation.
- `docs/compatibility.json`: reviewed S3 SDK, kits, server/admin pin, toolchain, compile targets, core required-patch list (currently empty), optional console protocol patch and test budgets.
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

For console changes, also build its separate executable and run the Go, frontend and acceptance-helper regressions:

```sh
make build-console
make test-console
PYTHONDONTWRITEBYTECODE=1 python3 -m unittest discover -s buildscripts -p 'test_console_*.py' -v
```

`make build` builds only `oc`. See [the local console guide](console.md) for startup and the limits of its opt-in write mode.

Format changed Go files with `gofmt -w PATH/TO/CHANGED.go` and review the diff. For concurrent code, cancellation, streams or file watching, also run:

```sh
go test -race --timeout 20m ./...
```

CI uses golangci-lint `v2.14.0` with [.golangci.yml](../.golangci.yml). With that version installed, run `golangci-lint run --timeout=5m`. `make getdeps` installs tools and may fetch packages; it is not necessary for the plain Go/Python checks above. The Python release tests use temporary fixtures and do not publish tags, releases or registry images.

`make test`, `make check` and `make verify` also invoke the inherited `functional-tests.sh`. That script defaults to the upstream public `play.min.io` server when `SERVER_ENDPOINT` is unset and performs object/bucket writes and deletes. Use the direct checks above and the temporary local integration fixture below for ordinary development. Run the legacy suite only against an explicitly configured disposable target after reviewing its environment and cleanup requirements; do not point it at a production deployment. `make crosscompile` checks the eleven targets in the compatibility manifest; it does not run the resulting binaries.

## Reproduce the server integration fixture

The integration test starts disposable local OtterIO processes with random credentials, temporary client configuration, local ports and temporary storage. It writes and deletes test objects and performs administration against those processes. It does not take the address of an existing deployment.

Build the integration server from the exact `github.com/soulteary/otterio` server/admin module version pinned in `go.mod`; it already includes the former compatibility fixes. The separate `github.com/soulteary/otterio-sdk/v7` dependency is the S3 client SDK. Copy the server source to a writable temporary directory and do not modify the shared module cache. In `docs/compatibility.json`, `otterioSource` records the server source SHA and `storageSDK` records the independent SDK identity.

The following prepares a `CGO_ENABLED=0` fixture matching the CI setup. Keep the same shell open so the paths remain available:

```sh
export GOTOOLCHAIN=local
export CGO_ENABLED=0
OC_INTEGRATION="$(mktemp -d)"
go mod download
go build -mod=readonly -trimpath -o "$OC_INTEGRATION/oc" .
otterio_source="$(go list -m -f '{{.Dir}}' github.com/soulteary/otterio)"
cp -R "$otterio_source" "$OC_INTEGRATION/otterio-source"
chmod -R u+w "$OC_INTEGRATION/otterio-source"
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

This is a longer check with local disk and memory requirements. A failure report contains partial results; check every scenario's status before treating the run as acceptance. Keep the directory if continuing with the optional console checks below. After completing the checks you need, save the report and relevant redacted evidence for review, then remove only the temporary directory you created. The script cleans up its servers and scenario storage; the supplied report/evidence directory remains for inspection. Repeat with `CGO_ENABLED=1` when investigating a cgo-specific issue.

The exact server regression tests and the Linux/macOS cgo matrix live in the [Go workflow](../.github/workflows/go.yml). [Compatibility](compatibility.md) describes what these checks establish and what remains unverified. Passing tests for one fixed source do not certify another OtterIO version. The `otterio-*-compat.patch` files are historical fixtures; the optional console patch below is still used by current integration checks.

## Reproduce the optional console protocol fixture

The pinned source remains `6f6d0835ddff68020f1491c403b958fade22841f`; it does not include P3. Current OtterIO source HEAD already includes P3 and must be identified separately. The exported patches below target the fixed pin, not that developer checkout. `requiredServerPatches: []` continues to describe the unpatched core baseline.

Current fixtures use four independent source copies and binaries:

- `console-server-base.patch`: protected bucket settings, own IAM secret rotation and metadata transactions. Its CAS lifecycle profile accepts prefix expiration and noncurrent expiration, while refusing transitions, tag filters and expired-delete-marker rules. Reads preserve complete existing documents, and ordinary unconditional S3 writes keep the pinned server's behavior.
- Base plus `console-features-server.patch` and `console-iam-bindings.patch`: primary five-feature acceptance, including GET/HEAD and CopyObject/UploadPartCopy version authorization. Lifecycle storage is not required by this profile.
- Base plus `lifecycle-storage.patch` and mandatory `lifecycle-storage-hardening.patch`: lifecycle transition/restore runtime acceptance with a separate disposable tier. The storage layer adds decoded-tag and noncurrent filtering, delete-marker scanning, durable references and deletion protection.
- Base plus storage, mandatory lifecycle hardening, version authorization and IAM: independent composition regression for features, protected settings and object writes.

`console-server-p3.patch` is frozen historical evidence. Do not apply it together with the new base/storage/hardening exports. Historical reports retain their original patch and binary hashes; they do not establish a passing result for a split profile. Current report status is recorded in [feature validation](console-features.md), [lifecycle scope](lifecycle-transition.md) and `consoleServerAcceptance` in [compatibility.json](compatibility.json).

Continue in the same shell with the `OC_INTEGRATION` directory prepared above. Keep the original pin intact, then build and test the four compositions separately:

```sh
set -eu
go build -mod=readonly -trimpath -o "$OC_INTEGRATION/oc-console" ./cmd/oc-console
OC_SERVER_SOURCE="$(python3 -c 'import json; print(json.load(open("docs/compatibility.json"))["otterioSource"])')"
OC_BASE_PATCH="$PWD/buildscripts/console-server-base.patch"
OC_STORAGE_PATCH="$PWD/buildscripts/lifecycle-storage.patch"
OC_HARDENING_PATCH="$PWD/buildscripts/lifecycle-storage-hardening.patch"
OC_VERSIONS_PATCH="$PWD/buildscripts/console-features-server.patch"
OC_IAM_PATCH="$PWD/buildscripts/console-iam-bindings.patch"
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio" --server-source "$OC_SERVER_SOURCE" \
  --settings-legacy --output "$OC_INTEGRATION/console-legacy.json"
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio" --server-source "$OC_SERVER_SOURCE" \
  --features --features-legacy --output "$OC_INTEGRATION/console-features-legacy.json"

cp -R "$OC_INTEGRATION/otterio-source" "$OC_INTEGRATION/otterio-base-source"
(
  cd "$OC_INTEGRATION/otterio-base-source"
  git apply --check "$OC_BASE_PATCH"
  git apply "$OC_BASE_PATCH"
  go test -mod=readonly ./cmd -run 'TestBucketConfig|TestSelfCredentials|TestConfiguration|TestFiber|TestGetBucket|TestPutBucket|TestDeleteBucket|TestAccountInfo|TestBucketMetadata' -count=1
  if [ "$CGO_ENABLED" = 1 ]; then
    go test -mod=readonly -race ./cmd -run 'TestBucketConfig|TestSelfCredentials|TestConfiguration|TestFiber|TestGetBucket|TestPutBucket|TestDeleteBucket|TestAccountInfo|TestBucketMetadata' -count=1
  fi
  go build -mod=readonly -trimpath -o "$OC_INTEGRATION/otterio-base" .
)
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio-base" --server-source "$OC_SERVER_SOURCE" \
  --server-patch "$OC_BASE_PATCH" --settings --settings-base \
  --output "$OC_INTEGRATION/console-base-settings.json"
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio-base" --server-source "$OC_SERVER_SOURCE" \
  --server-patch "$OC_BASE_PATCH" --writes \
  --output "$OC_INTEGRATION/console-base-objects.json"

cp -R "$OC_INTEGRATION/otterio-base-source" "$OC_INTEGRATION/otterio-features-source"
(
  cd "$OC_INTEGRATION/otterio-features-source"
  git apply "$OC_VERSIONS_PATCH"
  git apply "$OC_IAM_PATCH"
  go test -mod=readonly ./cmd -run '^TestConsoleVersionAuthorization|^TestConsoleIAM' -count=1
  if [ "$CGO_ENABLED" = 1 ]; then
    go test -mod=readonly -race ./cmd -run '^TestConsoleVersionAuthorization|^TestConsoleIAM' -count=1
  fi
  go test -mod=readonly ./pkg/iam/policy ./pkg/bucket/policy -count=1
  go build -mod=readonly -trimpath -o "$OC_INTEGRATION/otterio-features" .
)
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio-features" --server-source "$OC_SERVER_SOURCE" \
  --server-patch "$OC_BASE_PATCH" --server-patch "$OC_VERSIONS_PATCH" \
  --server-patch "$OC_IAM_PATCH" --features --version-copy \
  --output "$OC_INTEGRATION/console-base-features.json"

cp -R "$OC_INTEGRATION/otterio-base-source" "$OC_INTEGRATION/otterio-storage-source"
(
  cd "$OC_INTEGRATION/otterio-storage-source"
  git apply --check "$OC_STORAGE_PATCH"
  git apply "$OC_STORAGE_PATCH"
  git apply --check "$OC_HARDENING_PATCH"
  git apply "$OC_HARDENING_PATCH"
  go test -mod=readonly ./cmd -run 'TestBucketConfig|TestSelfCredentials|TestConfiguration|TestFiber|TestGetBucket|TestPutBucket|TestDeleteBucket|TestAccountInfo|TestBucketMetadata|TestBucketTarget|TestTransition|TestXLStorageInline|TestLifecycleTransition|TestLifecycleQueues|TestScannerLifecycle|TestExpiry|TestParseRestore|TestRestoreRequest|TestBeginRestore|TestRestoredVersion|TestPutObjectExpiry|TestFindFileInfoInQuorum|TestXLV2FormatData|TestErasureDeleteObjectBasic|TestErasureDeleteObjectsErasureSet' -count=1
  go test -mod=readonly ./pkg/bucket/lifecycle ./cmd/config/storageclass -count=1
  go test -mod=readonly ./cmd -run 'TestErasurePutObject|TestXLStorageReadFile|TestXLStorageHealsPendingTransitionMetadata|TestMonitorAndConnectEndpointsCanceled|TestPutObjectNoQuorum|TestObjectQuorumFromMeta|TestDisksWithAllParts' -count=1
  if [ "$CGO_ENABLED" = 1 ]; then
    go test -mod=readonly -race ./pkg/bucket/lifecycle ./cmd/config/storageclass -count=1
    go test -mod=readonly -race ./cmd -run 'TestErasurePutObject|TestXLStorageReadFile|TestXLStorageHealsPendingTransitionMetadata|TestMonitorAndConnectEndpointsCanceled|TestPutObjectNoQuorum|TestObjectQuorumFromMeta|TestDisksWithAllParts' -count=1
  fi
  go build -mod=readonly -trimpath -o "$OC_INTEGRATION/otterio-storage" .
)
python3 buildscripts/test-lifecycle-transition-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio-storage" --server-source "$OC_SERVER_SOURCE" \
  --server-patch "$OC_BASE_PATCH" --server-patch "$OC_STORAGE_PATCH" \
  --server-patch "$OC_HARDENING_PATCH" \
  --output "$OC_INTEGRATION/lifecycle-storage.json"

cp -R "$OC_INTEGRATION/otterio-storage-source" "$OC_INTEGRATION/otterio-combined-source"
(
  cd "$OC_INTEGRATION/otterio-combined-source"
  git apply "$OC_VERSIONS_PATCH"
  git apply "$OC_IAM_PATCH"
  go test -mod=readonly ./cmd -run '^TestConsoleVersionAuthorization|^TestConsoleIAM' -count=1
  if [ "$CGO_ENABLED" = 1 ]; then
    go test -mod=readonly -race ./cmd -run '^TestConsoleVersionAuthorization|^TestConsoleIAM' -count=1
  fi
  go build -mod=readonly -trimpath -o "$OC_INTEGRATION/otterio-combined" .
)
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio-combined" --server-source "$OC_SERVER_SOURCE" \
  --server-patch "$OC_BASE_PATCH" --server-patch "$OC_STORAGE_PATCH" \
  --server-patch "$OC_HARDENING_PATCH" \
  --server-patch "$OC_VERSIONS_PATCH" --server-patch "$OC_IAM_PATCH" \
  --features --version-copy --output "$OC_INTEGRATION/console-combined-features.json"
python3 buildscripts/test-console-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio-combined" --server-source "$OC_SERVER_SOURCE" \
  --server-patch "$OC_BASE_PATCH" --server-patch "$OC_STORAGE_PATCH" \
  --server-patch "$OC_HARDENING_PATCH" \
  --server-patch "$OC_VERSIONS_PATCH" --server-patch "$OC_IAM_PATCH" \
  --settings --writes --output "$OC_INTEGRATION/console-combined-settings-objects.json"
python3 buildscripts/test-lifecycle-transition-integration.py \
  --cli "$OC_INTEGRATION/oc" --console "$OC_INTEGRATION/oc-console" \
  --server "$OC_INTEGRATION/otterio-combined" --server-source "$OC_SERVER_SOURCE" \
  --server-patch "$OC_BASE_PATCH" --server-patch "$OC_STORAGE_PATCH" \
  --server-patch "$OC_HARDENING_PATCH" \
  --server-patch "$OC_VERSIONS_PATCH" --server-patch "$OC_IAM_PATCH" \
  --output "$OC_INTEGRATION/console-combined-lifecycle.json"
```

The console scenarios are `single-http`, `dual-http` and `dual-tls`; lifecycle execution uses `single-http` and `dual-tls` with independent four-drive source and destination processes. Neither proves external-provider or distributed acceptance. Use `CGO_ENABLED=1` for the matching race-enabled CI variant. Lifecycle reports accept repeatable `--server-patch` values and record each layer in `serverPatches`. Each report records source, actual patch hashes, binaries and harness identity; base settings/object, hardened lifecycle, standalone/full features and copy, and full settings/object reports have passed locally; the full composition lifecycle and final aggregate have also passed. See the [Go workflow](../.github/workflows/go.yml) for the authoritative step order and artifact names.

## Understand the CI scope

The Go workflow runs platform unit/race tests and console frontend/helper regressions on Linux, macOS and Windows, Linux lint/cross-compilation, and vet on Linux/macOS. Its Linux/macOS integration matrix uses cgo both disabled and enabled. It first tests the unpatched pinned server for core/CLI operations, console browsing, safe settings fallback and object writes. It then independently builds the base settings/write server, the base/version/IAM five-feature server, the base/storage/hardening runtime server, and the full composition regression server. Version-copy checks exercise actual CopyObject and UploadPartCopy requests in both feature compositions. Both lifecycle compositions also run the same independent-tier harness and archive separate reports. The upload step runs after a failure and preserves the reports generated before that point.

The separate [CLI compatibility workflow](../.github/workflows/cli-compat.yml) builds a fixed baseline and the candidate on Linux, macOS and Windows and compares their command contracts. The Go workflow also checks release boundaries, compiled-module inventory and reachable vulnerabilities. [CodeQL](../.github/workflows/codeql.yml) builds the Go code separately for analysis.

Some checks need network access to fetch dependencies or vulnerability data; server fixture tests use local temporary services. A local pass covers one environment. It does not substitute for platform CI, a clean-build inventory, or acceptance on an external S3 provider. Report relevant failures and platform limits in the pull request.

## Change dependencies, documentation or releases

For an intentional dependency change, review `go.mod` and `go.sum` together, update the compatibility baseline and its boundary checks when appropriate, and explain the reason in the pull request. Run `go mod tidy` only when the dependency graph needs adjustment, then inspect the diff. Do not commit a temporary `replace` or silently upgrade the reviewed S3 SDK or server/admin pin to make a local test pass.

Update command help and the English/Chinese user guides for changed flags, defaults, output or data behavior. Link migration requirements from [migration](migration.md), operational caveats from [administration](administration.md), and diagnostics from [troubleshooting](troubleshooting.md). The `oc-phase-*` documents retain design and verification history; the task guides are the user-facing starting point.

The [release guide](releasing.md) explains timestamp tags, archive contents, immutable multi-platform images and `latest` promotion. Release helper tests are suitable for a local PR check. Actual publication requires maintainer credentials, reviewed notes and successful main CI for the exact commit; it is not part of the development loop. [Maintainer guidance](MAINTAINERS.md) covers review and publication responsibilities.
