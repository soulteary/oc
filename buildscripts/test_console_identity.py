"""Do not mislabel acceptance binaries with the current dependency manifest."""
import importlib.util
import json
from pathlib import Path
import unittest

spec = importlib.util.spec_from_file_location('console_acceptance', Path(__file__).with_name('test-console-integration.py'))
acceptance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(acceptance)
SUPPORT = json.loads((Path(__file__).resolve().parents[1] / 'docs' / 'compatibility.json').read_text())
SDK = SUPPORT['otterioSDK']
SOURCE = SUPPORT['otterioSource']


def info():
    return {'oc': {'Deps': [{'Path': 'github.com/soulteary/otterio', 'Version': SDK}]},
            'oc-console': {'Deps': [{'Path': 'github.com/soulteary/otterio', 'Version': SDK}]},
            'otterio': {'Settings': [{'Key': 'vcs.revision', 'Value': SOURCE},
                                     {'Key': 'vcs.modified', 'Value': 'false'}]}}


class ConsoleBinaryIdentityTests(unittest.TestCase):
    def test_requested_sdk_and_binary_revision_are_verified(self):
        evidence = acceptance.verify_binary_identity(info(), SDK, SOURCE)
        self.assertEqual(evidence, {'sdkPin': 'verified-client-build-info', 'serverSource': 'verified-vcs-revision'})

    def test_wrong_or_replaced_sdk_and_wrong_server_are_rejected(self):
        for changed in ('wrong-sdk', 'missing-sdk', 'replacement', 'wrong-server'):
            with self.subTest(changed=changed):
                build = info()
                if changed == 'wrong-sdk':
                    build['oc']['Deps'][0]['Version'] = 'v0.0.0-old'
                elif changed == 'missing-sdk':
                    build['oc']['Deps'] = []
                elif changed == 'replacement':
                    build['oc']['Deps'][0]['Replace'] = {'Path': '/tmp/local-source'}
                else:
                    build['otterio']['Settings'][0]['Value'] = '0' * 40
                with self.assertRaises(ValueError):
                    acceptance.verify_binary_identity(build, SDK, SOURCE)

    def test_console_binary_must_match_the_verified_cli_pin(self):
        for changed in ('wrong-sdk', 'missing-sdk', 'replacement'):
            with self.subTest(changed=changed):
                build = info()
                if changed == 'wrong-sdk':
                    build['oc-console']['Deps'][0]['Version'] = 'v0.0.0-old'
                elif changed == 'missing-sdk':
                    build['oc-console']['Deps'] = []
                else:
                    build['oc-console']['Deps'][0]['Replace'] = {'Path': '/tmp/local-console-source'}
                with self.assertRaises(ValueError):
                    acceptance.verify_binary_identity(build, SDK, SOURCE)

    def test_archive_and_modified_source_are_explicit(self):
        archive = info()
        archive['otterio']['Settings'] = []
        self.assertEqual(acceptance.verify_binary_identity(archive, SDK, SOURCE)['serverSource'],
                         'declared-module-source-without-binary-vcs')
        modified = info()
        modified['otterio']['Settings'][1]['Value'] = 'true'
        self.assertEqual(acceptance.verify_binary_identity(modified, SDK, SOURCE)['serverSource'],
                         'verified-base-vcs-revision')


if __name__ == '__main__':
    unittest.main()
