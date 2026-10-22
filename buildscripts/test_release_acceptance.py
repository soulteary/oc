"""Packaged-input boundaries and owned runtime cleanup, without external services."""
import hashlib
import copy
import io
import json
import os
from pathlib import Path
import shlex
import shutil
import signal
import subprocess
import sys
import tarfile
import tempfile
import time
import unittest
from unittest.mock import patch

import console_release_acceptance as acceptance
from test_release_stage import OfficialFixture, TAGS


def archive(directory, tag, target='linux-amd64', members=None):
    stem = f'oc-{tag}-{target}'
    path = Path(directory) / (stem + '.tar.gz')
    members = members if members is not None else [(stem + '/oc', b'CLI'), (stem + '/oc-console', b'Console')]
    with tarfile.open(path, 'w:gz') as output:
        for name, data in members:
            row = tarfile.TarInfo(name)
            if isinstance(data, bytes):
                row.size, row.mode = len(data), 0o755
                output.addfile(row, io.BytesIO(data))
            else:
                row.type, row.linkname = tarfile.SYMTYPE, data
                output.addfile(row)
    return path


def immutable_inventory(directory):
    return {str(path.relative_to(directory)): (hashlib.sha256(path.read_bytes()).hexdigest(), path.stat().st_mode)
            for path in Path(directory).rglob('*') if path.is_file()}


