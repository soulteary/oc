# OC release preparation for 2026 10 07

> Historical release preparation record. Source cutoffs and acceptance items below describe preparation, not a claim that every later release completed every check. The archive-only `RELEASE.2026-10-07T14-10-00Z` release records source `29bb213e69b57e369d7d96e115c5463df8ef53ac` and has no image entries; subsequent container-enabled releases must record their own digest identities. For the current procedure see [releasing OC](../releasing.md).

Prepare a fresh timestamp release after this PR merges and its resulting main
commit passes Go and Code scanning. This preparation does not reserve a tag or
publish artifacts. Its archive and container workflow follows OtterIO's tag
convention.

## Source range

Git baseline: `RELEASE.2026-10-07T04-39-33Z`, commit
`074c7782812b29ebbeff61202c2eb8c842d9f556`.
No published OC GitHub release was returned by the release API during preparation;
this tag is a source baseline, not proof of an earlier artifact publication.
Reviewed implementation cutoff: `63d7d2ad53eeb4c14f5c2e168282340ff93ea842`.
[Source comparison](https://github.com/soulteary/oc/compare/074c7782812b29ebbeff61202c2eb8c842d9f556...63d7d2ad53eeb4c14f5c2e168282340ff93ea842).

- #1 and its follow-up fixes: Windows notification decoding, cancellation and
  lifetime handling, filename limits, filesystem attributes, mirror recovery,
  multipart observation, vulnerability tooling and license packaging.
- `95b460f8`: configuration/endpoint validation, JSON output, cache synchronization,
  live mirror filtering and the third pinned-server HTTP compatibility patch.
- #2–#4 and accompanying retention regressions: duration input and calendar bounds,
  representable retention dates, clear operations and validation before copying.
- This preparation: reviewed bilingual notes, exact-source release preflight,
  eleven archive targets, checksums/manifest, draft download verification and guide;
  Linux amd64/arm64 container publication from the same executables, registry
  digests and separate stable-release `latest` promotion.

The SDK pin stays at the October 4 server commit with three fixture patches.
The companion OtterIO preparation includes those server-side fixes but does not
silently change OC's dependency or broaden compatibility claims.

## Acceptance

- [ ] Reconcile later main changes, merge this PR, and record the final source SHA.
- [ ] Confirm Go and Code scanning success on that exact main commit.
- [ ] Create a fresh annotated or signed UTC tag from clean synchronized main.
- [ ] Confirm Release success, thirteen uploaded assets, correct licenses,
  checksums and manifest/runtime source identity.
- [ ] Confirm GHCR amd64/arm64 images and any enabled Docker Hub images match the
  manifest digests and runtime release tag, and contain the required licenses.
- [ ] Set the new GHCR package to public and confirm an unauthenticated pull.
- [ ] Confirm Stable release promotion updates `latest` only after publication
  and only for the newest stable release, then marks that GitHub release as
  latest; manual releases retain the opt-in.
- [ ] Verify platform-specific aliases/TLS, copy/mirror filtering, retention,
  cancellation and configuration migration before rollout.

See [the release guide](../releasing.md). Publication and runtime acceptance are
completed separately from the PR's build and regression checks.
