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

## Published assets

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

The publication job refuses existing published releases and prerelease drafts,
rechecks the remote tag, verifies local checksums, uploads to a stable draft and
compares every downloaded filename and byte before publishing. Publication leaves
the GitHub latest marker unchanged.
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
directly after the image name. The workflow checks the pushed image by digest
before publishing the GitHub release. It verifies that the index contains exactly
Linux amd64/arm64, then executes `--version`, `--help` and a local copy smoke test
on Linux amd64 runs console version/help checks, and compares both executables and the licenses with the prepared
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

An automatic tag push requests a `latest` update after the GitHub release is
published. The separate **Stable release promotion** workflow copies the verified
release digest to `latest` only when that tag is the newest published stable OC
release. The publication job and the entire promotion workflow share the
`oc-stable-promotion` concurrency group. Preserve this shared lock when changing
the workflows; putting it on the reusable workflow's caller would prevent the
called promotion from acquiring it. Publication must finish and release the lock
before requesting promotion.

A manually dispatched **Release** defaults `promote_latest` to `false`;
enable it when the manual release should request the same promotion. Promotion
does not rebuild executables or change version tags. After all image aliases
have been verified, its final step marks that GitHub release as latest.

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
the original workflow, or dispatch **Release** with the same existing tag while
it remains a stable draft. Extra/stale draft assets cause filename comparison to
fail; investigate and remove only erroneous draft assets before retrying.

Once any version image has been pushed, use a fresh timestamp tag after main
checks pass, even if the GitHub release is still a draft or another registry push
failed. The image guard intentionally prevents retrying that partially used tag;
do not delete or overwrite images to bypass it. Published GitHub releases are
also never overwritten.

If publication succeeds but `latest` promotion fails, keep the published release
and its version images. Dispatch **Stable release promotion** for that release
tag to retry promotion by its recorded digest. The workflow still requires the
newest published stable release, so an older release cannot roll `latest` back.
Successful promotion also sets GitHub's latest release marker after verifying
the registry aliases.
No automated self-update channel is enabled.
