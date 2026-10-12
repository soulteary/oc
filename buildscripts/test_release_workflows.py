"""Guard the draft, attested publication and manual promotion boundaries.

These checks use the repository's block mapping style and only the standard
library, so the release boundary suite also runs without a YAML dependency.
"""
from pathlib import Path
import re
import unittest


ROOT = Path(__file__).resolve().parents[1]


def section(text, key, indent=0):
    lines = text.splitlines()
    start = next((index for index, line in enumerate(lines)
                  if re.fullmatch(' ' * indent + re.escape(key) + r':\s*(?:#.*)?', line)), None)
    if start is None:
        return ''
    end = start + 1
    while end < len(lines):
        line = lines[end]
        if line.strip() and not line.lstrip().startswith('#') and len(line) - len(line.lstrip()) <= indent:
            break
        end += 1
    return '\n'.join(lines[start + 1:end])


def mapping_keys(text, indent):
    return re.findall(r'^' + ' ' * indent + r'([A-Za-z_][A-Za-z0-9_-]*):', text, re.MULTILINE)


def steps(text, job):
    body = section(section(section(text, 'jobs'), job, 2), 'steps', 4)
    return ['        ' + part for part in re.split(r'^      - ', body, flags=re.MULTILINE)[1:]]


def step_field(step, name):
    match = re.search(r'^        ' + re.escape(name) + r':\s*(.*)$', step, re.MULTILINE)
    return match.group(1).strip() if match else ''


def step_run(step):
    value = step_field(step, 'run')
    if value not in ('|', '>'):
        return value
    lines = step.splitlines()
    start = next(index for index, line in enumerate(lines) if re.match(r'^        run:', line))
    command = []
    for line in lines[start + 1:]:
        if line.strip() and len(line) - len(line.lstrip()) <= 8:
            break
        command.append(line[10:])
    return '\n'.join(command)


