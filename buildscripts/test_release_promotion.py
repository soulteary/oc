"""Offline OC manifest and promotion policy regressions; no publication."""
import copy
import importlib.util
import json
from pathlib import Path
import subprocess
import sys
import tempfile
import unittest

sys.dont_write_bytecode = True
SCRIPT = Path(__file__).with_name("release-promotion.py")
SPEC = importlib.util.spec_from_file_location("promotion", SCRIPT)
promotion = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(promotion)
TAG = "RELEASE.2026-10-07T07-00-00Z"
OLDER = "RELEASE.2026-10-06T07-00-00Z"
NEWER = "RELEASE.2026-10-08T07-00-00Z"
SHA = "a" * 40
DIGEST = "sha256:" + "b" * 64
REPOSITORY = "soulteary/oc"


def archive_manifest():
    return {
        "schema_version": 1,
        "release_tag": TAG,
        "source_commit": SHA,
        "go_toolchain": "1.27.1",
        "otterio_sdk": "v0.0.0-20261004215341-be8596f0d69d",
        "assets": [
            {"name": "oc-" + TAG + "-linux-amd64.tar.gz", "target": "linux/amd64", "sha256": "c" * 64},
            {"name": "oc-" + TAG + "-windows-arm64.zip", "target": "windows/arm64", "sha256": "d" * 64},
        ],
    }


def published(tag, **kwargs):
    return dict(tag_name=tag, draft=False, prerelease=False, **kwargs)


