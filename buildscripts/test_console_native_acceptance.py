"""Public IAM identifiers must not mask existing storage-secret disclosures."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest
from unittest.mock import Mock

from console_native_acceptance import native_iam_action, require_service_account_inventory, require_user_change

spec = importlib.util.spec_from_file_location('native_acceptance_scanner', Path(__file__).with_name('test-console-integration.py'))
acceptance = importlib.util.module_from_spec(spec)
spec.loader.exec_module(acceptance)


class NativeIAMAcceptanceTests(unittest.TestCase):
    def confirmed(self):
        return {'outcome': 'confirmed', 'user': {'accessKey': 'public-target', 'status': 'disabled'},
                'loginRequired': False, 'impact': {'kind': 'user', 'target': 'public-target',
                'state': 'known', 'coupledUsers': True, 'affectedAccessKeys': ['public-target', 'public-child']}}

    def request(self, reply):
        def client(path, method, body, expected, headers, allowed_credentials):
            self.assertEqual((path, method, expected, headers), ('/api/iam/actions', 'POST', 200, {'X-CSRF-Token': 'fixture-csrf'}))
            self.assertEqual(allowed_credentials, ('public-target', 'public-child'))
            payload = json.dumps(reply).encode()
            acceptance.require_no_browser_credentials(payload, ['public-target', 'existing-secret', 'public-child', 'child-secret'], allowed_credentials)
            return payload, {}
        return client

    def action(self, reply):
        return native_iam_action(self.request(reply), {'action': 'user.disable'},
                                 {'X-CSRF-Token': 'fixture-csrf'}, ['public-target', 'public-child'])

    def test_old_scanner_rejects_public_acknowledgement_without_narrow_exemption(self):
        with self.assertRaisesRegex(RuntimeError, 'disclosed storage credentials'):
            acceptance.require_no_browser_credentials(json.dumps(self.confirmed()).encode(), ['public-target', 'existing-secret'])

    def test_public_user_and_child_scope_are_allowed(self):
        result = self.action(self.confirmed())
        require_user_change(result, 'public-target', 'disabled', False, ['public-target', 'public-child'])

    def test_existing_parent_and_child_secrets_remain_forbidden(self):
        for leaked in ('existing-secret', 'child-secret'):
            with self.subTest(leaked=leaked):
                result = self.confirmed()
                result['unexpected'] = leaked
                with self.assertRaisesRegex(RuntimeError, 'disclosed storage credentials'):
                    self.action(result)

    def test_unlisted_identifier_remains_forbidden(self):
        with self.assertRaisesRegex(RuntimeError, 'disclosed storage credentials'):
            acceptance.require_no_browser_credentials(b'public-stranger', ['public-stranger'], ['public-target'])

    def test_acknowledgement_requires_matching_user_and_retirement_scope(self):
        mutations = [lambda result: result.update(outcome='unknown'),
                     lambda result: result['user'].update(accessKey='other-target'),
                     lambda result: result['user'].update(status='enabled'),
                     lambda result: result.update(loginRequired=True),
                     lambda result: result.update(restartRequired=True),
                     lambda result: result['impact'].update(target='other-target'),
                     lambda result: result['impact'].update(coupledUsers=False),
                     lambda result: result['impact'].update(affectedAccessKeys=['public-child'])]
        for mutate in mutations:
            result = copy.deepcopy(self.confirmed())
            mutate(result)
            with self.assertRaisesRegex(RuntimeError, 'retirement scope'):
                require_user_change(result, 'public-target', 'disabled', False, ['public-target', 'public-child'])

    def test_retirement_scope_requires_exact_parent_and_children(self):
        for affected in (['public-target'], ['public-child'], [],
                         ['public-target', 'public-child', 'unrelated-key'],
                         ['public-target', 'public-target'],
                         ['public-target', 'public-child', 'public-child']):
            with self.subTest(affected=affected):
                result = self.confirmed()
                result['impact']['affectedAccessKeys'] = affected
                with self.assertRaisesRegex(RuntimeError, 'retirement scope'):
                    require_user_change(result, 'public-target', 'disabled', False, ['public-target', 'public-child'])

    def test_retirement_scope_order_is_irrelevant(self):
        result = self.confirmed()
        result['impact']['affectedAccessKeys'].reverse()
        require_user_change(result, 'public-target', 'disabled', False, ['public-target', 'public-child'])

    def test_retirement_scope_requires_an_array_of_access_keys(self):
        for affected in (None, 'public-target', {'public-target': True},
                         ['public-target', None], ['public-target', []]):
            with self.subTest(affected=affected):
                result = self.confirmed()
                result['impact']['affectedAccessKeys'] = affected
                with self.assertRaisesRegex(RuntimeError, 'retirement scope'):
                    require_user_change(result, 'public-target', 'disabled', False, ['public-target', 'public-child'])

    def test_inventory_confirms_creation_deletion_and_no_child_after_parent_recreation(self):
        child = {'accessKey': 'public-child', 'parentUser': 'public-target', 'status': 'on'}
        client = Mock(side_effect=[(json.dumps({'user': 'public-target', 'serviceAccounts': accounts}).encode(), {})
                                  for accounts in ([child], [], [])])
        for present in (True, False, False):
            require_service_account_inventory(client, 'public-target', 'public-child', present, ['public-target', 'public-child'])
        self.assertEqual(client.call_count, 3)
        for call in client.call_args_list:
            self.assertEqual(call.args, ('/api/iam/service-accounts?user=public-target',))
            self.assertEqual(call.kwargs, {'expected': 200, 'allowed_credentials': ('public-target', 'public-child')})

    def test_disabled_or_orphaned_child_in_inventory_is_not_deleted(self):
        for status in ('on', 'off'):
            result = {'user': 'public-target', 'serviceAccounts': [
                {'accessKey': 'public-child', 'parentUser': 'public-target', 'status': status}]}
            client = Mock(return_value=(json.dumps(result).encode(), {}))
            with self.subTest(status=status), self.assertRaisesRegex(RuntimeError, 'child presence'):
                require_service_account_inventory(client, 'public-target', 'public-child', False, ['public-target', 'public-child'])

    def test_inventory_errors_and_malformed_results_cannot_establish_absence(self):
        client = Mock(side_effect=RuntimeError('management inventory query failed'))
        with self.assertRaisesRegex(RuntimeError, 'management inventory query failed'):
            require_service_account_inventory(client, 'public-target', 'public-child', False, ['public-target', 'public-child'])
        for result in ({'code': 'iam_not_found'}, {'user': 'public-target'},
                       {'user': 'other-parent', 'serviceAccounts': []},
                       {'user': 'public-target', 'serviceAccounts': None},
                       {'user': 'public-target', 'serviceAccounts': [{}]},
                       {'user': 'public-target', 'serviceAccounts': [{'accessKey': 'public-child', 'parentUser': 'other-parent'}]}):
            client = Mock(return_value=(json.dumps(result).encode(), {}))
            with self.subTest(result=result), self.assertRaisesRegex(RuntimeError, 'inventory'):
                require_service_account_inventory(client, 'public-target', 'public-child', False, ['public-target', 'public-child'])


if __name__ == '__main__':
    unittest.main()