def acceptance_boundary_errors(text):
    errors = []
    if mapping_keys(section(text, 'on'), 2) != ['workflow_dispatch']:
        errors.append('acceptance must be manual only')
    inputs = section(section(section(text, 'on'), 'workflow_dispatch', 2), 'inputs', 4)
    if set(mapping_keys(inputs, 6)) != {'oc_tag', 'otterio_tag'}:
        errors.append('acceptance must require both candidate tags')
    for name in ('oc_tag', 'otterio_tag'):
        field = section(inputs, name, 6)
        if not re.search(r'^\s+required:\s*true$', field, re.MULTILINE) or not re.search(r'^\s+type:\s*string$', field, re.MULTILINE):
            errors.append('acceptance tag must be a required string: ' + name)
    permission = section(text, 'permissions')
    if set(mapping_keys(permission, 2)) != {'contents', 'actions', 'packages'} or any(
            not re.search(r'^  ' + name + r': read$', permission, re.MULTILINE)
            for name in ('contents', 'actions', 'packages')):
        errors.append('acceptance must retain read-only verification permissions')
    if re.search(r'\b(?:write|write-all)\b', '\n'.join(
            line for line in text.splitlines() if re.search(r'permissions:|\b(?:contents|actions|packages|id-token):', line))):
        errors.append('acceptance cannot gain write permissions')
    if set(mapping_keys(section(text, 'jobs'), 2)) != {'packaged-pair'}:
        errors.append('acceptance must have only its verification job')
    job_steps = steps(text, 'packaged-pair')
    gate = 'test "$GITHUB_REF" = refs/heads/main'
    if not job_steps or step_run(job_steps[0]) != gate:
        errors.append('acceptance must refuse non-main as its first step')
    protected = [step for step in job_steps if not step_field(step, 'uses').startswith('actions/upload-artifact@')]
    if any(step_field(step, 'if') or step_field(step, 'continue-on-error') not in ('', 'false') for step in protected):
        errors.append('acceptance verification steps cannot be skipped or suppress failures')
    checkouts = [(index, step) for index, step in enumerate(job_steps)
                 if step_field(step, 'uses').startswith('actions/checkout@')]
    if len(checkouts) != 2:
        errors.append('acceptance must check out its exact workflow source and validated Otterio source')
    else:
        (_, own), (ot_index, partner) = checkouts
        own_with, partner_with = section(own, 'with', 8), section(partner, 'with', 8)
        if ('          ref: ${{ github.sha }}' not in own_with or '          path: oc' not in own_with or
                '          persist-credentials: false' not in own_with or 'repository:' in own_with):
            errors.append('acceptance own source must be the exact github.sha without persisted credentials')
        if not all(value in partner_with for value in ('          repository: soulteary/otterio',
                    '          ref: ${{ inputs.otterio_tag }}', '          path: otterio-candidate',
                    '          persist-credentials: false')):
            errors.append('acceptance partner source must be the requested validated Otterio tag')
        validators = [(index, step_run(step)) for index, step in enumerate(job_steps)
                      if 'stage.tag_valid(' in step_run(step)]
        if len(validators) != 1 or not (checkouts[0][0] < validators[0][0] < ot_index) or not all(
                value in validators[0][1] for value in ("stage.tag_valid(os.environ['OC_TAG'])",
                    "stage.tag_valid(os.environ['OTTERIO_TAG'])", "'buildscripts/release-stage.py'")):
            errors.append('both tags must be validated by the trusted helper before partner checkout')
    commands = '\n'.join(step_run(step) for step in job_steps)
    if any(re.search(pattern, commands) for pattern in (
            r'gh\s+release\s+(?:edit|create|upload|delete)', r'release-stage\.py\s+publish',
            r'git\s+(?:tag|push)', r'build-push-action', r'imagetools\s+create',
            r'gh\s+api[^\n]*(?:--method|-X)\s+(?:POST|PATCH|PUT|DELETE)')):
        errors.append('acceptance may not publish or mutate release assets and aliases')
    wrappers = [(index, step_run(step)) for index, step in enumerate(job_steps)
                if 'python3 buildscripts/console_release_acceptance.py' in step_run(step)]
    if len(wrappers) != 1 or not all(value in wrappers[0][1] for value in (
            '--download-drafts --oc-tag "$OC_TAG" --otterio-tag "$OTTERIO_TAG"',
            '--previous-oc "$RUNNER_TEMP/release-previous/oc"',
            '--previous-otterio "$RUNNER_TEMP/release-previous/otterio"',
            '--otterio-source-dir "$GITHUB_WORKSPACE/otterio-candidate"',
            '--output "$RUNNER_TEMP/console-release-acceptance"')) or any(
            value in wrappers[0][1] for value in ('--oc-assets', '--otterio-assets', '|| true')):
        errors.append('acceptance must run the actual draft wrapper and retain its exit status')
    old = [step_run(step) for step in job_steps if 'gh release download RELEASE.2026-10-10T16-38-05Z' in step_run(step)]
    expected = '2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3'
    if len(old) != 1 or not all(value in old[0] for value in (
            'gh release download RELEASE.2026-10-10T16-38-05Z --repo soulteary/otterio',
            'gh release download RELEASE.2026-10-10T11-42-18Z --repo soulteary/oc',
            'gh api repos/soulteary/otterio/releases/tags/RELEASE.2026-10-10T16-38-05Z',
            'select(.name == "release-manifest.json") | .digest',
            'test "$digest" = sha256:' + expected)):
        errors.append('acceptance must pin both previous releases and independently verify the official manifest digest')
    uploads = [(index, step) for index, step in enumerate(job_steps)
               if step_field(step, 'uses').startswith('actions/upload-artifact@')]
    if len(uploads) != 1 or not wrappers or uploads[0][0] <= wrappers[0][0] or step_field(uploads[0][1], 'if') != 'always()' or not all(
            value in section(uploads[0][1], 'with', 8) for value in (
                '          name: console-release-acceptance',
                '          path: ${{ runner.temp }}/console-release-acceptance/*.json',
                '          if-no-files-found: error')):
        errors.append('acceptance must always preserve every original JSON report and fail if none exist')
    return errors


