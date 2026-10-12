"""Publish only unchanged official draft bytes accepted by the trusted workflow."""
import base64
import copy
import hashlib
import importlib.util
import io
import json
from pathlib import Path
import shutil
import stat
import tarfile
import tempfile
from types import SimpleNamespace
import unittest
from unittest.mock import patch
import warnings
import zipfile


SCRIPT = Path(__file__).with_name('release-stage.py')
SPEC = importlib.util.spec_from_file_location('release_stage', SCRIPT)
stage = importlib.util.module_from_spec(SPEC)
SPEC.loader.exec_module(stage)
ROOT = SCRIPT.parents[1]
OC, OT = stage.REPOSITORIES
TAGS = {OC: 'RELEASE.2026-10-13T00-00-01Z', OT: 'RELEASE.2026-10-13T00-00-02Z'}
SOURCES = {OC: 'a' * 40, OT: 'b' * 40}
SDK = 'v0.0.0-20261009091443-bcc238bc4c2c'
RUN_ID = 321
RUN_HEAD = 'e' * 40
OLD_OC_SOURCE = 'ee1846d587a6548db4e595d653ca4f54baea2b1a'
OLD_OT_SOURCE = '8cc518b31ac8b69c8f855e19b4bd027f44275c47'
OLD_MANIFEST = '2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3'


def hashed(data):
    return hashlib.sha256(data).hexdigest()


def write_json(path, value):
    path.write_text(json.dumps(value, indent=2) + '\n')


