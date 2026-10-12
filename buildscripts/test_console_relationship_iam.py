import unittest
from console_relationship_iam_acceptance import binding_action, require_confirmed


class RelationshipContractTests(unittest.TestCase):
    def test_reviewed_revision_retained(self):
        result = binding_action('user', 'alice', {'kind': 'user', 'target': 'alice', 'revision': 'a'*64, 'conditional': True}, ['readonly'])
        self.assertEqual(result['revision'], 'a'*64)
        self.assertEqual(result['confirmTarget'], 'alice')

    def test_missing_capability_and_wrong_target_rejected(self):
        for snapshot in ({'kind': 'user', 'target': 'bob', 'conditional': True, 'revision': 'a'*64},
                         {'kind': 'user', 'target': 'alice', 'conditional': False, 'revision': 'a'*64},
                         {'kind': 'user', 'target': 'alice', 'conditional': True, 'revision': 'a'}):
            with self.assertRaises(RuntimeError):
                binding_action('user', 'alice', snapshot, [])

    def test_actor_acknowledgement_must_match_scope(self):
        require_confirmed({'outcome': 'confirmed', 'loginRequired': True}, True)
        for result in ({'outcome': 'confirmed', 'loginRequired': False}, {'outcome': 'unknown', 'loginRequired': True}, {'outcome': 'confirmed'}):
            with self.assertRaises(RuntimeError):
                require_confirmed(result, True)


if __name__ == '__main__':
    unittest.main()