def boundary_errors(workflows):
    errors = []
    draft, publish, promote = [workflows[name] for name in ('release.yml', 'publish-release.yml', 'promote-release.yml')]
    if set(mapping_keys(section(draft, 'on'), 2)) != {'push', 'workflow_dispatch'}:
        errors.append('draft build must retain tag push and manual dispatch')
    if "tags: ['RELEASE.*']" not in section(section(draft, 'on'), 'push', 2):
        errors.append('draft build must restrict pushes to timestamp tags')
    for name, text in (('publication', publish), ('promotion', promote)):
        if mapping_keys(section(text, 'on'), 2) != ['workflow_dispatch']:
            errors.append(name + ' must be manual only')
    for name, text in workflows.items():
        if re.search(r'^\s*uses:\s*.*promote-release\.yml', text, re.MULTILINE):
            errors.append('automatic promotion caller: ' + name)
    for pattern in (r'gh\s+release\s+edit', r'release-stage\.py\s+publish', r'promote_latest',
                    r'--draft=false', r'(?<![A-Za-z]):latest\b'):
        if re.search(pattern, draft):
            errors.append('draft build may not publish or promote')
            break
    if not re.search(r'gh\s+release\s+create[^\n]*--draft\b[^\n]*--latest=false', draft):
        errors.append('draft creation must remain stable draft without latest')
    if '.draft == true and .[0].prerelease == false' not in draft:
        errors.append('existing upload target must be a stable draft')
    if 'gh release download "$RELEASE_TAG"' not in draft or 'for asset in "$RUNNER_TEMP/oc-release/"*; do cmp' not in draft:
        errors.append('draft bytes must be downloaded and verified')
    if set(mapping_keys(section(draft, 'jobs'), 2)) != {'release'}:
        errors.append('draft build may not have a second publication job')
    inputs = section(section(section(publish, 'on'), 'workflow_dispatch', 2), 'inputs', 4)
    if set(mapping_keys(inputs, 6)) != {'tag', 'acceptance_run_id', 'attestation_sha256'}:
        errors.append('publication must require the complete attestation interface')
    for name in ('tag', 'acceptance_run_id', 'attestation_sha256'):
        field = section(inputs, name, 6)
        if not re.search(r'^\s+required:\s*true$', field, re.MULTILINE) or not re.search(r'^\s+type:\s*string$', field, re.MULTILINE):
            errors.append('publication input must be a required string: ' + name)
    gate = 'run: test "$GITHUB_REF" = refs/heads/main'
    if gate not in publish or publish.index(gate) > publish.index('uses: actions/checkout@'):
        errors.append('publication must refuse non-main before checkout')
    if not re.search(r'uses: actions/checkout@[^\n]+\n\s+with:\n\s+ref: main\n', publish):
        errors.append('publication helper must come from main')
    command = ('python3 buildscripts/release-stage.py publish',
               '--repository "$GITHUB_REPOSITORY" --tag "$RELEASE_TAG"',
               '--acceptance-run-id "$ACCEPTANCE_RUN_ID"',
               '--attestation-sha256 "$ATTESTATION_SHA256"')
    if not all(part in publish for part in command):
        errors.append('publication must delegate all bound inputs to the attestation verifier')
    if set(mapping_keys(section(publish, 'jobs'), 2)) != {'publish'}:
        errors.append('publication must have only the attested publication job')
    for pattern in (r'go\s+build', r'build-release\.py', r'build-push-action', r'gh\s+release\s+(?:create|upload|edit)',
                    r'git\s+(?:tag|push)', r'imagetools\s+create', r'--latest\b', r'promote_latest'):
        if re.search(pattern, publish):
            errors.append('publication may not rebuild, upload or change latest')
            break
    publish_job = section(section(publish, 'jobs'), 'publish', 2)
    permission = section(publish_job, 'permissions', 4)
    for name, value in (('contents', 'write'), ('actions', 'read'), ('packages', 'read')):
        if not re.search(r'^\s+' + name + r':\s*' + value + '$', permission, re.MULTILINE):
            errors.append('publication permission mismatch: ' + name)
    if re.search(r'^\s+packages:\s*write$', publish, re.MULTILINE):
        errors.append('publication must not write registry packages')
    for name, text in (('draft', draft), ('publication', publish), ('promotion', promote)):
        if 'group: oc-stable-promotion' not in text or 'queue: max' not in text:
            errors.append(name + ' must share the publication and promotion lock')
    verify = 'Verify all immutable version tags before changing any aliases'
    aliases = 'Promote the recorded digests and then the GitHub latest release'
    if verify not in promote or aliases not in promote or promote.index(verify) > promote.index(aliases):
        errors.append('promotion must verify all version digests before aliases')
    errors.extend(acceptance_boundary_errors(workflows['console-release-acceptance.yml']))
    return errors


