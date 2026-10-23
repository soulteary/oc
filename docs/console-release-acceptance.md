# Accept the packaged Console migration pair before publication

The source-pair and upgrade reports in the [candidate guide](console-migration-candidate.md)
are development evidence. They do not certify new release archives or images and
cannot be supplied to the publication workflow as official acceptance.

## Three release operations

1. The tag build, `release.yml`, produces fixed-version images and a verified
   stable GitHub draft in each repository. It stops at the draft and does not
   call publication or stable promotion. Registry images may already be public;
   draft preparation is not an RC or a promise of private artifacts.
2. After packaged-pair acceptance passes, manually run `publish-release.yml` on
   `main` with `tag`, `acceptance_run_id` and `attestation_sha256`. The publisher
   verifies the successful official acceptance and unchanged accepted assets,
   then makes the stable release public with GitHub latest unchanged. Publish OC
   first; OtterIO publication requires the accepted OC partner to be public.
3. Manually run `promote-release.yml` for an already published stable tag if it
   should become latest. Promotion uses the recorded digests, requires the newest
   published stable release and updates registry aliases before GitHub latest.

No stage creates a new release tag for the operator. Publication never rebuilds
programs, replaces assets, changes immutable version tags or invokes promotion.
These separate operations do not replace the required exact-main-commit source
CI, migration policy decision or deployment-specific backup/restore rehearsal.
See the [maintainer release procedure](releasing.md).

## Official acceptance and access

The official entry point is
[`console-release-acceptance.yml`](../.github/workflows/console-release-acceptance.yml)
in `soulteary/oc`, manually dispatched from `main` for the actual OC and OtterIO
timestamp tags. It must finish successfully. A PR run, another workflow,
cancelled/failed run, local JSON or renamed candidate report is not a substitute.
The resulting attestation binds both repositories' selected assets and runtime
evidence; keep its exact bytes and independently review its SHA-256 before
requesting publication.

```sh
gh workflow run console-release-acceptance.yml --repo soulteary/oc --ref main \
  -f oc_tag="$OC_TAG" -f otterio_tag="$OTTERIO_TAG"
```

The workflow uploads the `console-release-acceptance` artifact with
`acceptance.json`, `pair.json`, `upgrade.json`, `images.json` and the original pair
child reports. Its current retention is 90 days; an expired artifact cannot
unlock publication. The step summary prints the run ID and the SHA-256 of
`acceptance.json`. Download and inspect the actual JSON rather than accepting
the summary alone:

```sh
gh run download "$ACCEPTANCE_RUN_ID" --repo soulteary/oc \
  --name console-release-acceptance --dir "$ACCEPTANCE_DIRECTORY"
shasum -a 256 "$ACCEPTANCE_DIRECTORY/acceptance.json"
```

If the official acceptance artifact expires while the verified drafts remain
unchanged, dispatch a fresh official manual acceptance run and review its new
run ID and hash. Preserve prior failed/expired reports; do not reconstruct or
overwrite a previous run's attestation. The publisher requires exactly one
unexpired named artifact and matching run-attempt provenance. Expiry is not
permission to reconstruct the old attestation or rebuild over version tags.

The official document must identify `evidenceKind: draft-release-pair`,
`passed: true` and `publishable: true`. Its workflow provenance and raw-report
hashes are validated again by publication. A local report instead identifies
`local-packaged-candidate-pair`, remains `publishable: false`, and records images
as not certified even if its packaged functional and upgrade checks pass.