class PackageSelectionTests(unittest.TestCase):
    def test_extracts_only_exact_regular_programs_without_changing_archive(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            stem = 'oc-tag-linux-amd64'
            source = archive(root, 'tag', members=[(stem + '/oc', b'CLI'), (stem + '/oc-console', b'Console'),
                             ('../../escaped', b'unsafe extra'), (stem + '/extra-link', '../../escaped')])
            before = source.read_bytes(), source.stat().st_mode
            destination = root / 'programs'
            destination.mkdir()
            result = acceptance.unpack_oc(root, 'tag', 'linux-amd64', destination)
            self.assertEqual({p.name for p in destination.iterdir()}, {'oc', 'oc-console'})
            self.assertEqual(result['oc'].read_bytes(), b'CLI')
            self.assertEqual(result['console'].read_bytes(), b'Console')
            self.assertEqual(source.read_bytes(), before[0])
            self.assertEqual(source.stat().st_mode, before[1])
            self.assertFalse((root / 'escaped').exists())

    def test_wrong_target_or_missing_program_fails_before_writing(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            destination = root / 'programs'
            destination.mkdir()
            archive(root, 'tag', members=[('oc-tag-linux-amd64/oc', b'CLI'),
                                         ('oc-tag-linux-arm64/oc-console', b'wrong architecture')])
            with self.assertRaisesRegex(ValueError, 'missing or unsafe'):
                acceptance.unpack_oc(root, 'tag', 'linux-amd64', destination)
            self.assertEqual(list(destination.iterdir()), [])

    def test_rejects_duplicate_names_including_unselected_members(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            stem = 'oc-tag-linux-amd64'
            for duplicate in (stem + '/oc', stem + '/README'):
                with self.subTest(duplicate=duplicate):
                    archive(root, 'tag', members=[(stem + '/oc', b'CLI'), (stem + '/oc-console', b'Console'),
                                                 (duplicate, b'first'), (duplicate, b'second')])
                    with self.assertRaisesRegex(ValueError, 'duplicate'):
                        acceptance.unpack_oc(root, 'tag', 'linux-amd64', root)

    def test_rejects_symlink_and_empty_selected_program(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            stem = 'oc-tag-linux-amd64'
            for value in ('../../external', b''):
                with self.subTest(value=value):
                    archive(root, 'tag', members=[(stem + '/oc', b'CLI'), (stem + '/oc-console', value)])
                    with self.assertRaisesRegex(ValueError, 'missing or unsafe'):
                        acceptance.unpack_oc(root, 'tag', 'linux-amd64', root)

    def test_rejects_excessive_members(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            archive(root, 'tag', members=[(f'extra-{index}', b'x') for index in range(257)])
            with self.assertRaisesRegex(ValueError, 'excessive'):
                acceptance.unpack_oc(root, 'tag', 'linux-amd64', root)


class LocalEvidenceTests(unittest.TestCase):
    def prepare(self, root):
        fixture_root = root / 'fixture'
        fixture_root.mkdir()
        fixture = OfficialFixture(fixture_root)
        previous_oc, previous_ot = root / 'previous-oc', root / 'previous-ot'
        previous_oc.mkdir()
        previous_ot.mkdir()
        archive(previous_oc, acceptance.PREVIOUS_OC_TAG)
        arguments = ['console_release_acceptance.py', '--oc-tag', TAGS['soulteary/oc'],
                     '--otterio-tag', TAGS['soulteary/otterio'],
                     '--oc-assets', str(fixture.directories['soulteary/oc']),
                     '--otterio-assets', str(fixture.directories['soulteary/otterio']),
                     '--previous-oc', str(previous_oc), '--previous-otterio', str(previous_ot),
                     '--output', str(root / 'result')]
        return fixture, arguments

    def profile(self, command, report):
        Path(report).write_text(json.dumps({'passed': True, 'programs': {'previousServer': {'binarySHA256': 'a' * 64}}}))
        return True

    def test_successful_local_inputs_remain_immutable_and_never_publishable(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fixture, arguments = self.prepare(root)
            before = {repo: immutable_inventory(path) for repo, path in fixture.directories.items()}
            with patch.object(sys, 'argv', arguments), patch.object(acceptance, 'host_target', return_value='linux-amd64'), \
                 patch.object(acceptance, 'run_profile', side_effect=self.profile) as profile, \
                 patch.object(acceptance, 'image_acceptance', return_value=True) as image, \
                 patch.object(acceptance.STAGE, 'validate_evidence') as publish_validator, \
                 patch.dict(os.environ, {'GITHUB_REPOSITORY': 'soulteary/oc', 'GITHUB_REF': 'refs/heads/main'}), \
                 patch('sys.stdout', new_callable=io.StringIO):
                self.assertEqual(acceptance.main(), 0)
            document = json.loads((root / 'result/acceptance.json').read_text())
            self.assertTrue(document['passed'])
            self.assertFalse(document['publishable'])
            self.assertEqual(document['evidenceKind'], 'local-packaged-candidate-pair')
            self.assertNotIn('provenance', document)
            self.assertEqual(profile.call_count, 2)
            image.assert_not_called()
            publish_validator.assert_not_called()
            for repository, path in fixture.directories.items():
                self.assertEqual(immutable_inventory(path), before[repository])
            server = Path(profile.call_args_list[0].args[0][profile.call_args_list[0].args[0].index('--server') + 1])
            self.assertNotEqual(server.parent, fixture.directories['soulteary/otterio'])
            self.assertFalse(server.exists())
            images = json.loads((root / 'result/images.json').read_text())
            self.assertFalse(images['passed'])
            self.assertEqual(images['mode'], 'not-certified')

    def test_changed_local_assets_fail_even_after_successful_profiles(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fixture, arguments = self.prepare(root)
            def change(command, report):
                self.profile(command, report)
                (fixture.directories['soulteary/otterio'] / 'otterio-linux-amd64').write_bytes(b'replaced server')
                return True
            with patch.object(sys, 'argv', arguments), patch.object(acceptance, 'host_target', return_value='linux-amd64'), \
                 patch.object(acceptance, 'run_profile', side_effect=change), patch('sys.stdout', new_callable=io.StringIO):
                self.assertEqual(acceptance.main(), 1)
            document = json.loads((root / 'result/acceptance.json').read_text())
            self.assertFalse(document['passed'])
            self.assertFalse(document['publishable'])
            self.assertEqual(document['errorClass'], 'ValueError')

    def test_mixed_local_and_official_selection_fails_before_execution(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            _, arguments = self.prepare(root)
            arguments += ['--download-drafts']
            with patch.object(sys, 'argv', arguments), patch.object(acceptance.STAGE, 'inventory') as inventory, \
                 patch.object(acceptance.STAGE, 'snapshot') as snapshot, patch.object(acceptance, 'run_profile') as profile, \
                 patch('sys.stdout', new_callable=io.StringIO):
                self.assertEqual(acceptance.main(), 1)
            inventory.assert_not_called()
            snapshot.assert_not_called()
            profile.assert_not_called()
            self.assertFalse(json.loads((root / 'result/acceptance.json').read_text())['publishable'])

    def test_interrupted_profile_retains_failed_evidence_and_cleans_private_programs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fixture, arguments = self.prepare(root)
            before = immutable_inventory(fixture.directories['soulteary/otterio'])
            programs = []
            def interrupt(command, report):
                programs.append(Path(command[command.index('--server') + 1]))
                raise KeyboardInterrupt
            with patch.object(sys, 'argv', arguments), patch.object(acceptance, 'host_target', return_value='linux-amd64'), \
                 patch.object(acceptance, 'run_profile', side_effect=interrupt), patch('sys.stdout', new_callable=io.StringIO):
                self.assertEqual(acceptance.main(), 1)
            document = json.loads((root / 'result/acceptance.json').read_text())
            self.assertEqual(document['errorClass'], 'KeyboardInterrupt')
            self.assertFalse(document['publishable'])
            self.assertTrue(programs)
            self.assertTrue(all(not path.exists() for path in programs))
            self.assertEqual(immutable_inventory(fixture.directories['soulteary/otterio']), before)

    def official_mode(self, arguments):
        for flag in ('--oc-assets', '--otterio-assets'):
            offset = arguments.index(flag)
            del arguments[offset:offset + 2]
        return arguments + ['--download-drafts']

    def test_official_download_directories_do_not_collide_with_private_programs(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fixture, arguments = self.prepare(root)
            def snapshot(repository, tag, destination):
                shutil.copytree(fixture.directories[repository], destination)
                return copy.deepcopy(fixture.releases[repository])
            def images(releases, assets, output):
                self.assertTrue(all(path.is_dir() for path in assets.values()))
                Path(output).write_text('{"passed":true}')
                return True
            with patch.object(sys, 'argv', self.official_mode(arguments)), \
                 patch.object(acceptance, 'host_target', return_value='linux-amd64'), \
                 patch.object(acceptance.platform, 'system', return_value='Linux'), \
                 patch.object(acceptance.STAGE, 'snapshot', side_effect=snapshot) as snapshots, \
                 patch.object(acceptance.STAGE, 'validate_evidence') as validator, \
                 patch.object(acceptance, 'run_profile', side_effect=self.profile) as profiles, \
                 patch.object(acceptance, 'image_acceptance', side_effect=images), \
                 patch.dict(os.environ, {'GITHUB_REPOSITORY': 'soulteary/oc', 'GITHUB_RUN_ID': '123', 'GITHUB_RUN_ATTEMPT': '1',
                                        'GITHUB_SHA': 'e' * 40, 'GITHUB_EVENT_NAME': 'workflow_dispatch', 'GITHUB_REF': 'refs/heads/main'}), \
                 patch('sys.stdout', new_callable=io.StringIO):
                self.assertEqual(acceptance.main(), 0)
            document = json.loads((root / 'result/acceptance.json').read_text())
            self.assertTrue(document['publishable'])
            self.assertEqual(snapshots.call_count, 4)
            validator.assert_called_once()
            for call in profiles.call_args_list:
                command = call.args[0]
                for option in ('--cli', '--console', '--server'):
                    program = Path(command[command.index(option) + 1])
                    self.assertEqual(program.parent.name, 'programs')
                    self.assertFalse(program.exists())

    def test_official_draft_change_invalidates_successful_runtime_results(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            fixture, arguments = self.prepare(root)
            reads = []
            def snapshot(repository, tag, destination):
                shutil.copytree(fixture.directories[repository], destination)
                reads.append(repository)
                result = copy.deepcopy(fixture.releases[repository])
                if len(reads) > 2: result['releaseNotesSHA256'] = 'f' * 64
                return result
            with patch.object(sys, 'argv', self.official_mode(arguments)), \
                 patch.object(acceptance, 'host_target', return_value='linux-amd64'), \
                 patch.object(acceptance.platform, 'system', return_value='Linux'), \
                 patch.object(acceptance.STAGE, 'snapshot', side_effect=snapshot), \
                 patch.object(acceptance.STAGE, 'validate_evidence') as validator, \
                 patch.object(acceptance, 'run_profile', side_effect=self.profile), \
                 patch.object(acceptance, 'image_acceptance', return_value=True), \
                 patch('sys.stdout', new_callable=io.StringIO):
                self.assertEqual(acceptance.main(), 1)
            document = json.loads((root / 'result/acceptance.json').read_text())
            self.assertFalse(document['passed'])
            self.assertFalse(document['publishable'])
            self.assertEqual(document['reason'], 'candidate changed during acceptance')
            validator.assert_not_called()


@unittest.skipUnless(os.name == 'posix', 'packaged acceptance runs only on Linux/Darwin process groups')
class ProcessCleanupTests(unittest.TestCase):
    def wait_file(self, path, process=None):
        deadline = time.monotonic() + 5
        while not path.exists() and time.monotonic() < deadline:
            if process is not None and process.poll() is not None:
                self.fail('fixture wrapper exited before startup')
            time.sleep(0.02)
        self.assertTrue(path.exists(), 'fixture child did not start')

    def assert_stopped(self, pid):
        deadline = time.monotonic() + 3
        while time.monotonic() < deadline:
            try:
                os.kill(pid, 0)
            except ProcessLookupError:
                return
            state = Path('/proc') / str(pid) / 'stat'
            try:
                if state.is_file() and state.read_text().split(') ', 1)[1].startswith('Z '):
                    return
            except (FileNotFoundError, ProcessLookupError):
                # Linux can reap the child between the probe and /proc read.
                return
            time.sleep(0.02)
        self.fail('owned child remains running')

    def test_stopped_child_can_disappear_during_proc_state_read(self):
        for error in (FileNotFoundError, ProcessLookupError):
            with self.subTest(error=error.__name__), \
                 patch('os.kill'), patch.object(Path, 'is_file', return_value=True), \
                 patch.object(Path, 'read_text', side_effect=error):
                self.assert_stopped(12345)

    def test_subprocess_credentials_are_not_inherited_and_input_env_is_unchanged(self):
        env = dict(os.environ, GH_TOKEN='secret-one', GITHUB_TOKEN='secret-two', CONSOLE_RELEASE_TOKEN='secret-three', FIXTURE_SAFE='kept')
        code, output = acceptance.run_isolated([sys.executable, '-c',
            'import json,os; print(json.dumps({k:os.environ.get(k) for k in ("GH_TOKEN","GITHUB_TOKEN","CONSOLE_RELEASE_TOKEN","FIXTURE_SAFE")}))'], env, 5)
        self.assertEqual(code, 0)
        self.assertEqual(json.loads(output), {'GH_TOKEN': None, 'GITHUB_TOKEN': None, 'CONSOLE_RELEASE_TOKEN': None, 'FIXTURE_SAFE': 'kept'})
        self.assertEqual(env['GH_TOKEN'], 'secret-one')

    def test_profile_requires_both_success_and_report(self):
        with tempfile.TemporaryDirectory() as temporary:
            report = Path(temporary) / 'report.json'
            self.assertFalse(acceptance.run_profile([sys.executable, '-c', 'pass'], report))
            report.write_text('{}')
            self.assertFalse(acceptance.run_profile([sys.executable, '-c', 'raise SystemExit(7)'], report))
            self.assertTrue(acceptance.run_profile([sys.executable, '-c', 'pass'], report))

    def test_timeout_kills_term_ignoring_descendant_before_returning(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            child = root / 'child.py'
            pid = root / 'child.pid'
            child.write_text('import os,signal,time\nfrom pathlib import Path\nsignal.signal(signal.SIGTERM,signal.SIG_IGN)\n'
                             f'Path({str(pid)!r}).write_text(str(os.getpid()))\nwhile True: time.sleep(1)\n')
            command = [sys.executable, '-c', f'import subprocess,time; subprocess.Popen([{sys.executable!r},{str(child)!r}]); time.sleep(60)']
            with patch.object(acceptance, 'PROCESS_CLEANUP_TIMEOUT', 0.2):
                # Allow the Python wrapper and child to start on a loaded runner;
                # the child still ignores TERM and must be killed before return.
                code, output = acceptance.run_isolated(command, dict(os.environ), 5)
            self.assertEqual((code, output), (124, ''))
            self.assertTrue(pid.exists())
            self.assert_stopped(int(pid.read_text()))

    def test_real_sigterm_and_second_signal_finish_group_cleanup(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            child, pid, wrapper, cleaned = (root / name for name in ('child.py', 'child.pid', 'wrapper.py', 'cleaned'))
            child.write_text('import os,signal,time\nfrom pathlib import Path\nsignal.signal(signal.SIGTERM,signal.SIG_IGN)\n'
                             f'Path({str(pid)!r}).write_text(str(os.getpid()))\nwhile True: time.sleep(1)\n')
            wrapper.write_text('import sys,signal,os\nfrom pathlib import Path\n'
                f'sys.path.insert(0,{str(acceptance.ROOT / "buildscripts")!r})\nimport console_release_acceptance as a\n'
                'a.PROCESS_CLEANUP_TIMEOUT=0.3\nsignal.signal(signal.SIGTERM,a.handle_termination)\n'
                'try:\n'
                f' a.run_isolated([sys.executable,{str(child)!r}],dict(os.environ),60)\n'
                'except KeyboardInterrupt:\n'
                f' Path({str(cleaned)!r}).write_text("cleaned")\n')
            process = subprocess.Popen([sys.executable, str(wrapper)], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                self.wait_file(pid, process)
                process.send_signal(signal.SIGTERM)
                time.sleep(0.05)
                process.send_signal(signal.SIGTERM)
                stdout, stderr = process.communicate(timeout=5)
                self.assertEqual(process.returncode, 0, stderr.decode())
                self.assertTrue(cleaned.exists())
                self.assert_stopped(int(pid.read_text()))
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
                if pid.exists():
                    try: os.kill(int(pid.read_text()), signal.SIGKILL)
                    except ProcessLookupError: pass

    def test_real_cancellation_cleans_three_detached_sessions_cooperatively(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            server, inner, pair, wrapper = (root / name for name in ('server.py', 'inner.py', 'pair.py', 'wrapper.py'))
            server_pid = root / 'server.pid'
            server.write_text('import os,signal,time\nfrom pathlib import Path\nsignal.signal(signal.SIGTERM,signal.SIG_IGN)\n'
                              f'Path({str(server_pid)!r}).write_text(str(os.getpid()))\nwhile True: time.sleep(1)\n')
            protocol = acceptance.ROOT / 'buildscripts/test-real-protocol-ci.py'
            prefix = ('import os,sys,signal,importlib.util\nfrom pathlib import Path\n'
                      f'spec=importlib.util.spec_from_file_location("protocol",{str(protocol)!r})\n'
                      'h=importlib.util.module_from_spec(spec)\nspec.loader.exec_module(h)\n'
                      'def interrupted(*_): raise KeyboardInterrupt\nsignal.signal(signal.SIGTERM,interrupted)\n')
            for path, child, grace in ((inner, server, 0.2), (pair, inner, 0.8)):
                path.write_text(prefix + f'Path({str(path.with_suffix(".pid"))!r}).write_text(str(os.getpid()))\n'
                    'try:\n'
                    f' h.run([sys.executable,{str(child)!r}],dict(os.environ),60,cleanup_timeout={grace})\n'
                    'except KeyboardInterrupt:\n'
                    f' Path({str(path.with_suffix(".cleaned"))!r}).write_text("cleaned")\n')
            cleaned = root / 'wrapper.cleaned'
            wrapper.write_text('import sys,signal,os\nfrom pathlib import Path\n'
                f'sys.path.insert(0,{str(acceptance.ROOT / "buildscripts")!r})\nimport console_release_acceptance as a\n'
                'a.PROCESS_CLEANUP_TIMEOUT=2\nsignal.signal(signal.SIGTERM,a.handle_termination)\n'
                'try:\n'
                f' a.run_isolated([sys.executable,{str(pair)!r}],dict(os.environ),60)\n'
                'except KeyboardInterrupt:\n'
                f' Path({str(cleaned)!r}).write_text("cleaned")\n')
            process = subprocess.Popen([sys.executable, str(wrapper)], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
            try:
                self.wait_file(server_pid, process)
                pids = [int(path.read_text()) for path in (server_pid, inner.with_suffix('.pid'), pair.with_suffix('.pid'))]
                self.assertEqual(len({os.getpgid(pid) for pid in pids}), 3)
                process.send_signal(signal.SIGTERM)
                time.sleep(0.05)
                process.send_signal(signal.SIGTERM)
                stdout, stderr = process.communicate(timeout=5)
                self.assertEqual(process.returncode, 0, stderr.decode())
                self.assertTrue(cleaned.exists())
                self.assertTrue(inner.with_suffix('.cleaned').exists(), 'inner finally was force-killed')
                self.assertTrue(pair.with_suffix('.cleaned').exists(), 'pair finally was force-killed')
                for pid in pids: self.assert_stopped(pid)
            finally:
                if process.poll() is None:
                    process.kill()
                    process.wait()
                for path in (server_pid, inner.with_suffix('.pid'), pair.with_suffix('.pid')):
                    if path.exists():
                        try: os.kill(int(path.read_text()), signal.SIGKILL)
                        except ProcessLookupError: pass


class FakeDocker:
    """Track daemon-owned containers even when the attached client is killed."""
    def __init__(self, fixture, failure=None, probe_code=0):
        self.fixture, self.failure, self.probe_code = fixture, failure, probe_code
        self.active, self.created, self.removed, self.mounted = {}, [], [], []
        self.probe_calls = []

    def command(self, arguments, timeout=300):
        verb = arguments[0]
        if verb == 'pull': return ''
        if verb == 'create':
            name = arguments[arguments.index('--name') + 1]
            self.active[name] = arguments[-1]
            self.created.append(name)
            if self.failure == 'create': raise ValueError('fixture create failure')
            return 'id\n'
        if verb == 'cp':
            source, destination = arguments[1:]
            key = {'oc': 'oc', 'oc-console': 'console', 'otterio': 'server'}[source.rsplit('/', 1)[1]]
            Path(destination).write_bytes(b'wrong bytes' if self.failure == 'bytes' else self.fixture.package_bytes[key])
            return ''
        if verb == 'run':
            name = arguments[arguments.index('--name') + 1]
            self.active[name] = 'runtime'
            self.created.append(name)
            if self.failure == 'run': raise ValueError('fixture run failure')
            if '--mount' in arguments:
                mount = arguments[arguments.index('--mount') + 1]
                directory = Path(next(part[4:] for part in mount.split(',') if part.startswith('src=')))
                (directory / 'output').write_bytes((directory / 'input').read_bytes())
            self.active.pop(name)
            if '--version' in arguments:
                return TAGS['soulteary/oc' if '--entrypoint' in arguments else 'soulteary/otterio']
            return ''
        if verb == 'container':
            if self.failure == 'cleanup': raise ValueError('fixture query failure')
            name = arguments[arguments.index('--filter') + 1][len('name=^/'):-1]
            return name if name in self.active else ''
        if verb == 'rm':
            name = arguments[-1]
            # Mounted temporary data must remain present until daemon cleanup.
            self.assert_mounts_exist()
            self.active.pop(name)
            self.removed.append(name)
            return ''
        raise AssertionError('unexpected Docker operation')

    def assert_mounts_exist(self):
        if not all(path.exists() for path in self.mounted):
            raise AssertionError('mounted directory removed before daemon cleanup')

    def probe(self, command, env, timeout, cwd):
        self.probe_calls.append((command, env, timeout, cwd))
        launcher = Path(command[-1])
        arguments = shlex.split(launcher.read_text().splitlines()[1])
        name = arguments[arguments.index('--name') + 1]
        self.active[name] = 'daemon server'
        self.created.append(name)
        self.mounted = [Path(env['TMPDIR'])]
        self.assert_mounts_exist()
        if self.failure == 'interrupt': raise KeyboardInterrupt
        return self.probe_code, ''


class ImageCleanupTests(unittest.TestCase):
    def check(self, root, failure=None, probe_code=0, probe=None):
        fixture_root = root / 'fixture'
        fixture_root.mkdir()
        fixture = OfficialFixture(fixture_root)
        docker = FakeDocker(fixture, failure, probe_code)
        with patch.object(acceptance.STAGE, 'verify_images'), \
             patch.object(acceptance.platform, 'system', return_value='Linux'), \
             patch.object(acceptance.platform, 'machine', return_value='x86_64'), \
             patch.object(acceptance, 'docker_command', side_effect=docker.command), \
             patch.object(acceptance, 'run_isolated', side_effect=lambda *args, **kwargs:
                          probe(docker, *args, **kwargs) if probe else docker.probe(*args, **kwargs)):
            result = acceptance.image_acceptance(fixture.releases, fixture.directories, root / 'images.json')
        return result, json.loads((root / 'images.json').read_text()), docker

    def test_success_cleans_unique_names_and_records_native_and_qemu_scope(self):
        with tempfile.TemporaryDirectory() as temporary:
            result, report, docker = self.check(Path(temporary))
            self.assertTrue(result)
            self.assertEqual(docker.active, {})
            self.assertEqual(len(docker.created), len(set(docker.created)))
            self.assertEqual(report['runnerPlatform'], 'linux/amd64')
            self.assertEqual(len(report['checks']), 4)
            for row in report['checks']:
                self.assertEqual(row['executionMode'], 'native' if row['platform'] == 'linux/amd64' else 'qemu-binfmt-emulated')
                self.assertFalse(row['nativeHardwareQualification'])
                self.assertEqual(row['cleanup']['status'], 'passed')
                self.assertEqual(row['cleanup']['errors'], [])
            self.assertTrue(any(name.endswith('-server') for name in docker.removed))
            self.assertTrue(all(not path.exists() for path in docker.mounted))
            self.assertEqual(report['harnesses'], {name: acceptance.STAGE.sha256(acceptance.ROOT / name)
                                                  for name in acceptance.IMAGE_PROBE_HARNESS_PATHS})
            for command, env, _, cwd in docker.probe_calls:
                self.assertEqual(cwd, acceptance.ROOT)
                self.assertEqual(command[:4], ['go', 'run', '-mod=readonly', str(acceptance.ROOT / 'buildscripts/verify-s3-startup.go')])
                self.assertEqual(env['GOWORK'], 'off')
                self.assertEqual(env['GOENV'], 'off')
                self.assertNotIn('GOFLAGS', env)

    def test_zero_exit_partner_stub_cannot_override_trusted_probe_failure(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            candidate = root / 'candidate'
            (candidate / 'buildscripts').mkdir(parents=True)
            (candidate / 'buildscripts/verify-s3-startup.go').write_text('package main\nfunc main() {}\n')
            selected = []
            def probe(docker, command, env, timeout, cwd):
                selected.append((command, cwd))
                if Path(cwd) == candidate:
                    return 0, 'candidate stub skips all S3 checks'
                docker.probe(command, env, timeout, cwd)
                return 7, 'trusted S3 checks failed'
            original_directory = Path.cwd()
            try:
                os.chdir(candidate)
                result, report, docker = self.check(root, probe=probe)
            finally:
                os.chdir(original_directory)
            self.assertFalse(result)
            self.assertTrue(selected)
            self.assertTrue(all(cwd == acceptance.ROOT for _, cwd in selected))
            self.assertTrue(all(str(candidate) not in item for command, _ in selected for item in command))
            server = next(row for row in report['checks'] if row['repository'] == 'soulteary/otterio')
            self.assertFalse(server['runtimePassed'])
            self.assertEqual(docker.active, {})

    def test_changed_trusted_probe_dependency_cannot_mint_runtime_success(self):
        with tempfile.TemporaryDirectory() as temporary:
            root = Path(temporary)
            trusted = root / 'trusted'
            for name in acceptance.IMAGE_PROBE_HARNESS_PATHS:
                target = trusted / name
                target.parent.mkdir(parents=True, exist_ok=True)
                shutil.copyfile(acceptance.ROOT / name, target)
            def probe(docker, command, env, timeout, cwd):
                result = docker.probe(command, env, timeout, cwd)
                (trusted / 'go.mod').write_text('module substituted\n')
                return result
            with patch.object(acceptance, 'ROOT', trusted):
                result, report, docker = self.check(root, probe=probe)
            self.assertFalse(result)
            self.assertIn('dependencies changed', report['reason'])
            server = next(row for row in report['checks'] if row['repository'] == 'soulteary/otterio')
            self.assertFalse(server['runtimePassed'])
            self.assertEqual(docker.active, {})

    def test_partial_create_and_attached_run_failure_are_cleaned(self):
        for failure in ('create', 'run', 'bytes'):
            with self.subTest(failure=failure), tempfile.TemporaryDirectory() as temporary:
                result, report, docker = self.check(Path(temporary), failure)
                self.assertFalse(result)
                self.assertEqual(docker.active, {})
                self.assertEqual(report['checks'][0]['cleanup']['status'], 'passed')

    def test_server_failure_timeout_and_interruption_clean_daemon_before_temp_data(self):
        for failure, code in ((None, 7), (None, 124), ('interrupt', 0)):
            with self.subTest(failure=failure, code=code), tempfile.TemporaryDirectory() as temporary:
                result, report, docker = self.check(Path(temporary), failure, code)
                self.assertFalse(result)
                self.assertEqual(docker.active, {})
                server = next(row for row in report['checks'] if row['repository'] == 'soulteary/otterio')
                self.assertEqual(server['cleanup']['status'], 'passed')
                self.assertTrue(any(name.endswith('-server') for name in docker.removed))
                self.assertTrue(all(not path.exists() for path in docker.mounted))
                if failure == 'interrupt': self.assertEqual(report['errorClass'], 'KeyboardInterrupt')

    def test_cleanup_query_error_cannot_be_reported_as_absence_or_success(self):
        with tempfile.TemporaryDirectory() as temporary:
            result, report, docker = self.check(Path(temporary), 'cleanup')
            self.assertFalse(result)
            self.assertFalse(report['checks'][0]['passed'])
            self.assertEqual(report['checks'][0]['cleanup']['status'], 'failed')
            self.assertTrue(report['checks'][0]['cleanup']['errors'])
            self.assertTrue(docker.active)


if __name__ == '__main__':
    unittest.main()
