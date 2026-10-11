import copy
import unittest
from console_parity_gate import evaluate

SOURCE, BINARY = 'a' * 40, 'b' * 64


class GateTest(unittest.TestCase):
    def setUp(self):
        self.matrix = {'schemaVersion': 1, 'capabilities': [{'id': 'range', 'status': 'verified-replacement', 'requiredChecks': ['range'], 'requiredProfiles': ['dual-tls']}]}
        self.report = {'schemaVersion': 1, 'platform': 'Darwin/arm64', 'server': {'source': SOURCE, 'binarySHA256': BINARY, 'sourceState': 'clean', 'kind': 'release-asset'}, 'harnesses': {'range.py': 'c' * 64}, 'passed': True, 'checks': [{'name': 'range', 'status': 'passed', 'exitCode': 0, 'profiles': [{'profile': 'dual-tls'}]}]}

    def ready(self, report=None):
        return evaluate(self.matrix, report or self.report, SOURCE, BINARY)['ready']

    def test_valid_structural_evidence(self):
        self.assertTrue(self.ready())

    def test_fail_skip_wrong_version_missing_profile(self):
        mutations = [lambda r: r['checks'][0].update(status='failed'), lambda r: r['checks'][0].update(skippedTests=['required']), lambda r: r['checks'][0]['profiles'][0].update(status='skipped'), lambda r: r['server'].update(source='d' * 40), lambda r: r['server'].update(binarySHA256='d' * 64), lambda r: r['checks'][0].update(profiles=[]), lambda r: r.update(checks=[]), lambda r: r.update(harnesses={}), lambda r: r['server'].update(kind='module-build'), lambda r: r['checks'].append(copy.deepcopy(r['checks'][0]))]
        for mutation in mutations:
            report = copy.deepcopy(self.report)
            mutation(report)
            with self.subTest(report=report):
                self.assertFalse(self.ready(report))

    def test_pending_matrix_never_inherits_history(self):
        self.matrix['capabilities'][0]['status'] = 'pending-verification'
        self.assertFalse(self.ready())

    def test_legacy_natural_language_evidence_rejected(self):
        self.assertFalse(self.ready({'status': 'passed', 'checks': ['range passed']}))


if __name__ == '__main__':
    unittest.main()
