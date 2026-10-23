# Releasing OC

OC uses `RELEASE.YYYY-MM-DDTHH-MM-SSZ` in UTC, matching OtterIO.

[Documentation index](README.md) · [Installation](installation.md) · [Maintainer workflow](MAINTAINERS.md) · [简体中文](zh_CN/releasing.md)

Merge source and documentation changes first, then wait for **Go** and
**Code scanning - action** to pass on that exact main commit before creating a
fresh tag. A green PR run does not replace these exact-commit main checks.
Also review **CLI compatibility** for that commit and resolve or explicitly
approve contract changes before publication. The release job automatically
gates only `go.yml` and `codeql.yml`; it does not enforce the separate CLI
contract workflow.
Root `RELEASE_NOTES.md` supplies the GitHub release body. Keep dated preparation
records under `docs/releases/`; the [2026-10-10 preparation](releases/2026-10-10-release-review.md)
records the current source range, and the [2026-10-07 preparation](releases/2026-10-07-release-review.md)
remains historical. Neither record reserves a future tag.

The current process separates **draft preparation**, **verified publication**
and **stable promotion**. Tag pushes and manual `release.yml` runs stop at a
verified stable draft; they do not publish it or request `latest`. Fixed-version
registry images may already be public at that point. A stable draft is not an
RC and does not imply private image delivery. Earlier release records and the
retained Chinese guide describe their original workflow; use this English
procedure for the staged process.

## Prepare the tag

From a clean checkout with Git and Python 3.11 or newer:

```sh
git switch main &&
git fetch origin main --tags &&
git pull --ff-only origin main &&
TAG="RELEASE.$(date -u +%Y-%m-%dT%H-%M-%SZ)" &&
python3 buildscripts/release-preflight.py "$TAG" &&
git tag -a "$TAG" -m "OC $TAG" &&
git push origin "refs/tags/$TAG"
```

With a configured signing key, replace the annotation command with `git tag -s`
and verify it with `git verify-tag` before pushing. Do not reuse, move or delete a
published or partially used tag. The read-only preflight verifies syntax, a clean
main synchronized with origin/main, no local tag reuse and nonempty notes; it does
not fetch or prove remote CI success. The Release workflow enforces that gate.
Do not create a separate formal GitHub release manually.

## Draft assets

The workflow directly builds the eleven targets in `docs/compatibility.json`:
Linux amd64, arm64, arm (GOARM=7), 386, ppc64le and s390x; macOS amd64/arm64;
FreeBSD amd64; Windows amd64/arm64. Each archive contains `oc` and `oc-console` (both with `.exe` on Windows),
LICENSE, NOTICE, CREDITS, the notification MIT license, both READMEs and the
compatibility manifest. Windows uses ZIP; other platforms use tar.gz.

Eleven archives plus `release-manifest.json` and `checksums.txt` make thirteen
uploaded assets. The SHA-256 file covers all archives and the manifest.
The manifest records the tag, source SHA, SDK/toolchain baseline, archive hashes
and published container image digests. `storage_sdk` and `otterio_kits` record
the independent S3 client and its published kits; the legacy `otterio_sdk` field
continues to identify the server/admin module. Runtime version flags come from the
validated tag and actual source SHA, without requiring SemVer or changing the
SDK dependency. The workflow refreshes the manifest checksum after recording
the image digests.

The draft job refuses existing published releases and prerelease drafts,
rechecks the remote tag, verifies local checksums, uploads to a stable draft and
compares every downloaded filename and byte. It then leaves the release a draft.
Manual publication requires the official packaged-pair acceptance described
below; neither operation moves the GitHub latest marker.
`.goreleaser.yml` remains available for local snapshot/package builds with
publication disabled; this workflow does not invoke it.
That local configuration has ten build targets, omitting `linux/386`, and uses
its own archive layout and package metadata. Its output is not interchangeable
with the eleven-target release builder or its `release-manifest.json`.
The formal archives and images contain both `oc` and `oc-console`.

