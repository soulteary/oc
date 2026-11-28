# OC release notes

Changes since [OC RELEASE.2026-11-25T20-30-01Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-11-25T20-30-01Z).

## Console uploads of new objects

[OC #58](https://github.com/soulteary/oc/pull/58) fixes create-only uploads larger than 16 MiB failing with `501 NotImplemented` on servers that reject destination conditions during multipart initiation. OC now sends `If-None-Match: *` on the committing PUT or multipart completion request, while initiation only creates the upload session. An object created at the destination before completion is preserved and the upload reports a conflict. Failed or unconfirmed completion is not retried as an unconditional overwrite.

## Conditional Console rename

[OC #59](https://github.com/soulteary/oc/pull/59) protects both steps of rename. Copying requires an absent destination and the selected source ETag; deleting the source requires that same ETag. A concurrent destination is not overwritten, and a source with a different ETag at deletion is not removed. Successful copies retain normal metadata and tag copy behavior.

Rename requires Signature V4 and the separate `X-Otterio-Conditional-Copy: v1` and `X-Otterio-Conditional-Delete: v1` capabilities introduced by [OtterIO #55](https://github.com/soulteary/otterio/pull/55). When the server does not advertise these capabilities, OC returns `501 rename_unsupported` before copying or deleting an object. The older create-only upload capability is insufficient. Rename remains within one bucket and supports objects up to 5 GiB. The initial server capabilities support local filesystem and local single-pool erasure storage without object caching or bucket federation; other object and bucket restrictions can still reject an operation.

Copy and deletion are separate requests. If copying succeeds but source deletion fails or is unconfirmed, OC reports `rename_partial` and leaves the destination in place. When a different-ETag source is written after copying, conditional deletion fails with `412`, preserving that updated source and the copied destination. Check both names before retrying. OC does not roll back the destination or fall back to unconditional copy or deletion.

ETags are content validators, not unique revision identifiers. Same-ETag rewrites, changes away from and back to the same ETag, and metadata-only changes that retain the ETag are not distinguished. Rename does not request a historical version purge or retention bypass; current-key deletion in a versioned bucket follows the server's normal delete-marker behavior. See [rename semantics](https://github.com/soulteary/oc/blob/e1f87c84cdf2725f6b3f33cbfaa1fd1fe1c49068/docs/console.md#rename-semantics) for the supported contract and failure handling.

## Upgrade and rollback

This release retains `github.com/soulteary/otterio-sdk/v7 v7.3.4` and the existing server/admin module pin. The remote OtterIO server is selected and verified separately; upgrading OC alone does not add the required rename capabilities to an older server.

Deploy the exact accepted OC/OtterIO pair. Retain the previous programs or immutable image digests and back up configuration before rollout. Pause affected writers, upgrade the server before OC, and validate the configured credentials, permissions and object operations before resuming writes. This deployment order is separate from the documented release-publication order.

For rollback, pause writes and restore the previously validated program/configuration pair. Reconcile any partial or unconfirmed operation before retrying it. Replacing binaries does not undo committed uploads, copies or deletions; returning to an older OC also restores its earlier mutation behavior. Keep Console writes disabled until the restored pair has been checked. Restore the complete previous dependency lockfiles when rebuilding an older source version.

## Validation and release provenance

The permanent [conditional mutation fixture](https://github.com/soulteary/oc/blob/e1f87c84cdf2725f6b3f33cbfaa1fd1fe1c49068/docs/development.md#verify-conditional-uploads-and-rename) exercises fourteen real HTTP scenarios across temporary filesystem and local four-disk erasure servers. It covers large uploads, multipart source ETags, metadata and tags, destination races and source changes before copy or deletion. Its ordinary unencrypted objects and unversioned buckets do not establish support for every object configuration or deployment.

Release publication uses the existing [packaged-pair acceptance](https://github.com/soulteary/oc/blob/e1f87c84cdf2725f6b3f33cbfaa1fd1fe1c49068/docs/console-release-acceptance.md) to bind both releases' exact archive programs, immutable image digests, manifests and release notes. Source regression reports supplement that acceptance; they do not replace its native/HTTPS/OIDC, permissions, S3/Range, readiness and documented migration/rollback checks.
