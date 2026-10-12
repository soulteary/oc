"""Public IAM identifiers must not mask existing storage-secret disclosures."""
import copy
import importlib.util
import json
from pathlib import Path
import unittest
import urllib.error
from unittest.mock import Mock

from console_native_acceptance import native_iam_action, require_service_account_info, require_service_account_inventory, require_user_change

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

    def not_found(self):
        return {'Code': 'XOtterioInvalidIAMCredentials', 'Message': 'The specified service account is not found (Specified service account does not exist)',
                'Resource': '/otterio/admin/v3/info-service-account'}

    def direct_info(self, request, present=False):
        require_service_account_info(request, 'https://admin.example', 'manager-access', 'manager-secret',
                                     'verified-admin-context', 'public-child', present)

    def test_direct_info_uses_manager_and_child_key_without_parent_filter(self):
        request = Mock(return_value=(200, b'encrypted-info', {'Content-Type': 'application/json'}))
        self.direct_info(request, True)
        request.assert_called_once_with('https://admin.example', 'GET', '/otterio/admin/v3/info-service-account', b'',
                                        'manager-access', 'manager-secret', 'verified-admin-context', query='accessKey=public-child')

    def test_direct_info_accepts_only_explicit_service_account_not_found(self):
        request = Mock(return_value=(404, json.dumps(self.not_found()).encode(), {'Content-Type': 'application/json'}))
        self.direct_info(request)

    def test_direct_info_rejects_authorization_and_other_failures_as_absence(self):
        payload = json.dumps(self.not_found()).encode()
        responses = [(status, payload, {'Content-Type': 'application/json'}) for status in (200, 401, 403, 500)]
        responses += [(404, b'<html>not found</html>', {'Content-Type': 'text/html'}),
                      (404, b'not JSON', {'Content-Type': 'application/json'}),
                      (404, b'[]', {'Content-Type': 'application/json'})]
        for field, value in (('Code', 'AccessDenied'), ('Message', 'The account key is not eligible for this operation'),
                             ('Resource', '/proxy/missing-route')):
            result = self.not_found()
            result[field] = value
            responses.append((404, json.dumps(result).encode(), {'Content-Type': 'application/json'}))
        for response in responses:
            with self.subTest(response=response), self.assertRaisesRegex(RuntimeError, 'direct service-account info'):
                self.direct_info(Mock(return_value=response))

    def test_direct_info_does_not_swallow_transport_failure(self):
        request = Mock(side_effect=urllib.error.URLError('synthetic connection failure'))
        with self.assertRaises(urllib.error.URLError):
            self.direct_info(request)

    def test_direct_info_requires_successful_nonempty_creation_evidence(self):
        for response in ((404, json.dumps(self.not_found()).encode(), {'Content-Type': 'application/json'}),
                         (403, b'denied', {'Content-Type': 'application/json'}),
                         (200, b'', {'Content-Type': 'application/json'}),
                         (200, b'<html>proxy</html>', {'Content-Type': 'text/html'})):
            with self.subTest(response=response), self.assertRaisesRegex(RuntimeError, 'child existence'):
                self.direct_info(Mock(return_value=response), True)


if __name__ == '__main__':
    unittest.main()