## Container images

Releases from the container-enabled workflow publish
`ghcr.io/soulteary/oc:RELEASE.YYYY-MM-DDTHH-MM-SSZ`
for `linux/amd64` and `linux/arm64`. Images contain the exact executables from the
matching release archives, CA certificates, LICENSE, NOTICE, CREDITS and the
notification MIT license. The image's entrypoint is `oc`; use `--entrypoint oc-console` for the console. Pass client arguments
directly after the image name. The draft workflow checks the pushed image by digest.
It verifies that the index contains exactly
Linux amd64/arm64, then executes `--version`, `--help` and a local copy smoke test
on Linux amd64, checks console version/help, and compares both executables and the licenses with the prepared
archive context. The arm64 archive is hash-checked during context preparation,
but the workflow does not run the arm64 image; two-platform publication is not
two-platform runtime acceptance. Older archive-only releases, including
`RELEASE.2026-10-07T14-10-00Z`, have no `images` entry and do not establish image
availability. Use [the container guide](containers.md) to select and run a version
whose manifest records image identities.

To also publish Docker Hub images, set both repository Actions secrets
`DOCKERHUB_USERNAME` and `DOCKERHUB_TOKEN`. The image name is
`DOCKERHUB_USERNAME/oc` with the same timestamp tag and platforms. With neither
secret configured, Docker Hub is skipped. Configuring only one secret fails the
release with a credential configuration error. GHCR uses the workflow's GitHub
token and does not require these secrets.