class OfficialFixture:
    """Fake official APIs and actual small package payloads; no external effects."""
    def __init__(self, root):
        self.root = Path(root)
        self.events = []
        self.edits = []
        self.api_changes = {}
        self.tag_changes = {}
        self.image_changes = {}
        self.snapshot_reads = {OC: 0, OT: 0}
        self.after_image = None
        self.package_bytes = {'oc': b'packaged OC CLI bytes', 'console': b'packaged OC Console bytes',
                              'server': b'packaged OtterIO server bytes'}
        self.hashes = {name: hashed(data) for name, data in self.package_bytes.items()}
        self.directories = {}
        self.release_states = {}
        self.releases = {}
        for repository in stage.REPOSITORIES:
            directory = self.root / repository.split('/')[1]
            directory.mkdir()
            self.directories[repository] = directory
            self.make_candidate(directory, repository)
            state = {'id': 101 if repository == OC else 202, 'tag_name': TAGS[repository],
                     'draft': True, 'prerelease': False, 'body': 'Reviewed English release announcement.'}
            self.release_states[repository] = state
            self.refresh_official(repository)
            row = stage.inventory(directory, repository, TAGS[repository])
            row.update(releaseID=state['id'], releaseNotesSHA256=hashed(state['body'].encode()))
            self.releases[repository] = row
        self.reports = self.root / 'reports'
        self.reports.mkdir()
        self.make_reports()
        self.run = {'status': 'completed', 'conclusion': 'success', 'event': 'workflow_dispatch',
                    'head_branch': 'main', 'path': stage.ACCEPTANCE_WORKFLOW,
                    'repository': {'full_name': OC}, 'run_attempt': 2, 'head_sha': RUN_HEAD}
        self.attestation = {'schemaVersion': 1, 'evidenceKind': 'draft-release-pair',
                            'passed': True, 'publishable': True, 'releases': copy.deepcopy(self.releases),
                            'packagedPrograms': dict(self.hashes),
                            'previousPrograms': {key: copy.deepcopy(self.upgrade['programs'][key])
                                                 for key in ('previousServer', 'previousCLI', 'previousConsole')},
                            'provenance': {'repository': OC, 'workflowPath': stage.ACCEPTANCE_WORKFLOW,
                                           'runID': RUN_ID, 'runAttempt': 2, 'headSHA': RUN_HEAD,
                                           'event': 'workflow_dispatch', 'ref': 'refs/heads/main'}}
        self.refresh_evidence()

    def make_candidate(self, directory, repository):
        tag = TAGS[repository]
        if repository == OC:
            assets = []
            for target in stage.OC_TARGETS:
                suffix = 'zip' if target.startswith('windows') else 'tar.gz'
                path = directory / f'oc-{tag}-{target}.{suffix}'
                stem = f'oc-{tag}-{target}'
                payloads = {('oc.exe' if suffix == 'zip' else 'oc'): self.package_bytes['oc'],
                            ('oc-console.exe' if suffix == 'zip' else 'oc-console'): self.package_bytes['console'],
                            'LICENSE': b'license', 'NOTICE': b'notice', 'CREDITS': b'credits',
                            'licenses/notify-LICENSE': b'notify license', 'README.md': b'readme',
                            'README_zh_CN.md': b'readme', 'docs/compatibility.json': b'{}'}
                if suffix == 'zip':
                    with zipfile.ZipFile(path, 'w') as archive:
                        for name, data in payloads.items(): archive.writestr(stem + '/' + name, data)
                else:
                    with tarfile.open(path, 'w:gz') as archive:
                        for name, data in payloads.items():
                            member = tarfile.TarInfo(stem + '/' + name)
                            member.mode, member.size = 0o755, len(data)
                            archive.addfile(member, io.BytesIO(data))
                assets.append({'name': path.name, 'target': target.replace('-', '/', 1), 'sha256': stage.sha256(path)})
            manifest = {'schema_version': 1, 'release_tag': tag, 'source_commit': SOURCES[OC],
                        'go_toolchain': 'go1.27.2', 'otterio_sdk': SDK, 'assets': assets}
        else:
            for target in stage.OT_TARGETS:
                name = 'otterio-' + target + ('.exe' if target.startswith('windows') else '')
                (directory / name).write_bytes(self.package_bytes['server'])
            manifest = {'schema_version': 1, 'tag': tag, 'source_sha': SOURCES[OT]}
        manifest['images'] = [{'repository': 'ghcr.io/' + repository,
                               'digest': 'sha256:' + ('c' if repository == OC else 'd') * 64}]
        write_json(directory / 'release-manifest.json', manifest)
        self.refresh_checksums(repository)

    def refresh_checksums(self, repository):
        directory, tag = self.directories[repository], TAGS[repository]
        name = 'checksums.txt' if repository == OC else f'otterio-{tag}-checksums.txt'
        (directory / name).write_text(''.join(f'{stage.sha256(path)}  {path.name}\n'
                                             for path in sorted(directory.iterdir()) if path.name != name))

    def refresh_official(self, repository):
        self.release_states[repository]['assets'] = [
            {'id': index + 1000, 'name': path.name, 'state': 'uploaded', 'digest': 'sha256:' + stage.sha256(path)}
            for index, path in enumerate(sorted(self.directories[repository].iterdir()))]

    def cleanup(self):
        return {'status': 'passed', 'errors': [], 'processes': [{'component': 'server', 'stopped': True}]}

    def identities(self):
        return {key: {'source': SOURCES[OT if key == 'server' else OC], 'binarySHA256': value,
                      'binaryVCSRevision': SOURCES[OT if key == 'server' else OC], 'binaryVCSModified': False,
                      'sourceKind': 'unpatched-git-build', 'platform': 'linux/amd64', **({'sdkPin': SDK} if key != 'server' else {})}
                for key, value in self.hashes.items()}

    def integration(self, profile):
        scope = {'storage': 'read-only-and-writes', 'settings': 'read-only-and-protected-settings',
                 'features-version-copy': 'console-five-features-and-version-copy',
                 'native': 'native-iam-https-read-only', 'relationship-iam': 'relationship-iam-native'}[profile]
        report = {'status': 'passed', 'scope': scope, 'serverSource': SOURCES[OT], 'sdkPin': SDK,
                  'binaries': {name: {'sha256': self.hashes[key]} for name, key in
                               (('oc', 'oc'), ('oc-console', 'console'), ('otterio', 'server'))},
                  'identityEvidence': {'sdkPin': 'verified-client-build-info', 'serverSource': 'verified-vcs-revision'},
                  'storage': 'single-node-four-disk-erasure', 'settingsProfile': 'v1-protected-settings', 'scenarios': []}
        names = ['single-tls', 'dual-tls'] if profile in ('native', 'relationship-iam') else ['single-http', 'dual-http', 'single-tls', 'dual-tls']
        for name in names:
            row = {'scenario': name, 'status': 'passed', 'checks': ['meaningful fixture check'] * 10, 'processCleanup': self.cleanup()}
            if profile == 'storage': row['writeMetrics'] = {'fixtureBytes': 1}
            elif profile == 'settings': row['settingsMetrics'] = {'profile': 'v1-protected-settings'}
            elif profile == 'features-version-copy':
                row['featureMetrics'] = {'fixtureBytes': 1}
                row['versionCopyMetrics'] = {'profile': 'copy-source-version-authorization', 'confirmedCopies': 1, 'accessDeniedWithoutDestination': 1}
            elif profile == 'native':
                row['nativeMetrics'] = {'iamCreation': 'native user/group/service-account; permission and duplicate-target checks verified',
                                        'browserTLS': 'verified', 'storageTLS': 'verified', 'rootLogin': 'denied'}
            else:
                row['relationshipIAMMetrics'] = {'capabilityMode': 'required-supported', 'processCleanup': self.cleanup(),
                    'positiveMutationCoverage': ['user.lifecycle-parent-service-sts-cascade', 'user.lifecycle-self',
                                                'user.rotate-other', 'user.rotate-self', 'disabled-user.rotate', 'user.policies',
                                                'group.policies', 'group.status-members-delete', 'service-account.status-secret-policy-delete']}
            report['scenarios'].append(row)
        return report

    def protocol(self):
        return {'schemaVersion': 1, 'passed': True, 'server': {'source': SOURCES[OT], 'kind': 'module-build',
                'sourceState': 'clean', 'binarySHA256': self.hashes['server']},
                'clientBinarySHA256': {'cli': self.hashes['oc'], 'console': self.hashes['console']},
                'checks': [{'name': 'range', 'status': 'passed', 'exitCode': 0, 'profiles': [
                    {'profile': name, 'checks': 9, 'fixtureBytes': 1024, 'sha256': '7' * 64, 'emptyObjectChecks': 2,
                     'nullVersionStatus': 206, 'historicalVersionStatus': 206, 'restrictedCurrentStatus': 206,
                     'restrictedHistoricalStatus': 403, 'restrictedConditionalFullStatus': 200, 'restrictedIfRangeFallbackStatus': 200}
                    for name in ('single-http', 'dual-http', 'dual-tls')]},
                    {'name': 'diagnostics', 'status': 'passed', 'exitCode': 0, 'reason': 'named-test-ran-and-passed', 'test': 'TestDiagnosticsRealServer'},
                    {'name': 'oidc-sts', 'status': 'passed', 'exitCode': 0, 'reason': 'named-test-ran-and-passed', 'test': 'TestOtterIOSTSIntegration'}]}

    def oidc(self, dual):
        checks = ['different_sites', 'alice_binding_cookie_flags', 'bob_binding_cookie_flags',
                  'alice_cross_site_top_level_callback_binding', 'bob_cross_site_top_level_callback_binding',
                  'alice_ui_session', 'bob_ui_session', 'alice_ui_logout', 'bob_logout_isolation',
                  'bob_session_after_initial_token_expiry', 'fixed_deadline_401']
        return {'status': 'passed', 'cleanup': 'passed', 'programs': {
                    key: {'declaredSource': row['source'], 'binaryVCSRevision': row['source'], 'binaryVCSModified': False,
                          'sha256': row['binarySHA256']} for key, row in self.identities().items()},
                'storageTopology': 'dual HTTPS S3/Admin listeners' if dual else 'single HTTPS S3/Admin listener',
                'browserSiteTopology': 'localhost IdP and 127.0.0.1 Console (different sites)',
                'certificateIdentities': {'primaryCAsha256': '1' * 64, 'adminCAsha256': ('2' if dual else '1') * 64},
                'checks': ['protocol check'] * 12 + ['Bob saved pre-expiry cookie is rejected after fixed TTL despite successful refresh']
                          + (['independent Admin HTTPS CA accepted; S3 primary CA is rejected for Admin listener'] if dual else []),
                'consoleRefreshEvidence': {'waitSeconds': 95, 'idTokenSeconds': 90, 'keycloakEventsBefore': 1, 'keycloakEventsAfter': 2},
                'browserAcceptance': {'status': 'passed', 'persistentProfile': False, 'successfulRefreshEventsDelta': 1,
                                      'browser': 'Chromium fixture', 'checks': [{'name': name, 'status': 'passed'} for name in checks]},
                'consoleAcceptance': 'passed: actual fixture assertions', 'consoleFixedExpiryReference': 'callback plus fixed TTL',
                'keycloak': {'image': 'quay.io/keycloak/keycloak@sha256:' + '3' * 64}}

    def make_reports(self):
        self.pair = {'schemaVersion': 1, 'passed': True, 'requiredProfiles': list(stage.PROFILES),
                     'programs': self.identities(), 'harnesses': {name: stage.sha256(ROOT / name) for name in ('buildscripts/test-console-migration-pair.py', 'buildscripts/console_pair_evidence.py')},
                     'checks': []}
        for profile in stage.PROFILES:
            child = self.protocol() if profile == 'real-protocol' else self.oidc(profile.endswith('-dual')) if profile.startswith('oidc-browser-') else self.integration(profile)
            name = profile + '.json'
            write_json(self.reports / name, child)
            row = {'name': profile, 'status': 'passed', 'exitCode': 0, 'report': name, 'reportSHA256': stage.sha256(self.reports / name)}
            if profile == 'real-protocol':
                write_json(self.reports / 'range.json', {'serverSource': SOURCES[OT], 'profiles': child['checks'][0]['profiles']})
                row['additionalReports'] = [{'report': 'range.json', 'reportSHA256': stage.sha256(self.reports / 'range.json')}]
            self.pair['checks'].append(row)
        old = {key: {'source': source, 'binarySHA256': hashed(key.encode()), 'kind': 'release-asset',
                     'embeddedVCS': source, 'embeddedModified': 'false', 'goVersion': 'go1.27.2', 'goos': 'linux', 'goarch': 'amd64'}
               for key, source in (('previousServer', OLD_OT_SOURCE), ('previousCLI', OLD_OC_SOURCE), ('previousConsole', OLD_OC_SOURCE))}
        self.upgrade = {'passed': True, 'profiles': [{'profile': name, 'status': 'passed', 'cleanup': 'passed',
                          'checks': [{'name': check, 'status': 'passed'} for check in stage.UPGRADE_CHECKS]}
                         for name in ('single-tls', 'dual-tls')],
                        'programs': {**old, **{name: {'source': SOURCES[OT if key == 'server' else OC], 'binarySHA256': self.hashes[key],
                                                   'kind': 'candidate-source-build', 'embeddedVCS': SOURCES[OT if key == 'server' else OC],
                                                   'embeddedModified': 'false', 'goVersion': 'go1.27.2', 'goos': 'linux', 'goarch': 'amd64'}
                                               for name, key in (('cli', 'oc'), ('console', 'console'), ('server', 'server'))}},
                        'previousReleaseManifests': {'server': {'tag': 'RELEASE.2026-10-10T16-38-05Z', 'source': OLD_OT_SOURCE,
                            'manifestSHA256': OLD_MANIFEST, 'expectedManifestSHA256': OLD_MANIFEST, 'verification': 'matches-expected-sha256'},
                            'oc': {'tag': 'RELEASE.2026-10-10T11-42-18Z', 'source': OLD_OC_SOURCE, 'manifestSHA256': '4' * 64}}}
        self.images = {'passed': True, 'mode': 'registry-digest', 'checks': [
            {'repository': repository, 'platform': arch, 'passed': True, 'binaryBytesMatched': True, 'runtimePassed': True,
             'cleanup': {'status': 'passed', 'errors': []},
             'digest': self.releases[repository]['images'][0]['digest']}
            for repository in stage.REPOSITORIES for arch in ('linux/amd64', 'linux/arm64')]}

    def refresh_evidence(self):
        for name, value in (('pair.json', self.pair), ('upgrade.json', self.upgrade), ('images.json', self.images)):
            write_json(self.reports / name, value)
        self.attestation['reportFiles'] = {path.name: stage.sha256(path) for path in self.reports.glob('*.json') if path.name != 'acceptance.json'}
        write_json(self.reports / 'acceptance.json', self.attestation)
        self.expected = stage.sha256(self.reports / 'acceptance.json')
        stream = io.BytesIO()
        with zipfile.ZipFile(stream, 'w') as archive:
            for path in sorted(self.reports.glob('*.json')): archive.writestr(path.name, path.read_bytes())
        self.archive = stream.getvalue()
        self.artifact = {'id': 543, 'name': 'console-release-acceptance', 'expired': False,
                         'digest': 'sha256:' + hashed(self.archive)}

    def mutate_report(self, name, mutation):
        path = self.reports / name
        value = json.loads(path.read_text())
        mutation(value)
        write_json(path, value)
        for row in self.pair['checks']:
            if row['report'] == name: row['reportSHA256'] = stage.sha256(path)
            for extra in row.get('additionalReports', []):
                if extra['report'] == name: extra['reportSHA256'] = stage.sha256(path)
        self.refresh_evidence()

    def api(self, path):
        self.events.append(('api', path))
        if path in self.api_changes: return copy.deepcopy(self.api_changes[path])
        if path == f'repos/{OC}/actions/runs/{RUN_ID}': return copy.deepcopy(self.run)
        if path == f'repos/{OC}/actions/runs/{RUN_ID}/artifacts?per_page=100': return {'artifacts': [copy.deepcopy(self.artifact)]}
        for repository in stage.REPOSITORIES:
            tag = TAGS[repository]
            if path == f'repos/{repository}/releases/tags/{tag}':
                self.snapshot_reads[repository] += 1
                return copy.deepcopy(self.release_states[repository])
            if path == f'repos/{repository}/git/ref/tags/{tag}':
                return {'object': {'type': 'tag', 'sha': ('5' if repository == OC else '6') * 40}}
            if path == f'repos/{repository}/git/tags/{("5" if repository == OC else "6") * 40}':
                return {'object': {'type': 'commit', 'sha': self.tag_changes.get(repository, SOURCES[repository])}}
        prefix = f'repos/{OC}/contents/'
        if path.startswith(prefix):
            filename, _, _ = path[len(prefix):].partition('?')
            data = (ROOT / filename).read_bytes()
            return {'encoding': 'base64', 'content': base64.b64encode(data).decode(), 'sha': hashed(data)}
        raise AssertionError('Unexpected API read: ' + path)

    def command(self, args, binary=False, write=False):
        self.events.append(('command', tuple(args)))
        assert write == (args[:3] == ['gh', 'release', 'edit']), 'Only the final edit may use the repository write token'
        if args[:3] == ['gh', 'api', f'repos/{OC}/actions/artifacts/543/zip']:
            self.assert_binary(binary)
            return self.archive
        if args[:3] == ['gh', 'release', 'download']:
            repository = args[args.index('--repo') + 1]
            assert args[3] == TAGS[repository]
            directory = Path(args[args.index('--dir') + 1])
            for path in self.directories[repository].iterdir(): shutil.copyfile(path, directory / path.name)
            return ''
        if args[:4] == ['docker', 'buildx', 'imagetools', 'inspect']:
            reference = args[4]
            repository = next(repo for repo in stage.REPOSITORIES if reference == 'ghcr.io/' + repo + ':' + TAGS[repo])
            result = json.dumps({'digest': self.image_changes.get(repository, self.releases[repository]['images'][0]['digest'])})
            if self.after_image: self.after_image(repository)
            return result
        if args[:3] == ['gh', 'release', 'edit']:
            self.edits.append(tuple(args))
            return ''
        raise AssertionError('Unexpected external command: ' + repr(args))

    def assert_binary(self, binary):
        assert binary, 'Artifact ZIP must be downloaded as bytes'

    def publish(self, repository=OC):
        with patch.object(stage, 'api', side_effect=self.api), patch.object(stage, 'command', side_effect=self.command), \
             patch.dict(stage.os.environ, {'GITHUB_REF': 'refs/heads/main', 'GITHUB_REPOSITORY': repository}):
            stage.publish(repository, TAGS[repository], RUN_ID, self.expected)


