# Releasing OC

OC uses `RELEASE.YYYY-MM-DDTHH-MM-SSZ` in UTC, matching OtterIO.
The [release preparation](releases/2026-10-07-release-review.md) records this
update's source range. Root `RELEASE_NOTES.md` supplies the GitHub release body.
Generate the actual tag after the preparation PR is merged and the resulting
main commit passes **Go** and **Code scanning - action**. A green PR run does not
replace these exact-commit main checks.

## Prepare the tag

From a clean checkout with Git and Python 3:

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
FreeBSD amd64; Windows amd64/arm64. Each archive contains `oc` or `oc.exe`,
LICENSE, NOTICE, CREDITS, the notification MIT license, both READMEs and the
compatibility manifest. Windows uses ZIP; other platforms use tar.gz.

Eleven archives plus `release-manifest.json` and `checksums.txt` make thirteen
uploaded assets. The SHA-256 file covers all archives and the manifest.
The manifest records the tag, source SHA, SDK/toolchain baseline and archive
hashes. Runtime version flags come from the validated tag and actual source SHA,
without requiring SemVer or changing the SDK dependency.

Only the publication job has contents write permission. It refuses existing
published releases and prerelease drafts, rechecks the remote tag, verifies local
checksums, uploads to a stable draft and compares every downloaded filename and
byte before publishing. Publication leaves the GitHub latest marker unchanged.
Container images, package registries and latest aliases are outside this workflow.
`.goreleaser.yml` remains available for local snapshot/package builds with
publication disabled; this workflow does not invoke it.

## Verify and recover

Download all thirteen files into one directory and run:

```sh
shasum -a 256 -c checksums.txt
```

Extract your platform archive, check `oc --version` and match its tag/commit to
`release-manifest.json`. Verify normal operations and your deployment's TLS,
mirror and retention settings before replacing production binaries.
Checksums and the manifest are identity/integrity records, not signatures.
Cross-compilation does not establish runtime acceptance on every target.

If a job fails before publication, inspect the draft and rerun the original
workflow, or dispatch **Release** with the same existing tag while it remains a
stable draft. Published releases are never overwritten. Extra/stale draft assets
cause filename comparison to fail; investigate and remove only erroneous draft
assets before retrying. For a changed source or published version, create a new
tag after main checks pass. No automated self-update channel is enabled.
