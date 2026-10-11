import copy
import unittest
import tempfile
from pathlib import Path
from unittest.mock import patch
from console_oidc_keycloak_acceptance import check_discovery, realm_config, LoginForm, CaptureCallback, check_refresh_claims, program_identity


class KeycloakProfileTests(unittest.TestCase):
    def test_realm_forces_code_pkce_rs256_and_distinct_user_policies(self):
        realm = realm_config('https://console.invalid/api/oidc/callback', {'alice': 'first', 'bob': 'second'})
        client = realm['clients'][0]
        self.assertFalse(client['directAccessGrantsEnabled'])
        self.assertEqual(client['attributes']['pkce.code.challenge.method'], 'S256')
        self.assertEqual(client['attributes']['id.token.signed.response.alg'], 'RS256')
        self.assertEqual(client['redirectUris'], ['https://console.invalid/api/oidc/callback'])
        self.assertEqual([user['attributes']['policy'] for user in realm['users']], [['alice-read'], ['bob-read']])

    def test_confidential_profile_preserves_mandatory_pkce_and_fixed_callback(self):
        realm = realm_config('https://console.invalid/api/oidc/callback', {'alice': 'temporary'}, 'client-secret')
        client = realm['clients'][0]
        self.assertFalse(client['publicClient'])
        self.assertEqual(client['secret'], 'client-secret')
        self.assertEqual(client['attributes']['pkce.code.challenge.method'], 'S256')

    def test_discovery_rejects_cross_origin_and_missing_required_profile(self):
        issuer = 'https://idp.invalid/realms/fixture'
        valid = {'issuer': issuer, 'authorization_endpoint': 'https://idp.invalid/auth',
                 'token_endpoint': 'https://idp.invalid/token', 'jwks_uri': 'https://idp.invalid/keys',
                 'id_token_signing_alg_values_supported': ['RS256'], 'response_types_supported': ['code'],
                 'code_challenge_methods_supported': ['S256'], 'token_endpoint_auth_methods_supported': ['none']}
        check_discovery(valid, issuer)
        for field, value in [('issuer', issuer+'/'), ('jwks_uri', 'https://other.invalid/keys'),
                             ('token_endpoint', 'http://idp.invalid/token'), ('code_challenge_methods_supported', ['plain']),
                             ('id_token_signing_alg_values_supported', ['HS256'])]:
            invalid = copy.deepcopy(valid);invalid[field] = value
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                check_discovery(invalid, issuer)

    def test_login_only_collects_hidden_inputs_from_keycloak_password_form(self):
        form = LoginForm()
        form.feed('<form id="unrelated"><input type="hidden" name="bad" value="x"></form>'
                  '<form id="kc-form-login" action="https://idp.invalid/login?a=1&amp;b=2">'
                  '<input type="hidden" name="csrf" value="protected"><input name="username"></form>')
        self.assertEqual(form.action, 'https://idp.invalid/login?a=1&b=2')
        self.assertEqual(form.fields, {'csrf': 'protected'})

    def test_refresh_nonce_may_be_absent_but_present_nonce_and_identity_must_match(self):
        initial = {'sub': 'alice', 'iss': 'https://idp.invalid', 'aud': 'oc-console', 'iat': 10, 'nonce': 'bound'}
        refreshed = {**initial, 'iat': 11}
        del refreshed['nonce']
        self.assertTrue(check_refresh_claims(initial, refreshed, 'bound')['nonceAbsentAllowed'])
        self.assertFalse(check_refresh_claims(initial, initial, 'bound')['nonceAbsentAllowed'])
        for field, value in [('nonce', 'wrong'), ('sub', 'bob'), ('iss', 'https://other.invalid'),
                             ('aud', 'other-client'), ('iat', 9)]:
            with self.subTest(field=field), self.assertRaises(RuntimeError):
                check_refresh_claims(initial, {**refreshed, field: value}, 'bound')

    @patch('console_oidc_keycloak_acceptance.run')
    def test_binary_evidence_refuses_dirty_or_wrong_source(self, run):
        source = 'a'*40
        with tempfile.TemporaryDirectory() as root:
            binary = Path(root)/'program';binary.write_bytes(b'program fixture')
            metadata = 'program: go1.27\n\tbuild\tvcs.revision='+source+'\n\tbuild\tvcs.modified=false\n\tbuild\tGOOS=darwin\n\tbuild\tGOARCH=arm64\n'
            run.return_value = metadata.encode()
            self.assertEqual(program_identity(str(binary), source)['binaryVCSRevision'], source)
            for invalid in (metadata.replace('modified=false', 'modified=true'), metadata.replace(source, 'b'*40)):
                run.return_value = invalid.encode()
                with self.assertRaises(RuntimeError):
                    program_identity(str(binary), source)

    def test_callback_redirect_is_not_sent_to_unstarted_console(self):
        handler = CaptureCallback('https://console.invalid/api/oidc/callback')
        self.assertIsNone(handler.redirect_request(None, None, 302, '', {},
                         'https://console.invalid/api/oidc/callback?code=sensitive'))


if __name__ == '__main__':
    unittest.main()