Draft access must be verified with the actual `CONSOLE_RELEASE_TOKEN` secret
on **both** repositories before dispatch. The REST endpoints list **Contents:
read**, and official run/artifact reads need **Actions: read**, on exactly
`soulteary/oc` and `soulteary/otterio`. These endpoint permissions alone do not
establish draft visibility: [GitHub's release API documentation](https://docs.github.com/en/rest/releases/releases#list-releases)
separately requires push access for draft listings. A successful `/user` request
or public-release listing is not a draft-access test. A maintainer must choose a
credential authorized to read both drafts and verify both collection lookup and
the selected numeric release ID return the expected stable draft and assets.
Do not assume that enlarging a scope is required or sufficient; inspect the
credential's repository selection, permissions and draft access first.

The October 22 diagnostic run [39817718011](https://github.com/soulteary/oc/actions/runs/39817718011)
authenticated the configured reader as `soulteary`, but requests for OC draft ID
`420956983` and OtterIO draft ID `420886261` both returned HTTP 403; neither target
tag appeared in the reader's collections. That is an access failure, not proof
that the draft tag or assets are missing. Keep both drafts and immutable assets
unchanged while repairing access. On October 23, after replacing the configured
reader, attempt 2 of the same diagnostic returned HTTP 200 for both fixed IDs,
with matching tag/source identities, stable-draft state and 13 OC / 8 OtterIO
assets. This resolves that credential preflight failure. The subsequent official
[acceptance run 39877163013](https://github.com/soulteary/oc/actions/runs/39877163013)
selects OC `RELEASE.2026-10-22T16-25-21Z` and OtterIO
`RELEASE.2026-10-22T15-10-13Z`; successful draft reads alone do not certify its
packaged functional, upgrade/rollback or image-runtime checks.

The acceptance workflow requires the cross-repository secret explicitly; its
own repository token is not a fallback. Its workflow permissions remain read
only. The publisher uses the secret only for read operations and its own
`GITHUB_TOKEN` for the final mutation of its own release. Keep token values in
Actions secrets and out of release notes, reports and logs. This guide does not
assert that the cross-repository secret or package access is configured.

## What the acceptance must bind

Review the exact eight OtterIO assets and thirteen OC assets, excluding GitHub's
generated source archives. Check asset filenames and bytes, each manifest's
tag/source, archive or executable checksums and the actual programs extracted
from the selected OC archive. The current OtterIO draft build regenerates its
checksum file after recording registry identities: its seven entries cover the
six executables **and** `release-manifest.json`. The eight-asset inventory is
unchanged. Check immutable image repository/digest pairs and executable contents
against the selected packages. Runtime checks execute the primary GHCR image;
every optional registry mirror must have the same immutable manifest digest.
A mirror with different contents is rejected even if its own version tag matches
its recorded digest. Mirror runtime is not separately executed.

OtterIO's actual draft body contains both the tagged root release notes and the
full tagged English migration announcement, with relative links bound to that
source commit. The attestation records `releaseNotesSHA256`; publication rechecks
that body as well as the accepted assets. A separate document in Git alone is
not a substitute for the announcement being in the public Release body.

The independently identified **previous** OtterIO October 10 release has the
older six-entry executable-only checksum file. Its later-created manifest has
no checksum entry; bind those previous manifest bytes to the independently
trusted official asset digest as described in the [upgrade guide](console-migration-candidate.md#real-upgrade-and-rollback).
Do not apply that historical exclusion to the new draft checksum contract.

The official hosted workflow executes packaged programs on Linux amd64. Darwin
arm64 packaged execution, when separately run and recorded, is local evidence;
it is not a second platform within that official workflow attestation. The first
intended functional scope is direct browser HTTPS, shared or independent split
S3/Admin HTTPS
with independent CAs, and the declared local/native/OIDC identity profiles.
Object/settings/version/copy behavior, native IAM and relationship writes,
permission denials, Range, permission-scoped diagnostics, WebIdentity STS and
real Chromium OIDC require their actual recorded checks. Image execution on
Linux amd64/arm64 is a separate result; it must pass by digest before those image
architectures can be described as runtime-accepted. Those checks compare each
image's executables with its matching packages, execute OC/Console version/help
and an OC filesystem copy, and exercise the server's signed S3 startup/round-trip
probe. The probe is [reviewed OC code](../buildscripts/verify-s3-startup.go) from
the immutable workflow-dispatch checkout and runs with that checkout's `go.mod`
and `go.sum`, with workspace overrides disabled and read-only module resolution.
The image report fingerprints the probe, acceptance wrapper and Go dependency
files and rejects changes during execution. OtterIO supplies packaged programs
and image bytes; its candidate checkout or probe cannot select or weaken these
runtime assertions. They do not run the whole Console functional suite inside
each image.
The hosted workflow installs QEMU for architecture execution; arm64 execution
there is not a claim of native arm64 hardware testing. Cross-built archives alone
do not establish execution on other platforms.

Upgrade acceptance must start the actual October 10 published OC/OtterIO
programs, upgrade to the selected packaged programs, then actually restart the
previous programs on the declared isolated storage state. Its report must name
the old and new program hashes, manifests and sources. It checks both HTTPS
arrangements, the seeded object/version/tag/versioning/IAM/deny fixture, old
local preference compatibility, isolated new native preferences and restart
persistence. Old/local preference checks use loopback HTTP; new native checks
use browser HTTPS. The separate new-object write and frozen-snapshot restore
checks define the tested rollback boundary. They do not certify an OIDC
cross-version upgrade or arbitrary later IAM, secret, lifecycle or external
changes.

The [28-capability mapping](console-migration-capabilities.md) records deliberate
withdrawals and uncovered scope. Diagnostics is a snapshot, not a certified long
stream. Proxy TLS termination, multiple replicas, gateways, distributed or
federated storage, shared etcd/external OPA and every IdP/browser remain outside
this first rollout. Formal acceptance must state its own passed scope and must
not relabel the historical matrix as entirely verified.

## Publication inputs and final review

For each repository, run the manual publication workflow on `main` using the
already prepared tag and the **same official OC acceptance run** and attestation
hash for the pair:

```sh
gh workflow run publish-release.yml --repo soulteary/oc --ref main \
  -f tag="$OC_TAG" -f acceptance_run_id="$ACCEPTANCE_RUN_ID" \
  -f attestation_sha256="$ATTESTATION_SHA256"
```

After OC publication succeeds, the corresponding OtterIO request is:

```sh
gh workflow run publish-release.yml --repo soulteary/otterio --ref main \
  -f tag="$OTTERIO_TAG" -f acceptance_run_id="$ACCEPTANCE_RUN_ID" \
  -f attestation_sha256="$ATTESTATION_SHA256"
```

Set these variables from the actual reviewed tags/run/report, not a newly
generated timestamp or a SHA-256 copied from development evidence. The helper
rechecks the current assets and fixed-image identities; a changed or missing
accepted file must fail closed. A successful run and JSON hash are a binding to
that workflow's evidence, not a Cosign/Sigstore signature or a guarantee for all
deployments.

Both publishers require a `main` dispatch and check out its exact
`${{ github.sha }}`. A queued job does not switch to a later `main` helper. The
publisher runs its locally reviewed validation code from that dispatch commit;
remote Python source is checked by hash and is not imported or executed as
validation code. The official acceptance run's recorded source and report hashes
remain evidence bindings, distinct from the publisher's trusted local code.

The operator must review the approved release body and
[English migration announcement](https://github.com/soulteary/otterio/blob/main/docs/releases/console-migration-announcement.en.md)
before public Web removal. The accelerated notice plus verified
older-server/snapshot rollback decision is authorized. Its revision becomes
effective only at the first approved public Web-free release containing that
announcement, not through a tag build, acceptance run or this document. Final
publication approval remains separate. The October 10 notice and unfinished
historical windows remain recorded as history.

## Evidence status

This document describes the publication gate; it does not announce a new release
or claim an official packaged-pair run has passed. Local packaged execution can
help verify the runner but is not an official main-branch workflow attestation
and cannot unlock publication. Preserve genuine failed runs, exact manifests,
runtime reports and their scope alongside any later passing report.