For the first GHCR publication, set the `oc` package's visibility to **Public**
in GitHub's package settings to allow the unauthenticated pulls shown in the
READMEs. A new GHCR package defaults to private; linking it to a public repository
does not replace this visibility setting. See
[GitHub's Container registry guide](https://docs.github.com/en/packages/working-with-a-github-packages-registry/working-with-the-container-registry).

Version tags are never overwritten. Before pushing, the workflow refuses a
version tag that already exists in any enabled registry. See the recovery rules
below if an earlier attempt pushed only some images.

Neither tag pushes nor manual draft/publication runs request a `latest` update.
The separately dispatched **Stable release promotion** workflow copies the
verified release digest to `latest` only when the tag is the newest published
stable OC release. Draft asset mutation, manual publication and the entire
promotion workflow share the `oc-stable-promotion` concurrency group. Preserve
that shared lock. Promotion does not rebuild executables or change version tags;
after all image aliases verify, its final step marks the release as GitHub latest.

For a reproducible deployment, use a timestamp tag or a digest from
`release-manifest.json`. The `latest` tag is a moving alias. For example, replace
the placeholder with a published release tag:

```sh
TAG="RELEASE.YYYY-MM-DDTHH-MM-SSZ"
docker run --rm "ghcr.io/soulteary/oc:$TAG" --version
docker run --rm -v "$HOME/.oc:/root/.oc" "ghcr.io/soulteary/oc:$TAG" --help
```

Mount `/root/.oc` to persist aliases, certificates and other client configuration
between runs.

## Verify and recover

### Accept the packaged pair and publish unchanged bytes

After both repositories have verified drafts, dispatch the official
`soulteary/oc` [`console-release-acceptance.yml`](../.github/workflows/console-release-acceptance.yml)
on `main` for the actual pair. Review its successful run, accepted asset/program
and image identities, runtime scope and attestation bytes. Local source reports
and local packaged runs cannot replace that official workflow evidence.
Cross-repository reads require `CONSOLE_RELEASE_TOKEN` with Contents read
and Actions read on both repositories, plus verified access to both actual
drafts. GitHub restricts draft listings to callers with push access; endpoint
read permissions or public-release visibility alone do not establish that access.
The default repository token is not a cross-repository fallback. The publisher uses that token only for reads
and its own `GITHUB_TOKEN` for the final release mutation. See the [packaged-pair acceptance guide](console-release-acceptance.md)
for exact access and scope boundaries.

Review the English release body, supported upgrade/restore boundary and selected
migration policy before authorizing publication. The
[English announcement](https://github.com/soulteary/otterio/blob/main/docs/releases/console-migration-announcement.en.md)
records the approved accelerated decision, effective only at the first approved
public Web-free release containing it;
the historical two-notice/default-off/fallback sequence has not been completed.

Run manual publication from `main` using the existing OC tag, successful official
run ID and independently reviewed attestation SHA-256:

```sh
gh workflow run publish-release.yml --repo soulteary/oc --ref main \
  -f tag="$OC_TAG" -f acceptance_run_id="$ACCEPTANCE_RUN_ID" \
  -f attestation_sha256="$ATTESTATION_SHA256"
```

The publisher rechecks the unchanged accepted draft bytes and immutable image
identities, then makes that stable release public with `latest=false`. It does
not rebuild, replace assets or request promotion. Publish OC before the accepted
OtterIO partner. A JSON hash plus official workflow checks is an evidence binding,
not a new asset signature or certification of every platform/deployment.

Publication is restricted to a `main` dispatch, but its helper checkout is the
exact dispatch commit, `${{ github.sha }}`, rather than the moving `main` branch.
Waiting for the shared publication/promotion lock does not adopt later helper
changes. Dispatch a new run if a newer reviewed helper is required.

### Inspect public artifacts and recover

Download all thirteen files into one directory and run:

```sh
shasum -a 256 -c checksums.txt
```

Extract your platform archive and match the `oc --version` tag to
`release-manifest.json`; the manifest records the full source commit, which
`--version` does not print. For containers, match the registry digest to the
manifest, run the image by digest with `--version` and `--help`, and check the
reported release tag. Verify normal operations and your deployment's TLS, mirror
and retention settings before replacing production binaries or images.
Checksums and the manifest are identity/integrity records, not signatures.
Cross-compilation does not establish runtime acceptance on every target.

If a job fails before any version image is pushed, inspect the draft and rerun
the original workflow, or dispatch the draft build with the same existing tag while
it remains a stable draft. Extra/stale draft assets cause filename comparison to
fail; investigate and remove only erroneous draft assets before retrying.

Once any version image has been pushed, use a fresh timestamp tag after main
checks pass, even if the GitHub release is still a draft or another registry push
failed. The image guard intentionally prevents retrying that partially used tag;
do not delete or overwrite images to bypass it. Published GitHub releases are
also never overwritten.

If official acceptance or manual publication fails, keep the verified draft and
investigate the failing identity or runtime check. Publication retries consume
the same unchanged accepted bytes; they do not invoke the build pipeline. Changed
programs, assets, manifests or image identities require a fresh release identity
and acceptance rather than a permissive publisher or an edited attestation.

After publication, leave `latest` unchanged until an operator explicitly
dispatches **Stable release promotion** for that release
tag to promote or retry its recorded digest. The workflow still requires the
newest published stable release, so an older release cannot roll `latest` back.
Successful promotion also sets GitHub's latest release marker after verifying
the registry aliases.
No automated self-update channel is enabled.

```sh
gh workflow run promote-release.yml --repo soulteary/oc --ref main \
  -f tag="$OC_TAG"
```

## Prepare an immutable release tag from Actions

After the exact main commit passes its required checks, run **Prepare release tag**
on `main` with a new UTC `RELEASE.YYYY-MM-DDTHH-MM-SSZ` tag. The workflow refuses
existing tags, a changed main branch, dirty source, or incomplete CI. It creates
the tag once and explicitly dispatches the existing draft builder from that tag
because token-created tag pushes do not trigger Actions. This prepares a draft
only; packaged-pair acceptance, publication, and latest promotion remain separate.

If dispatch fails after tag creation, retain the tag and manually run the existing
Release workflow for that exact tag. Never delete or move the tag to retry.
