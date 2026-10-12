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