class ManifestTests(unittest.TestCase):
    def build(self, **kwargs):
        args = dict(tag=TAG, source_sha=SHA, repository=REPOSITORY, digest=DIGEST,
                    dockerhub_published="false", dockerhub_user="", manifest=archive_manifest())
        args.update(kwargs)
        return promotion.build_manifest(**args)

    def test_archive_schema_and_nested_metadata_are_preserved(self):
        original = archive_manifest()
        original["future_metadata"] = {"notes": ["retained", "unchanged"]}
        before = copy.deepcopy(original)
        manifest = self.build(manifest=original)
        self.assertEqual(original, before)
        self.assertEqual({key: value for key, value in manifest.items() if key != "images"}, before)
        self.assertNotIn("tag", manifest)
        self.assertNotIn("source_sha", manifest)
        self.assertEqual(manifest["images"], [{"repository": "ghcr.io/soulteary/oc", "digest": DIGEST}])
        # Image augmentation does not share mutable archive metadata with its input.
        manifest["assets"][0]["name"] = "changed"
        self.assertEqual(original, before)

    def test_dockerhub_uses_the_actual_configured_identity(self):
        for user in ("soulteary", "docker-owner"):
            with self.subTest(user=user):
                manifest = self.build(repository="SoulTeary/OC", dockerhub_published="true", dockerhub_user=user)
                self.assertEqual(manifest["images"], [
                    {"repository": "ghcr.io/soulteary/oc", "digest": DIGEST},
                    {"repository": user + "/oc", "digest": DIGEST},
                ])

    def test_unselected_dockerhub_is_omitted(self):
        self.assertEqual(len(self.build(dockerhub_user="soulteary")["images"]), 1)

    def test_missing_build_outputs_fail_closed(self):
        for status in (None, "", "TRUE", "0", True, False, "false\n"):
            with self.subTest(status=status), self.assertRaisesRegex(ValueError, "publication status"):
                self.build(dockerhub_published=status)
        for digest in (None, "", "latest", "sha256:abc", DIGEST + "\n", "sha256:" + "z" * 64):
            with self.subTest(digest=digest), self.assertRaisesRegex(ValueError, "image digest"):
                self.build(digest=digest)

    def test_dockerhub_publication_requires_a_valid_username(self):
        for user in ("", None, "SoulTeary", "owner/other", " owner", "owner\n", "***"):
            with self.subTest(user=user), self.assertRaisesRegex(ValueError, "Docker Hub username"):
                self.build(dockerhub_published="true", dockerhub_user=user)

    def test_archive_tag_and_source_commit_must_match(self):
        for key, value in (("release_tag", OLDER), ("source_commit", "e" * 40)):
            original = archive_manifest()
            original[key] = value
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "current tag commit"):
                self.build(manifest=original)

    def test_archive_metadata_is_required(self):
        for field, value in (("schema_version", True), ("go_toolchain", ""), ("otterio_sdk", None), ("assets", [])):
            original = archive_manifest()
            original[field] = value
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.build(manifest=original)
        with self.assertRaises(ValueError):
            self.build(manifest={"schema_version": 1, "tag": TAG, "source_sha": SHA})

    def test_archive_checksum_and_duplicate_assets_are_rejected(self):
        original = archive_manifest()
        original["assets"][0]["sha256"] = "incorrect"
        with self.assertRaisesRegex(ValueError, "checksum"):
            self.build(manifest=original)
        original = archive_manifest()
        original["assets"].append(copy.deepcopy(original["assets"][0]))
        with self.assertRaisesRegex(ValueError, "duplicate"):
            self.build(manifest=original)

    def test_invalid_source_and_repository_are_rejected(self):
        for field, values in {
            "source_sha": (None, "", "a" * 39, SHA + "\n"),
            "repository": (None, "", "owner", "ghcr.io/owner/repo", "owner/repo\n"),
        }.items():
            for value in values:
                with self.subTest(field=field, value=value), self.assertRaises(ValueError):
                    self.build(**{field: value})

    def test_cli_augments_existing_manifest_and_invalid_write_leaves_it_untouched(self):
        with tempfile.TemporaryDirectory() as directory:
            output = Path(directory) / "release-manifest.json"
            output.write_text(json.dumps(archive_manifest()), encoding="utf-8")
            command = [sys.executable, str(SCRIPT), TAG, "--write-manifest", str(output),
                       "--source-sha", SHA, "--repository", REPOSITORY,
                       "--dockerhub-published", "false", "--digest"]
            result = subprocess.run(command + [DIGEST], capture_output=True, text=True, timeout=10)
            self.assertEqual(result.returncode, 0, result.stderr)
            self.assertEqual(json.loads(output.read_text(encoding="utf-8")), self.build())
            self.assertTrue(output.read_bytes().endswith(b"\n"))
            before = output.read_bytes()
            result = subprocess.run(command + [""], capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(output.read_bytes(), before)
            mismatched = json.loads(before)
            mismatched["source_commit"] = "e" * 40
            output.write_text(json.dumps(mismatched), encoding="utf-8")
            before = output.read_bytes()
            result = subprocess.run(command + [DIGEST], capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(output.read_bytes(), before)
            output.unlink()
            result = subprocess.run(command + [DIGEST], capture_output=True, text=True, timeout=10)
            self.assertNotEqual(result.returncode, 0)
            self.assertFalse(output.exists())

    def test_invalid_separate_input_does_not_truncate_destination(self):
        with tempfile.TemporaryDirectory() as directory:
            source = Path(directory) / "input.json"
            source.write_text(json.dumps(archive_manifest()), encoding="utf-8")
            output = Path(directory) / "output.json"
            output.write_bytes(b"preserve existing destination\n")
            result = subprocess.run(
                [sys.executable, str(SCRIPT), TAG, "--write-manifest", str(output),
                 "--manifest", str(source), "--source-sha", SHA, "--repository", REPOSITORY,
                 "--dockerhub-published", "false", "--digest", "invalid"],
                capture_output=True, text=True, timeout=10,
            )
            self.assertNotEqual(result.returncode, 0)
            self.assertEqual(output.read_bytes(), b"preserve existing destination\n")


class PromotionTests(unittest.TestCase):
    def setUp(self):
        self.manifest = promotion.build_manifest(
            TAG, SHA, REPOSITORY, DIGEST, "false", manifest=archive_manifest()
        )

    def plan(self, releases=None, **kwargs):
        return promotion.promotion_plan(
            TAG, self.manifest, releases if releases is not None else [[published(TAG)]],
            SHA, REPOSITORY, **kwargs,
        )

    def test_newest_release_promotes_and_retry_preserves_digest(self):
        before = copy.deepcopy(self.manifest)
        self.assertTrue(self.plan([[published(OLDER), published(TAG)]])["promote"])
        self.assertEqual(self.plan(), self.plan())
        self.assertEqual(self.plan()["images"][0]["digest"], DIGEST)
        self.assertEqual(self.manifest, before)

    def test_older_release_on_later_page_cannot_roll_back_latest(self):
        plan = self.plan([[published(TAG)], [published(OLDER)], [published(NEWER)]])
        self.assertFalse(plan["promote"])
        self.assertIn(NEWER, plan["reason"])

    def test_unpublished_or_unstable_candidate_is_rejected(self):
        with self.assertRaisesRegex(ValueError, "not a published stable"):
            self.plan([[published(OLDER)]])
        for field in ("draft", "prerelease"):
            candidate = published(TAG)
            candidate[field] = True
            with self.subTest(field=field), self.assertRaises(ValueError):
                self.plan([[candidate]])

    def test_newer_draft_and_prerelease_do_not_block_stable(self):
        for field in ("draft", "prerelease"):
            newer = published(NEWER)
            newer[field] = True
            with self.subTest(field=field):
                self.assertTrue(self.plan([[published(TAG), newer]])["promote"])

    def test_archive_identity_cannot_change_before_promotion(self):
        for key, value in (("release_tag", OLDER), ("source_commit", "e" * 40)):
            original = copy.deepcopy(self.manifest)
            self.manifest[key] = value
            with self.subTest(key=key), self.assertRaisesRegex(ValueError, "current tag commit"):
                self.plan()
            self.manifest = original

    def test_digest_and_registry_allowlist_are_enforced(self):
        for value in ("latest", "sha256:abc", DIGEST + "\n", "sha256:" + "z" * 64):
            self.manifest["images"][0]["digest"] = value
            with self.subTest(value=value), self.assertRaises(ValueError):
                self.plan()
        self.manifest["images"][0]["digest"] = DIGEST
        for name in ("example.invalid/other/repo", "ghcr.io/***/oc", "", None):
            self.manifest["images"][0]["repository"] = name
            with self.subTest(name=name), self.assertRaisesRegex(ValueError, "unexpected"):
                self.plan()

    def test_duplicate_repository_is_rejected(self):
        self.manifest["images"].append(dict(self.manifest["images"][0]))
        with self.assertRaisesRegex(ValueError, "duplicate"):
            self.plan()

    def test_dockerhub_requires_configured_identity_and_ghcr_remains_primary(self):
        self.manifest["images"].append({"repository": "docker-owner/oc", "digest": DIGEST})
        with self.assertRaises(ValueError):
            self.plan()
        self.assertTrue(self.plan(dockerhub_user="docker-owner")["has_dockerhub"])
        self.manifest["images"] = self.manifest["images"][1:]
        with self.assertRaisesRegex(ValueError, "primary GHCR"):
            self.plan(dockerhub_user="docker-owner")

    def test_manifest_without_images_is_not_silently_adopted(self):
        self.manifest.pop("images")
        with self.assertRaisesRegex(ValueError, "one or two images"):
            self.plan()

    def test_invalid_tags(self):
        for value in (None, "main", "v1.2.3", TAG + "\n", "RELEASE.2026-02-29T00-00-00Z", "RELEASE.$(touch injected)"):
            with self.subTest(value=value), self.assertRaises(ValueError):
                promotion.release_time(value)

    def test_historical_non_timestamp_tags_are_ignored(self):
        self.assertTrue(self.plan([[published(TAG), published("v0.1.0")]])["promote"])

    def test_malformed_release_listing_is_rejected(self):
        for releases in (None, {}, [[published(TAG)], ["invalid"]]):
            with self.subTest(releases=releases), self.assertRaises(ValueError):
                promotion.promotion_plan(TAG, self.manifest, releases, SHA, REPOSITORY)

    def test_cli_emits_plan_with_preserved_image_digest(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / "release-manifest.json"
            manifest.write_text(json.dumps(self.manifest), encoding="utf-8")
            releases = Path(directory) / "published.json"
            releases.write_text(json.dumps([[published(TAG)], [published(NEWER)]]), encoding="utf-8")
            result = subprocess.run(
                [sys.executable, str(SCRIPT), TAG, "--manifest", str(manifest),
                 "--published", str(releases), "--source-sha", SHA, "--repository", REPOSITORY],
                capture_output=True, text=True, timeout=10,
            )
            self.assertEqual(result.returncode, 0, result.stderr)
            plan = json.loads(result.stdout)
            self.assertFalse(plan["promote"])
            self.assertEqual(plan["images"], self.manifest["images"])


if __name__ == "__main__":
    unittest.main()