class ReleaseStageTests(unittest.TestCase):
    def setUp(self):
        self.temporary = tempfile.TemporaryDirectory()
        self.addCleanup(self.temporary.cleanup)
        self.fixture = OfficialFixture(self.temporary.name)

    def reject(self, repository=OC):
        with self.assertRaises((ValueError, KeyError, TypeError, OSError)):
            self.fixture.publish(repository)
        self.assertEqual(self.fixture.edits, [], 'No release mutation is permitted after any failed verification')

    def test_official_accepted_oc_draft_is_published_once_after_all_reads(self):
        self.fixture.publish()
        self.assertEqual(self.fixture.edits, [('gh', 'release', 'edit', TAGS[OC], '--repo', OC, '--draft=false', '--latest=false')])
        self.assertEqual(self.fixture.events[-1], ('command', self.fixture.edits[0]))
        self.assertGreaterEqual(self.fixture.snapshot_reads[OC], 2)
        self.assertGreaterEqual(self.fixture.snapshot_reads[OT], 2)

    def test_otterio_publication_requires_the_exact_accepted_oc_stable_first(self):
        self.reject(OT)
        self.fixture.release_states[OC]['draft'] = False
        self.fixture.publish(OT)
        self.assertEqual(len(self.fixture.edits), 1)
        self.assertEqual(self.fixture.edits[0][3], TAGS[OT])
        self.assertGreaterEqual(self.fixture.snapshot_reads[OC], 2)

    def test_tampered_uploaded_asset_blocks_publication(self):
        name = 'otterio-linux-amd64'
        (self.fixture.directories[OT] / name).write_bytes(b'tampered server')
        self.reject()

    def test_moved_current_or_partner_tag_blocks_oc_publication(self):
        for repository in stage.REPOSITORIES:
            with self.subTest(repository=repository):
                self.fixture.tag_changes[repository] = 'f' * 40
                self.reject()
                self.fixture.tag_changes.clear()

    def test_notes_or_assets_cannot_drift_during_final_image_checks(self):
        for target in stage.REPOSITORIES:
            with self.subTest(target=target):
                original = self.fixture.release_states[target]['body']
                self.fixture.after_image = lambda repo: self.fixture.release_states[target].update(body='Unreviewed replacement announcement.') if repo == OT else None
                self.reject()
                self.fixture.release_states[target]['body'] = original

    def test_current_and_partner_notes_changed_since_acceptance_are_rejected(self):
        for repository in stage.REPOSITORIES:
            with self.subTest(repository=repository):
                old = self.fixture.release_states[repository]['body']
                self.fixture.release_states[repository]['body'] = 'Changed after acceptance'
                self.reject()
                self.fixture.release_states[repository]['body'] = old

    def test_wrong_official_asset_metadata_digest_is_rejected(self):
        self.fixture.release_states[OC]['assets'][0]['digest'] = 'sha256:' + '0' * 64
        self.reject()

    def test_only_successful_official_main_dispatch_acceptance_is_trusted(self):
        for field, value in (('status', 'in_progress'), ('conclusion', 'failure'), ('event', 'pull_request'),
                             ('head_branch', 'untrusted'), ('path', '.github/workflows/arbitrary.yml'),
                             ('repository', {'full_name': 'attacker/oc'})):
            with self.subTest(field=field):
                original = copy.deepcopy(self.fixture.run[field])
                self.fixture.run[field] = value
                self.reject()
                self.fixture.run[field] = original

    def test_attestation_provenance_must_match_run_attempt_head_and_main_ref(self):
        for field, value in (('runID', 999), ('runAttempt', 1), ('headSHA', 'f' * 40), ('ref', 'refs/tags/release')):
            with self.subTest(field=field):
                original = self.fixture.attestation['provenance'][field]
                self.fixture.attestation['provenance'][field] = value
                self.fixture.refresh_evidence()
                self.reject()
                self.fixture.attestation['provenance'][field] = original
                self.fixture.refresh_evidence()

    def test_official_artifact_archive_and_attestation_bytes_are_both_hash_bound(self):
        self.fixture.artifact['digest'] = 'sha256:' + '0' * 64
        self.reject()
        self.fixture.artifact['digest'] = 'sha256:' + hashed(self.fixture.archive)
        self.fixture.expected = '0' * 64
        self.reject()

    def test_expired_or_duplicate_official_acceptance_artifact_is_rejected(self):
        self.fixture.artifact['expired'] = True
        self.reject()
        self.fixture.artifact['expired'] = False
        path = f'repos/{OC}/actions/runs/{RUN_ID}/artifacts?per_page=100'
        self.fixture.api_changes[path] = {'artifacts': [self.fixture.artifact, self.fixture.artifact]}
        self.reject()

    def test_missing_or_skipped_required_parent_profile_is_rejected(self):
        original = copy.deepcopy(self.fixture.pair)
        for mutation in (lambda doc: doc['checks'].pop(), lambda doc: doc['checks'][0].update(status='skipped'),
                         lambda doc: doc['checks'][0].update(exitCode=1)):
            mutation(self.fixture.pair)
            self.fixture.refresh_evidence()
            self.reject()
            self.fixture.pair = copy.deepcopy(original)
            self.fixture.refresh_evidence()

    def test_child_failure_or_missing_scenario_cannot_hide_behind_passed_parent(self):
        for mutation in (lambda doc: doc['scenarios'].pop(), lambda doc: doc['scenarios'][0].update(status='skipped'),
                         lambda doc: doc['scenarios'][0]['processCleanup'].update(status='failed')):
            self.fixture.mutate_report('storage.json', mutation)
            self.reject()
            write_json(self.fixture.reports / 'storage.json', self.fixture.integration('storage'))
            self.fixture.mutate_report('storage.json', lambda doc: None)

    def test_browser_skip_and_fake_refresh_are_rejected_even_when_hashes_are_consistent(self):
        self.fixture.mutate_report('oidc-browser-single.json', lambda doc: doc['browserAcceptance']['checks'][0].update(status='skipped'))
        self.reject()

    def test_skipped_real_named_protocol_test_is_rejected(self):
        self.fixture.mutate_report('real-protocol.json', lambda doc: doc['checks'][1].update(reason='required-test-absent-or-skipped'))
        self.reject()

    def test_negative_partial_or_wrong_source_additional_range_report_is_rejected(self):
        original = json.loads((self.fixture.reports / 'range.json').read_text())
        for mutation in (lambda doc: doc['profiles'].pop(), lambda doc: doc.update(serverSource='f' * 40),
                         lambda doc: doc['profiles'][0].update(restrictedHistoricalStatus=200)):
            self.fixture.mutate_report('range.json', mutation)
            self.reject()
            write_json(self.fixture.reports / 'range.json', original)
            self.fixture.mutate_report('range.json', lambda doc: None)

    def test_consistently_negative_range_raw_and_summary_cannot_pass(self):
        self.fixture.mutate_report('range.json', lambda doc: doc['profiles'][0].update(restrictedIfRangeFallbackStatus=403))
        self.fixture.mutate_report('real-protocol.json', lambda doc: doc['checks'][0]['profiles'][0].update(restrictedIfRangeFallbackStatus=403))
        self.reject()

    def test_new_program_hash_must_match_pair_upgrade_and_packaged_bytes(self):
        self.fixture.upgrade['programs']['cli']['binarySHA256'] = '0' * 64
        self.fixture.refresh_evidence()
        self.reject()

    def test_consistently_forged_program_hashes_must_still_match_actual_package_bytes(self):
        forged = '0' * 64
        self.fixture.attestation['packagedPrograms'] = {key: forged for key in self.fixture.hashes}
        for row in self.fixture.pair['programs'].values(): row['binarySHA256'] = forged
        for key in ('cli', 'console', 'server'): self.fixture.upgrade['programs'][key]['binarySHA256'] = forged
        for profile in stage.PROFILES:
            path = self.fixture.reports / (profile + '.json')
            child = json.loads(path.read_text())
            if profile == 'real-protocol':
                child['server']['binarySHA256'] = forged
                child['clientBinarySHA256'] = {'cli': forged, 'console': forged}
            elif profile.startswith('oidc-browser-'):
                for row in child['programs'].values(): row['sha256'] = forged
            else:
                for row in child['binaries'].values(): row['sha256'] = forged
            write_json(path, child)
            next(row for row in self.fixture.pair['checks'] if row['name'] == profile)['reportSHA256'] = stage.sha256(path)
        self.fixture.refresh_evidence()
        self.reject()

    def test_pair_embedded_revision_cannot_disagree_with_accepted_source(self):
        self.fixture.pair['programs']['oc']['binaryVCSRevision'] = 'f' * 40
        self.fixture.refresh_evidence()
        self.reject()

    def test_upgrade_source_cannot_disagree_with_the_paired_artifact_hash(self):
        self.fixture.upgrade['programs']['cli']['source'] = 'f' * 40
        self.fixture.upgrade['programs']['cli']['embeddedVCS'] = 'f' * 40
        self.fixture.refresh_evidence()
        self.reject()

    def test_old_program_tuple_must_match_independently_recorded_previous_programs(self):
        self.fixture.upgrade['programs']['previousServer']['binarySHA256'] = '0' * 64
        self.fixture.refresh_evidence()
        self.reject()

    def test_previous_manifest_binding_and_known_baseline_source_are_required(self):
        self.fixture.upgrade['previousReleaseManifests']['server']['expectedManifestSHA256'] = '0' * 64
        self.fixture.refresh_evidence()
        self.reject()

    def test_partial_or_skipped_upgrade_checks_cannot_hide_behind_passed_profile(self):
        original = copy.deepcopy(self.fixture.upgrade)
        for mutation in (lambda doc: doc['profiles'][0]['checks'].pop(),
                         lambda doc: doc['profiles'][0]['checks'][0].update(status='skipped')):
            mutation(self.fixture.upgrade)
            self.fixture.refresh_evidence()
            self.reject()
            self.fixture.upgrade = copy.deepcopy(original)
            self.fixture.refresh_evidence()

    def test_upgrade_checks_require_the_exact_executed_names_and_order(self):
        original = copy.deepcopy(self.fixture.upgrade)
        for mutation in (
                lambda doc: doc['profiles'][0]['checks'][0].update(name='unrelated successful assertion'),
                lambda doc: doc['profiles'][0]['checks'].reverse(),
                lambda doc: doc['profiles'][0]['checks'][1].update(name=stage.UPGRADE_CHECKS[0])):
            mutation(self.fixture.upgrade)
            self.fixture.refresh_evidence()
            self.reject()
            self.fixture.upgrade = copy.deepcopy(original)
            self.fixture.refresh_evidence()

    def test_missing_image_architecture_or_unexecuted_runtime_is_rejected(self):
        original = copy.deepcopy(self.fixture.images)
        for mutation in (lambda doc: doc['checks'].pop(), lambda doc: doc['checks'][0].update(runtimePassed=False),
                         lambda doc: doc['checks'][0].update(binaryBytesMatched=False),
                         lambda doc: doc['checks'][0].update(platform='linux/unknown')):
            mutation(self.fixture.images)
            self.fixture.refresh_evidence()
            self.reject()
            self.fixture.images = copy.deepcopy(original)
            self.fixture.refresh_evidence()

    def test_image_cleanup_must_be_present_successful_and_error_free(self):
        original = copy.deepcopy(self.fixture.images)
        for mutation in (
                lambda doc: doc['checks'][0].pop('cleanup'),
                lambda doc: doc['checks'][0]['cleanup'].update(status='failed'),
                lambda doc: doc['checks'][0]['cleanup'].update(errors=['container removal failed'])):
            mutation(self.fixture.images)
            self.fixture.refresh_evidence()
            self.reject()
            self.fixture.images = copy.deepcopy(original)
            self.fixture.refresh_evidence()

    def test_version_image_digest_changed_after_acceptance_blocks_publication(self):
        self.fixture.image_changes[OT] = 'sha256:' + '0' * 64
        self.reject()

    def test_release_manifest_tag_source_and_schema_are_strict(self):
        directory = self.fixture.directories[OC]
        original = json.loads((directory / 'release-manifest.json').read_text())
        for field, value in (('schema_version', True), ('release_tag', TAGS[OT]), ('source_commit', 'short')):
            with self.subTest(field=field):
                write_json(directory / 'release-manifest.json', {**original, field: value})
                self.fixture.refresh_checksums(OC)
                with self.assertRaises(ValueError): stage.inventory(directory, OC, TAGS[OC])
        write_json(directory / 'release-manifest.json', original)

    def test_archive_manifest_target_must_match_its_real_filename(self):
        directory = self.fixture.directories[OC]
        manifest = json.loads((directory / 'release-manifest.json').read_text())
        manifest['assets'][0]['target'] = 'linux/unreviewed'
        write_json(directory / 'release-manifest.json', manifest)
        self.fixture.refresh_checksums(OC)
        with self.assertRaises(ValueError): stage.inventory(directory, OC, TAGS[OC])

    def test_acceptance_schema_rejects_boolean_version(self):
        self.fixture.attestation['schemaVersion'] = True
        self.fixture.refresh_evidence()
        self.reject()

    def test_pair_validator_must_match_trusted_acceptance_workflow_source(self):
        self.fixture.pair['harnesses']['buildscripts/test-console-migration-pair.py'] = '0' * 64
        self.fixture.refresh_evidence()
        self.reject()

    def test_downloaded_historical_python_is_fingerprinted_but_never_executed(self):
        filename = 'buildscripts/test-console-migration-pair.py'
        payload = b"raise AssertionError('downloaded Python must never execute with publication credentials')\n"
        self.fixture.api_changes[f'repos/{OC}/contents/{filename}?ref={RUN_HEAD}'] = {
            'encoding': 'base64', 'content': base64.b64encode(payload).decode()}
        self.fixture.pair['harnesses'][filename] = hashed(payload)
        self.fixture.refresh_evidence()
        self.fixture.publish()
        self.assertEqual(len(self.fixture.edits), 1)

    def test_official_schema_must_match_local_reviewed_code_without_execution(self):
        filename = 'buildscripts/console_pair_evidence.py'
        payload = b"raise AssertionError('unreviewed schema must never execute')\n"
        self.fixture.api_changes[f'repos/{OC}/contents/{filename}?ref={RUN_HEAD}'] = {
            'encoding': 'base64', 'content': base64.b64encode(payload).decode()}
        self.fixture.pair['harnesses'][filename] = hashed(payload)
        self.fixture.refresh_evidence()
        self.reject()

    def test_secondary_registry_must_share_the_exercised_digest(self):
        directory = self.fixture.directories[OC]
        manifest = json.loads((directory / 'release-manifest.json').read_text())
        manifest['images'].append({'repository': 'soulteary/oc', 'digest': manifest['images'][0]['digest']})
        write_json(directory / 'release-manifest.json', manifest)
        self.fixture.refresh_checksums(OC)
        accepted = stage.inventory(directory, OC, TAGS[OC])
        self.assertEqual(len(accepted['images']), 2)
        manifest['images'][1]['digest'] = 'sha256:' + 'f' * 64
        write_json(directory / 'release-manifest.json', manifest)
        self.fixture.refresh_checksums(OC)
        with self.assertRaisesRegex(ValueError, 'registry mirror'):
            stage.inventory(directory, OC, TAGS[OC])
        with self.assertRaisesRegex(ValueError, 'registry mirror'):
            stage.verify_images({'images': manifest['images']})
        self.fixture.attestation['releases'][OC]['images'] = manifest['images']
        self.fixture.refresh_evidence()
        self.reject()

    def test_checksum_inventory_requires_all_assets_once_and_no_extra_or_symlink(self):
        directory = self.fixture.directories[OC]
        path = directory / 'checksums.txt'
        original = path.read_text()
        for checksum in ('\n'.join(original.splitlines()[1:]) + '\n', original + original.splitlines()[0] + '\n',
                         original + '0' * 64 + '  ../outside\n'):
            path.write_text(checksum)
            with self.assertRaises(ValueError): stage.inventory(directory, OC, TAGS[OC])
        path.write_text(original)
        (directory / 'extra').write_text('unexpected')
        with self.assertRaises(ValueError): stage.inventory(directory, OC, TAGS[OC])
        (directory / 'extra').unlink()
        target = directory / 'release-manifest.json'
        saved = directory.parent / 'saved-manifest'
        shutil.copyfile(target, saved)
        target.unlink()
        target.symlink_to(saved)
        with self.assertRaises(ValueError): stage.inventory(directory, OC, TAGS[OC])

    def test_attestation_zip_rejects_duplicate_noncanonical_traversal_and_symlink_entries(self):
        cases = [('acceptance.json', 'acceptance.json'), ('acceptance.json', './acceptance.json'),
                 ('../acceptance.json',), ('/acceptance.json',), ('folder/acceptance.json',), ('..\\acceptance.json',)]
        for names in cases:
            with self.subTest(names=names), tempfile.TemporaryDirectory() as temporary:
                archive = Path(temporary) / 'input.zip'
                with warnings.catch_warnings():
                    warnings.simplefilter('ignore', UserWarning)
                    with zipfile.ZipFile(archive, 'w') as out:
                        for name in names: out.writestr(name, '{}')
                destination = Path(temporary) / 'out'; destination.mkdir()
                with self.assertRaises(ValueError): stage.extract_attestation(archive, destination)
        with tempfile.TemporaryDirectory() as temporary:
            archive = Path(temporary) / 'input.zip'
            with zipfile.ZipFile(archive, 'w') as out:
                row = zipfile.ZipInfo('acceptance.json'); row.external_attr = (stat.S_IFLNK | 0o777) << 16
                out.writestr(row, 'outside')
            destination = Path(temporary) / 'out'; destination.mkdir()
            with self.assertRaises(ValueError): stage.extract_attestation(archive, destination)

    def test_unrequested_raw_json_file_cannot_be_smuggled_into_official_artifact(self):
        self.fixture.refresh_evidence()
        stream = io.BytesIO()
        with zipfile.ZipFile(stream, 'w') as archive:
            with zipfile.ZipFile(io.BytesIO(self.fixture.archive)) as original:
                for name in original.namelist(): archive.writestr(name, original.read(name))
            archive.writestr('unbound.json', '{}')
        self.fixture.archive = stream.getvalue()
        self.fixture.artifact['digest'] = 'sha256:' + hashed(self.fixture.archive)
        self.reject()

    def test_non_main_or_wrong_repository_cannot_read_or_mutate_releases(self):
        for environment in ({'GITHUB_REF': 'refs/tags/RELEASE', 'GITHUB_REPOSITORY': OC},
                            {'GITHUB_REF': 'refs/heads/main', 'GITHUB_REPOSITORY': 'attacker/oc'}):
            with self.subTest(environment=environment), patch.dict(stage.os.environ, environment), \
                 patch.object(stage, 'api') as api, patch.object(stage, 'command') as command:
                with self.assertRaises(ValueError): stage.publish(OC, TAGS[OC], RUN_ID, self.fixture.expected)
                api.assert_not_called()
                command.assert_not_called()

    def test_cross_repository_reader_token_is_never_used_for_publication(self):
        result = SimpleNamespace(returncode=0, stdout='verified')
        with patch.dict(stage.os.environ, {'GH_TOKEN': 'repository-write-token', 'CONSOLE_RELEASE_TOKEN': 'cross-repository-read-token'}, clear=True), \
             patch.object(stage.subprocess, 'run', return_value=result) as run:
            stage.command(['gh', 'api', 'repos/soulteary/otterio/releases'])
            read_environment = run.call_args.kwargs['env']
            self.assertEqual(read_environment['GH_TOKEN'], 'cross-repository-read-token')
            self.assertNotIn('CONSOLE_RELEASE_TOKEN', read_environment)
            stage.command(['gh', 'release', 'edit', TAGS[OC], '--repo', OC, '--draft=false', '--latest=false'], write=True)
            write_environment = run.call_args.kwargs['env']
            self.assertEqual(write_environment['GH_TOKEN'], 'repository-write-token')
            self.assertNotIn('CONSOLE_RELEASE_TOKEN', write_environment)

    def test_reader_fallback_and_non_gh_commands_receive_no_github_credentials(self):
        result = SimpleNamespace(returncode=0, stdout='verified')
        with patch.dict(stage.os.environ, {'GH_TOKEN': 'repository-token'}, clear=True), \
             patch.object(stage.subprocess, 'run', return_value=result) as run:
            stage.command(['gh', 'api', 'repos/soulteary/oc/releases'])
            self.assertEqual(run.call_args.kwargs['env']['GH_TOKEN'], 'repository-token')
        with patch.dict(stage.os.environ, {'GH_TOKEN': 'repository-token', 'GITHUB_TOKEN': 'repository-action-token',
                                          'CONSOLE_RELEASE_TOKEN': 'reader-token', 'FIXTURE_SAFE': 'kept'}, clear=True), \
             patch.object(stage.subprocess, 'run', return_value=result) as run:
            stage.command(['docker', 'buildx', 'imagetools', 'inspect', 'immutable-image'])
            self.assertNotIn('GH_TOKEN', run.call_args.kwargs['env'])
            self.assertNotIn('GITHUB_TOKEN', run.call_args.kwargs['env'])
            self.assertNotIn('CONSOLE_RELEASE_TOKEN', run.call_args.kwargs['env'])
            self.assertEqual(run.call_args.kwargs['env']['FIXTURE_SAFE'], 'kept')


if __name__ == '__main__':
    unittest.main()