class ReleaseWorkflowBoundaryTests(unittest.TestCase):
    def setUp(self):
        self.workflows = {path.name: path.read_text() for path in (ROOT / '.github/workflows').glob('*.yml')}

    def reject_change(self, name, before, after, message):
        self.assertIn(before, self.workflows[name])
        self.workflows[name] = self.workflows[name].replace(before, after, 1)
        self.assertTrue(any(message in error for error in boundary_errors(self.workflows)), boundary_errors(self.workflows))

    def test_repository_keeps_three_distinct_release_boundaries(self):
        self.assertEqual(boundary_errors(self.workflows), [])

    def test_draft_cannot_automatically_become_public(self):
        original = self.workflows['release.yml']
        self.reject_change('release.yml', '--verify-tag --draft', '--verify-tag --draft=false', 'draft')
        self.workflows['release.yml'] = original
        self.workflows['release.yml'] += '\n          gh release edit "$RELEASE_TAG" --draft=false\n'
        self.assertIn('draft build may not publish or promote', boundary_errors(self.workflows))

    def test_draft_cannot_hide_a_second_publication_job(self):
        self.workflows['release.yml'] += '\n  publish:\n    runs-on: ubuntu-latest\n'
        self.assertIn('draft build may not have a second publication job', boundary_errors(self.workflows))

    def test_draft_requires_downloaded_bytes_before_acceptance(self):
        self.reject_change('release.yml', 'for asset in "$RUNNER_TEMP/oc-release/"*; do cmp',
                           'for asset in "$RUNNER_TEMP/oc-release/"*; do echo', 'downloaded and verified')

    def test_publication_and_promotion_reject_automatic_triggers(self):
        for name in ('publish-release.yml', 'promote-release.yml'):
            with self.subTest(name=name):
                original = self.workflows[name]
                self.reject_change(name, 'on:\n', 'on:\n  push:\n    branches: [main]\n', 'manual only')
                self.workflows[name] = original

    def test_reusable_promotion_caller_is_rejected_anywhere(self):
        self.workflows['unexpected.yml'] = 'jobs:\n  promote:\n    uses: ./.github/workflows/promote-release.yml\n'
        self.assertIn('automatic promotion caller: unexpected.yml', boundary_errors(self.workflows))

    def test_publication_rejects_missing_or_optional_attestation_inputs(self):
        for name in ('acceptance_run_id', 'attestation_sha256'):
            with self.subTest(name=name):
                original = self.workflows['publish-release.yml']
                block = section(original, name, 6)
                self.reject_change('publish-release.yml', block, block.replace('required: true', 'required: false'),
                                   'required string: ' + name)
                self.workflows['publish-release.yml'] = original

    def test_non_main_publication_is_rejected_before_loading_helpers(self):
        self.reject_change('publish-release.yml', 'run: test "$GITHUB_REF" = refs/heads/main',
                           'run: true', 'refuse non-main')

    def test_publication_cannot_load_a_tag_controlled_helper(self):
        self.reject_change('publish-release.yml', 'ref: main', 'ref: ${{ inputs.tag }}', 'come from main')

    def test_publication_cannot_drop_the_trusted_attestation_hash(self):
        self.reject_change('publish-release.yml', '--attestation-sha256 "$ATTESTATION_SHA256"',
                           '--attestation-sha256 untrusted', 'all bound inputs')

    def test_publication_rejects_rebuild_asset_mutation_and_latest_updates(self):
        for command in ('go build .', 'python3 buildscripts/build-release.py TAG', 'git tag TAG',
                        'gh release upload TAG rebuilt.tar.gz', 'gh release edit TAG --latest',
                        'docker buildx imagetools create --tag repo:latest repo@sha256:hash'):
            with self.subTest(command=command):
                original = self.workflows['publish-release.yml']
                self.workflows['publish-release.yml'] += '\n          ' + command + '\n'
                self.assertIn('publication may not rebuild, upload or change latest', boundary_errors(self.workflows))
                self.workflows['publish-release.yml'] = original

    def test_publication_cannot_gain_registry_write_permission(self):
        self.reject_change('publish-release.yml', 'packages: read', 'packages: write', 'registry packages')

    def test_shared_lock_cannot_be_removed_from_any_stage(self):
        for name in ('release.yml', 'publish-release.yml', 'promote-release.yml'):
            with self.subTest(name=name):
                original = self.workflows[name]
                self.reject_change(name, 'group: oc-stable-promotion', 'group: unrelated', 'share the publication')
                self.workflows[name] = original

    def test_acceptance_rejects_automatic_triggers_and_non_main_execution(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        self.reject_change(name, 'on:\n', 'on:\n  pull_request:\n', 'acceptance must be manual only')
        self.workflows[name] = original
        self.reject_change(name, 'run: test "$GITHUB_REF" = refs/heads/main', 'run: true', 'first step')

    def test_acceptance_rejects_optional_tags_and_unchecked_partner_checkout(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for key in ('oc_tag', 'otterio_tag'):
            with self.subTest(tag=key):
                block = section(original, key, 6)
                self.reject_change(name, block, block.replace('required: true', 'required: false'), 'required string: ' + key)
                self.workflows[name] = original
        for tag in ('OC_TAG', 'OTTERIO_TAG'):
            with self.subTest(validation=tag):
                self.reject_change(name, "stage.tag_valid(os.environ['" + tag + "'])", 'pass', 'before partner checkout')
                self.workflows[name] = original
        blocks = steps(original, 'packaged-pair')
        validation = next(block for block in blocks if 'stage.tag_valid(' in step_run(block))
        partner = next(block for block in blocks if 'repository: soulteary/otterio' in block)
        self.workflows[name] = original.replace(validation[8:], '__VALIDATION__', 1).replace(partner[8:], validation[8:], 1).replace('__VALIDATION__', partner[8:], 1)
        self.assertIn('both tags must be validated by the trusted helper before partner checkout', boundary_errors(self.workflows))

    def test_acceptance_uses_exact_own_sha_and_requested_partner_tag(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for before, after, message in (
                ('ref: ${{ github.sha }}', 'ref: main', 'exact github.sha'),
                ('ref: ${{ inputs.otterio_tag }}', 'ref: main', 'validated Otterio tag'),
                ('persist-credentials: false', 'persist-credentials: true', 'persisted credentials')):
            with self.subTest(change=before):
                self.reject_change(name, before, after, message)
                self.workflows[name] = original

    def test_acceptance_permissions_remain_read_only(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for permission in ('contents', 'actions', 'packages'):
            with self.subTest(permission=permission):
                self.reject_change(name, permission + ': read', permission + ': write', 'write permissions')
                self.workflows[name] = original
        self.reject_change(name, '  packaged-pair:\n', '  packaged-pair:\n    permissions: write-all\n', 'write permissions')

    def test_acceptance_cannot_publish_or_mutate_any_candidate(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for command in ('gh release edit TAG --draft=false', 'gh release upload TAG rebuilt.tar.gz',
                        'python3 buildscripts/release-stage.py publish', 'git push origin TAG',
                        'docker buildx imagetools create --tag repo:latest repo@sha256:hash',
                        'gh api repos/soulteary/oc/releases/1 --method PATCH'):
            with self.subTest(command=command):
                self.reject_change(name, '          python3 buildscripts/console_release_acceptance.py',
                                   '          ' + command + '\n          python3 buildscripts/console_release_acceptance.py',
                                   'may not publish')
                self.workflows[name] = original

    def test_acceptance_cannot_skip_or_suppress_required_verification(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for extra in ('        if: false\n', '        continue-on-error: true\n'):
            with self.subTest(extra=extra.strip()):
                self.reject_change(name, '      - name: Accept the actual draft programs and immutable image digests\n',
                                   '      - name: Accept the actual draft programs and immutable image digests\n' + extra,
                                   'cannot be skipped')
                self.workflows[name] = original

    def test_acceptance_requires_actual_draft_download_and_wrapper_exit_status(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for before, after in (
                ('--download-drafts', '--oc-assets candidate-oc --otterio-assets candidate-otterio'),
                ('--output "$RUNNER_TEMP/console-release-acceptance"', '--output "$RUNNER_TEMP/console-release-acceptance" || true')):
            with self.subTest(change=before):
                self.reject_change(name, before, after, 'actual draft wrapper')
                self.workflows[name] = original

    def test_acceptance_baseline_and_independent_manifest_digest_cannot_drift(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for before, after in (
                ('gh release download RELEASE.2026-10-10T16-38-05Z', 'gh release download latest'),
                ('gh release download RELEASE.2026-10-10T11-42-18Z', 'gh release download latest'),
                ('gh api repos/soulteary/otterio/releases/tags/RELEASE.2026-10-10T16-38-05Z', 'cat "$previous/otterio/release-manifest.json"'),
                ('test "$digest" = sha256:2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3', 'test -n "$digest"')):
            with self.subTest(change=before):
                self.reject_change(name, before, after, 'independently verify')
                self.workflows[name] = original

    def test_acceptance_always_preserves_raw_reports_even_after_failure(self):
        name = 'console-release-acceptance.yml'
        original = self.workflows[name]
        for before, after in (
                ('        if: always()', '        if: success()'),
                ('console-release-acceptance/*.json', 'console-release-acceptance/acceptance.json'),
                ('          if-no-files-found: error', '          if-no-files-found: ignore'),
                ('          name: console-release-acceptance', '          name: unbound-report')):
            with self.subTest(change=before):
                self.reject_change(name, before, after, 'every original JSON report')
                self.workflows[name] = original


if __name__ == '__main__':
    unittest.main()
