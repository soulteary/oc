"""Execute the real tag guard with a fake Buildx; never contact a registry."""
import os
from pathlib import Path
import shutil
import subprocess
import tempfile
import unittest

GUARD = Path(__file__).with_name("check-new-image-tags.sh").resolve()
TAG = "RELEASE.2026-10-04T07-00-00Z"


class ImageReferenceTests(unittest.TestCase):
    def probe(self, reference, expected, error=None, exit_code=1, shadow_bash=False):
        bash = os.environ.get("OC_TEST_BASH") or shutil.which("bash")
        self.assertIsNotNone(bash, "Bash is required to test the image tag guard")
        bash = str(Path(bash).resolve())
        with tempfile.TemporaryDirectory(prefix="oc image refs ") as directory:
            docker = Path(directory) / "docker"
            # Native Windows text writes would put CRLF in the shebang.
            docker.write_bytes(
                b'#!/bin/sh\n'
                b'[ "$#" -eq 4 ] && [ "$1 $2 $3" = "buildx imagetools inspect" ] || exit 98\n'
                b'printf "%s" "$4" > "$MOCK_CAPTURE"\n'
                b'printf "%s\\n" "$MOCK_ERROR" >&2\n'
                b'exit "$MOCK_EXIT"\n'
            )
            docker.chmod(0o755)
            if shadow_bash:
                decoy = Path(directory) / "bash"
                decoy.write_bytes(b'#!/bin/sh\necho "fixture Bash must not run" >&2\nexit 97\n')
                decoy.chmod(0o755)
            # A script avoids Windows command-line quoting of a Bash -c string.
            # Reuse the running interpreter after adding the mock to PATH.
            runner = Path(directory) / "run-guard.sh"
            runner.write_bytes(b'export PATH="$PWD:$PATH"\nexec "$BASH" "$@"\n')
            capture = Path(directory) / "reference"
            env = dict(
                os.environ, MOCK_CAPTURE=capture.name, MOCK_EXIT=str(exit_code),
                MOCK_ERROR=error if error is not None else "ERROR: " + expected + ": not found",
            )
            result = subprocess.run(
                [bash, runner.as_posix(), GUARD.as_posix(), reference],
                cwd=directory, env=env,
                text=True, capture_output=True, timeout=10,
            )
            self.assertTrue(capture.is_file(),
                            f"mock Docker did not run with {bash!r} (exit {result.returncode}); "
                            f"stdout={result.stdout!r}; stderr={result.stderr!r}")
            self.assertEqual(capture.read_text(), expected)
            return result

    def test_fixture_cannot_shadow_running_bash(self):
        ref = "ghcr.io/soulteary/oc:" + TAG
        self.assertEqual(self.probe(ref, ref, shadow_bash=True).returncode, 0)

    def test_dockerhub_short_reference(self):
        ref = "soulteary/oc:" + TAG
        self.assertEqual(self.probe(ref, "docker.io/" + ref).returncode, 0)

    def test_dockerhub_qualified_reference(self):
        ref = "docker.io/soulteary/oc:" + TAG
        self.assertEqual(self.probe(ref, ref).returncode, 0)

    def test_legacy_dockerhub_domain(self):
        self.assertEqual(self.probe(
            "index.docker.io/soulteary/oc:" + TAG,
            "docker.io/soulteary/oc:" + TAG,
        ).returncode, 0)

    def test_dockerhub_library_names(self):
        for ref in ("alpine:" + TAG, "docker.io/alpine:" + TAG):
            with self.subTest(reference=ref):
                self.assertEqual(self.probe(ref, "docker.io/library/alpine:" + TAG).returncode, 0)

    def test_explicit_registries_are_unchanged(self):
        for registry in ("ghcr.io", "localhost:5000", "registry:5000", "registry.example.com:5443"):
            ref = registry + "/soulteary/oc:" + TAG
            with self.subTest(reference=ref):
                self.assertEqual(self.probe(ref, ref).returncode, 0)

    def test_existing_tag_is_rejected(self):
        ref = "docker.io/soulteary/oc:" + TAG
        result = self.probe(ref, ref, error="", exit_code=0)
        self.assertNotEqual(result.returncode, 0)
        self.assertIn("Refusing to overwrite", result.stderr)

    def test_unrelated_not_found_is_rejected(self):
        ref = "docker.io/soulteary/oc:" + TAG
        for error in (
            "proxy hostname: not found",
            "ERROR: docker.io/other/oc:" + TAG + ": not found",
            "ERROR: " + ref + "-other: not found",
            "ERROR: " + ref + ": not found (proxy failure)",
        ):
            with self.subTest(error=error):
                self.assertNotEqual(self.probe(ref, ref, error).returncode, 0)

    def test_auth_network_and_rate_limit_fail_closed(self):
        ref = "docker.io/soulteary/oc:" + TAG
        for error in ("401 Unauthorized", "403 Forbidden", "429 Too Many Requests", "TLS handshake timeout", "no such host"):
            with self.subTest(error=error):
                self.assertNotEqual(self.probe(ref, ref, error).returncode, 0)

    def test_manifest_unknown_remains_supported(self):
        ref = "docker.io/soulteary/oc:" + TAG
        for error in ("ERROR: manifest unknown", "MANIFEST_UNKNOWN"):
            with self.subTest(error=error):
                self.assertEqual(self.probe(ref, ref, error).returncode, 0)


if __name__ == "__main__":
    unittest.main()
