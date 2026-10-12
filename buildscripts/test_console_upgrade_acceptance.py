"""Fail-closed provenance and failure cleanup for the actual upgrade fixture."""
import io
import json
import os
from pathlib import Path
import signal
import subprocess
import sys
import tarfile
import tempfile
import unittest
from unittest.mock import Mock, patch

import console_upgrade_acceptance as upgrade


class UpgradeEvidenceTests(unittest.TestCase):
    def main_arguments(self, root, manifest, expected):
        arguments = ['console_upgrade_acceptance.py']
        for option in ('previous-server', 'previous-cli', 'previous-console', 'server', 'cli', 'console',
                       'previous-checksums', 'previous-oc-manifest', 'previous-oc-checksums', 'previous-oc-archive'):
            arguments += ['--' + option, str(root / option)]
        arguments += ['--previous-manifest', str(manifest), '--previous-manifest-sha256', expected]
        for option in ('previous-source', 'server-source', 'cli-source'):
            arguments += ['--' + option, 'a' * 40]
        arguments += ['--previous-release', 'release', '--previous-oc-release', 'oc-release',
                      '--profiles', 'single-tls', '--output', str(root / 'report.json')]
        return arguments

    def test_manifest_sha256_accepts_exact_trusted_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / 'manifest.json'
            manifest.write_bytes(b'official manifest bytes')
            expected = upgrade.PROTOCOL.digest(manifest)
            self.assertEqual(upgrade.manifest_sha256_bound(manifest, expected), expected)
            self.assertEqual(upgrade.manifest_sha256_bound(manifest, expected.upper()), expected)

    def test_manifest_sha256_rejects_invalid_expectation_before_reading_file(self):
        with patch.object(upgrade.PROTOCOL, 'digest') as digest:
            for expected in ('', 'a' * 63, 'a' * 65, 'g' * 64, 'sha256:' + 'a' * 64, 'a' * 64 + '\n', None):
                with self.subTest(expected=expected), self.assertRaisesRegex(RuntimeError, 'invalid expected'):
                    upgrade.manifest_sha256_bound(Path('must-not-be-read'), expected)
            digest.assert_not_called()

    def test_manifest_sha256_rejects_wrong_trusted_hash(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / 'manifest.json'
            manifest.write_bytes(b'official manifest bytes')
            with self.assertRaisesRegex(RuntimeError, 'SHA-256 mismatch'):
                upgrade.manifest_sha256_bound(manifest, '0' * 64)

    def test_manifest_sha256_rejects_tamper_with_unchanged_tag_and_source(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / 'manifest.json'
            data = {'schema_version': 1, 'tag': 'release', 'source_sha': 'a' * 40}
            manifest.write_text(json.dumps(data))
            expected = upgrade.PROTOCOL.digest(manifest)
            manifest.write_text(json.dumps({**data, 'substituted': True}))
            self.assertEqual(upgrade.manifest_bound(manifest, 'release', 'a' * 40)['source'], 'a' * 40)
            with self.assertRaisesRegex(RuntimeError, 'SHA-256 mismatch'):
                upgrade.manifest_sha256_bound(manifest, expected)

    def test_main_rejects_bad_manifest_before_parsing_or_starting_profiles(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = root / 'manifest.json'
            data = {'schema_version': 1, 'tag': 'release', 'source_sha': 'a' * 40}
            manifest.write_text(json.dumps(data))
            trusted = upgrade.PROTOCOL.digest(manifest)
            for case, expected in [('invalid', 'g' * 64), ('wrong', '0' * 64), ('tampered', trusted)]:
                manifest.write_text(json.dumps({**data, 'substituted': True}) if case == 'tampered' else json.dumps(data))
                with self.subTest(case=case), patch.object(sys, 'argv', self.main_arguments(root, manifest, expected)), \
                     patch.object(upgrade.signal, 'signal'), patch.object(upgrade, 'manifest_bound') as parse_manifest, \
                     patch.object(upgrade, 'run_profile') as run_profile, patch.object(upgrade, 'program_identity') as identity, \
                     patch('sys.stdout', new_callable=io.StringIO):
                    self.assertEqual(upgrade.main(), 1)
                    parse_manifest.assert_not_called()
                    run_profile.assert_not_called()
                    identity.assert_not_called()
                report = json.loads((root / 'report.json').read_text())
                self.assertFalse(report['passed'])
                self.assertEqual(report['profiles'], [])
                self.assertEqual(report['preparationErrorClass'], 'RuntimeError')
                self.assertIn('manifest', report['reason'])

    def test_main_records_trusted_manifest_verification_before_profile(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            manifest = root / 'manifest.json'
            manifest.write_text(json.dumps({'schema_version': 1, 'tag': 'release', 'source_sha': 'a' * 40}))
            (root / 'previous-oc-manifest').write_text(json.dumps({'schema_version': 1, 'release_tag': 'oc-release', 'source_commit': 'a' * 40}))
            expected = upgrade.PROTOCOL.digest(manifest)
            events = []
            actual_sha256_bound, actual_manifest_bound = upgrade.manifest_sha256_bound, upgrade.manifest_bound
            def check_sha256(*args):
                result = actual_sha256_bound(*args)
                events.append('SHA-256 verified')
                return result
            def parse_manifest(*args, **kwargs):
                self.assertIn('SHA-256 verified', events)
                events.append('manifest parsed')
                return actual_manifest_bound(*args, **kwargs)
            def profile(args, name):
                self.assertEqual(events, ['SHA-256 verified', 'manifest parsed', 'manifest parsed'])
                return {'profile': name, 'status': 'passed', 'cleanup': 'passed'}
            with patch.object(sys, 'argv', self.main_arguments(root, manifest, expected)), \
                 patch.object(upgrade.signal, 'signal'), patch.object(upgrade, 'manifest_sha256_bound', side_effect=check_sha256), \
                 patch.object(upgrade, 'manifest_bound', side_effect=parse_manifest), \
                 patch.object(upgrade, 'checksum_bound', return_value='b' * 64), patch.object(upgrade, 'archive_bound'), \
                 patch.object(upgrade, 'program_identity', return_value={}), patch.object(upgrade, 'run_profile', side_effect=profile), \
                 patch('sys.stdout', new_callable=io.StringIO):
                self.assertEqual(upgrade.main(), 0)
            server = json.loads((root / 'report.json').read_text())['previousReleaseManifests']['server']
            self.assertEqual(server['expectedManifestSHA256'], expected)
            self.assertEqual(server['manifestSHA256'], expected)
            self.assertEqual(server['verification'], 'matches-expected-sha256')

    def test_checksum_requires_exact_unique_asset(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            binary, sums = root / 'otterio-linux-amd64', root / 'checksums.txt'
            binary.write_bytes(b'published bytes')
            digest = upgrade.PROTOCOL.digest(binary)
            sums.write_text(digest + '  ' + binary.name + '\n')
            self.assertEqual(upgrade.checksum_bound(binary, sums), digest)
            sums.write_text(digest + '  path/' + binary.name + '\n')
            with self.assertRaisesRegex(RuntimeError, 'missing or ambiguous'):
                upgrade.checksum_bound(binary, sums)
            sums.write_text((digest + '  ' + binary.name + '\n') * 2)
            with self.assertRaisesRegex(RuntimeError, 'missing or ambiguous'):
                upgrade.checksum_bound(binary, sums)
            sums.write_text('0' * 64 + '  ' + binary.name + '\n')
            with self.assertRaisesRegex(RuntimeError, 'checksum mismatch'):
                upgrade.checksum_bound(binary, sums)

    def test_manifest_formats_bind_tag_and_source(self):
        with tempfile.TemporaryDirectory() as directory:
            manifest = Path(directory) / 'manifest.json'
            source = 'a' * 40
            for kind, data in [('server', {'tag': 'release', 'source_sha': source}),
                               ('oc', {'release_tag': 'release', 'source_commit': source})]:
                manifest.write_text(json.dumps({'schema_version': 1, **data}))
                self.assertEqual(upgrade.manifest_bound(manifest, 'release', source, kind)['source'], source)
                with self.assertRaises(RuntimeError):
                    upgrade.manifest_bound(manifest, 'other', source, kind)
                with self.assertRaisesRegex(RuntimeError, 'source differs'):
                    upgrade.manifest_bound(manifest, 'release', 'b' * 40, kind)

    def test_verified_archive_must_contain_exact_program_bytes_once(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            archive, binary = root / 'oc.tar.gz', root / 'oc'
            binary.write_bytes(b'expected program')
            def package(contents):
                with tarfile.open(archive, 'w:gz') as output:
                    for name, data in contents:
                        item = tarfile.TarInfo(name)
                        item.size = len(data)
                        output.addfile(item, io.BytesIO(data))
            package([('release/oc', binary.read_bytes())])
            upgrade.archive_bound(archive, {'oc': binary})
            package([('release/oc', b'wrong program')])
            with self.assertRaisesRegex(RuntimeError, 'differs'):
                upgrade.archive_bound(archive, {'oc': binary})
            package([('a/oc', binary.read_bytes()), ('b/oc', binary.read_bytes())])
            with self.assertRaisesRegex(RuntimeError, 'missing or duplicate'):
                upgrade.archive_bound(archive, {'oc': binary})

    def test_candidate_dirty_or_wrong_revision_cannot_certify_upgrade(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'oc'; binary.write_bytes(b'program')
            for revision, dirty in [('b' * 40, 'false'), ('a' * 40, 'true'), (None, None)]:
                settings = [] if revision is None else [{'Key': 'vcs.revision', 'Value': revision}, {'Key': 'vcs.modified', 'Value': dirty}]
                result = Mock(stdout=json.dumps({'Settings': settings, 'Deps': []}))
                with patch.object(upgrade.subprocess, 'run', return_value=result):
                    with self.assertRaises(RuntimeError):
                        upgrade.program_identity(binary, 'a' * 40, 'candidate-source-build')

    def test_historical_modified_metadata_is_retained_not_relabelled_clean(self):
        with tempfile.TemporaryDirectory() as directory:
            binary = Path(directory) / 'otterio'; binary.write_bytes(b'published')
            result = Mock(stdout=json.dumps({'Settings': [{'Key': 'vcs.revision', 'Value': 'a' * 40}, {'Key': 'vcs.modified', 'Value': 'true'}]}))
            with patch.object(upgrade.subprocess, 'run', return_value=result):
                identity = upgrade.program_identity(binary, 'a' * 40, 'release-asset')
            self.assertEqual(identity['embeddedModified'], 'true')

    def test_live_process_stopped_before_fixture_deletion_on_failure(self):
        process = Mock()
        observations = []
        with patch.object(upgrade.PROTOCOL, 'stop', side_effect=lambda p: observations.append((p, root.exists()))):
            with self.assertRaisesRegex(RuntimeError, 'fixture failure'):
                with upgrade.fixture_directory([process], []) as root:
                    (root / 'data').write_bytes(b'volume')
                    raise RuntimeError('fixture failure')
        self.assertEqual(observations, [(process, True)])
        self.assertFalse(root.exists())

    def test_snapshot_digest_detects_new_files_and_modified_bytes(self):
        with tempfile.TemporaryDirectory() as directory:
            root = Path(directory)
            (root / 'a').write_bytes(b'original')
            baseline = upgrade.tree_digest(root)
            (root / 'a').write_bytes(b'changed')
            self.assertNotEqual(upgrade.tree_digest(root), baseline)
            (root / 'a').write_bytes(b'original')
            (root / 'new').write_bytes(b'after upgrade')
            self.assertNotEqual(upgrade.tree_digest(root), baseline)

    def test_first_termination_during_cleanup_stops_every_writer_before_deletion(self):
        first, last = Mock(), Mock()
        observations = []
        def stop(process):
            observations.append((process, root.exists()))
            if len(observations) == 1:
                raise KeyboardInterrupt
        with patch.object(upgrade.PROTOCOL, 'stop', side_effect=stop):
            with self.assertRaises(KeyboardInterrupt):
                with upgrade.fixture_directory([first, last], []) as root:
                    (root / 'data').write_bytes(b'volume')
        self.assertEqual(observations, [(last, True), (last, True), (first, True)])
        self.assertFalse(root.exists())

    @unittest.skipUnless(os.name == 'posix', 'POSIX subprocess signal semantics')
    def test_sigterm_cleans_actual_child_and_volume_before_exit(self):
        code = '\n'.join([
            'import json, signal, subprocess, sys, time',
            'from pathlib import Path',
            'sys.path.insert(0, ' + repr(str(Path(upgrade.__file__).parent)) + ')',
            'import console_upgrade_acceptance as fixture',
            'signal.signal(signal.SIGTERM, fixture.handle_termination)',
            'processes = []',
            'try:',
            '    with fixture.fixture_directory(processes, []) as root:',
            '        (root / "volume").write_bytes(b"fixture")',
            '        child = subprocess.Popen([sys.executable, "-c", "import time; time.sleep(30)"])',
            '        processes.append(child)',
            '        print(json.dumps({"pid": child.pid, "root": str(root)}), flush=True)',
            '        time.sleep(30)',
            'except KeyboardInterrupt:',
            '    print(json.dumps({"passed": False, "interrupted": True, "cleanup": all(p.poll() is not None for p in processes)}), flush=True)',
        ])
        parent = subprocess.Popen([sys.executable, '-c', code], stdout=subprocess.PIPE,
                                  stderr=subprocess.PIPE, text=True)
        try:
            ready = json.loads(parent.stdout.readline())
            os.kill(parent.pid, signal.SIGTERM)
            stdout, stderr = parent.communicate(timeout=15)
            self.assertEqual(parent.returncode, 0, stderr)
            report = json.loads(stdout)
            self.assertEqual(report, {'passed': False, 'interrupted': True, 'cleanup': True})
            self.assertFalse(Path(ready['root']).exists())
            with self.assertRaises(ProcessLookupError): os.kill(ready['pid'], 0)
        finally:
            if parent.poll() is None:
                parent.kill(); parent.communicate()


if __name__ == '__main__':
    unittest.main()
