# Maintaining OC

[中文维护者指南](zh_CN/MAINTAINERS.md)

This guide describes review, dependency and release responsibilities for this repository. It is not a roster of people with repository access. Contributors should start with [CONTRIBUTING.md](../CONTRIBUTING.md) and the [development guide](development.md).

## Triage and review

Keep bug reports reproducible: request the OC version, platform, command, expected/actual result and relevant server baseline. Ask for redacted evidence rather than raw credential files. Route potential vulnerabilities through [SECURITY.md](../SECURITY.md); use this repository's issues for ordinary support and feature discussion.

Agree the scope of a new command, dependency change or compatibility change before treating its implementation as ready for review. Check that help text, exit status, JSON errors, cancellation and migration behavior remain consistent with the affected commands.

Changes that write, remove, replace or synchronize data need regression evidence for their failure paths as well as the successful case. Review ownership and permissions, partial writes, cleanup, retries and signal handling where the change touches them. Use disposable fixtures for tests and examples. A full passing test suite does not excuse an unreviewed data-loss path.

Review test results for the actual change and environment. The [Go workflow](../.github/workflows/go.yml) records platform, race, server integration, inventory and vulnerability checks; [CodeQL](../.github/workflows/codeql.yml) provides separate analysis. A local run does not establish every platform's runtime behavior, and cross-compilation does not extend the [compatibility claim](compatibility.md).

Keep PR descriptions and release notes about the final change: its trigger, resulting behavior, user impact, validation and remaining limits. Split unrelated work when it makes review clearer. Verify English/Chinese documentation and example commands alongside changed flags or defaults.

## Dependencies and licensing

Dependencies are managed by Go modules. Review intentional `go.mod`/`go.sum` changes together, including why a new package or upgrade is needed. Do not commit local `replace` directives or use broad dependency upgrades to bypass a failing fixture.

The OtterIO SDK/toolchain baseline and server patches are recorded in [compatibility.json](compatibility.json) and checked by `buildscripts/verify-release-boundaries.py`. A baseline change needs its related tests, documentation and boundary checks updated in the same reviewed work. The current pin includes the former server fixes; build the exact module source without applying historical compatibility patches. The [development guide](development.md#reproduce-the-server-integration-fixture) documents the setup.

Preserve [LICENSE](../LICENSE), [NOTICE](../NOTICE), [CREDITS](../CREDITS), source copyright notices and the notification package's MIT license. Archive and image packaging must retain the applicable license files. Retained upstream names identify origin and compatibility; they do not imply affiliation with or endorsement by MinIO, Inc. The Go module path remains `github.com/soulteary/mc`, while the executable is `oc`.

## Documentation and compatibility

Maintain task guides as the current user entry points: [installation](installation.md), [configuration](configuration.md), [usage](usage.md), [migration](migration.md), [administration](administration.md), [troubleshooting](troubleshooting.md), [compatibility](compatibility.md) and [development](development.md). Preserve the `oc-phase-*` documents as design/verification history and avoid silently presenting historical examples as current instructions.

Document what a check establishes. Keep build targets, platform runtime tests, server fixture acceptance and external-provider support distinct. Do not add an SLA, supported version range, maintainer email or production guarantee without an actual project decision and evidence.

The [Code of Conduct](../code_of_conduct.md) retains its Contributor Covenant attribution and licensing clarification. Keep its enforcement contact current and separate from vulnerability reporting. At present OC does not document a dedicated confidential conduct-reporting channel; an upstream MinIO address is not an OC contact. Requests for a confidential contact must not expose private incident details in public.

## Publish a release

Follow [releasing OC](releasing.md); do not use the obsolete `make release` advice from the upstream maintainer guide. The current workflow builds archives directly and `.goreleaser.yml` has publication disabled.

Before creating a timestamp tag, review `RELEASE_NOTES.md`, confirm a clean `main` at the fetched remote commit, and check successful main runs of both Go and CodeQL for that exact commit. A green PR run is insufficient. `buildscripts/release-preflight.py` is a read-only prerequisite check; it does not create the tag or prove remote CI success.

The current publication workflow produces eleven archives, a manifest and checksums, plus immutable Linux amd64/arm64 images. GHCR uses the workflow token; optional Docker Hub publication requires both configured secrets. Keep registry credentials out of PR test jobs and never expose them to untrusted code. Verify package visibility before documenting anonymous GHCR pulls. An older archive-only release does not establish that images exist: check its manifest's `images` field and the registry. Promotion refuses manifests without recorded images.

The workflow validates the image by digest, records registry identities in the existing archive manifest, regenerates its checksum, and downloads/compares draft assets before publication. The publication job and the entire stable promotion workflow share `oc-stable-promotion`. Preserve that shared critical section when editing workflows, and do not put its lock on the reusable workflow's caller: the caller must release the lock before promotion can acquire it.

Only the newest published stable timestamp release can promote its recorded digests to `latest`. Aliases must verify before GitHub's latest marker is updated. Promotion does not rebuild, change version tags or regenerate release assets.

## Recover and support a release

Never move a release tag or overwrite an existing version image to retry publication. Once any version image has been pushed, prepare a fresh timestamp tag after successful main checks. If publication succeeded and promotion failed, retain the release and retry **Stable release promotion** for its tag. A newer published release prevents an older one from rolling aliases back.

Review partial results and draft state before retrying. Multi-service publication is not atomic; the [release guide](releasing.md#verify-and-recover) describes the supported recovery path. Investigate reported regressions against a pinned release, retain sanitized reproduction evidence, and update compatibility or migration guidance when a fix changes user behavior. No automated self-update or upstream MinIO upload channel is enabled.
