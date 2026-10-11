# Isolated Keycloak OIDC acceptance

Date: 2026-10-12 (Asia/Shanghai). This is a real third-party IdP and real OtterIO STS protocol run, not completed OC Console session acceptance or a published-release certification.

## Profile and sources

The [official downloads page](https://www.keycloak.org/downloads) listed Keycloak 26.8.0 when checked. The fixture uses its fixed official Quay manifest digest `sha256:27b3230fbabb8c9ff3c66c864cf09dcd8a41aca82e5100679d109ef0eeb05741`; this run executed `linux/arm64` (architecture manifest `sha256:cae4f0fe0315f8c259a2e6deba8df93a5f1f297f65aa09198660add1056a8632`). Container configuration follows the official [container](https://www.keycloak.org/server/containers), [hostname](https://www.keycloak.org/server/hostname) and [realm-import](https://www.keycloak.org/server/importExport) documentation.

The supported fixture profile is confidential `client_secret_basic`, authorization code, mandatory PKCE S256, RS256, `openid offline_access`, fixed issuer/redirect URI and HTTPS with a temporary CA. Both imported users explicitly receive the `offline_access` realm role. A mapper supplies each user's distinct storage-policy claim in ID/access tokens. No existing IdP/admin account is accessed. The standalone development-mode IdP is temporary test infrastructure, not a production deployment recommendation.

The native OtterIO program came from clean source `cbbffbd40531f101cb67c263c69010a0a1b8ed4e`; CLI from clean `b32781defdcc859081fb7797ea15339c894804b0`. Both executed `darwin/arm64`. The harness verifies the declared source against binary VCS revision and refuses modified/mismatching programs. Full hashes, discovery capabilities, profile and cleanup are in [raw passing evidence](console-keycloak-sts-results.json).

## Actual results and limits

Both Alice and Bob completed real password-form authorization-code/PKCE login and token refresh. Real STS credentials read that user's permitted bucket prefix and receive 403 for another prefix and the other user's bucket. Refreshed tokens produce STS credentials with the same restrictions. Storage/Admin share one HTTPS listener in this run; dual-listener/dual-CA topology is not certified here.

Keycloak refresh ID tokens omit nonce. This is accepted by existing OC verification when refreshing; present nonce must match, and subject/issuer/audience must remain stable. The harness checks that rule. Its earlier overstrict check produced a [false-negative report](console-keycloak-harness-nonce-failed.json): it is a harness mistake, not a production protocol defect, and was corrected without weakening production validation. [OIDC Core section 12.2](https://openid.net/specs/openid-connect-core-1_0.html#RefreshTokenResponse) specifies the refresh-token ID-token rules. The Python claim inspection is structural; real STS verifies token signatures. OC's own cryptographic/session validation still needs the integrated Console run.

The [public-client negative profile](console-keycloak-public-profile-failed.json) fails discovery because this Keycloak version does not advertise the `none` token authentication method. The existing OC strict discovery rule is retained. No rewritten discovery document, patched token or weakened TLS check is used.

One intermediate run received an unclassified STS HTTP 400 on Bob's initial exchange after Alice passed; a subsequent fresh run with added policy-claim/error-code diagnostics passed both users unchanged. That intermediate response was not retained and its cause is not established. The passing run does not establish sustained stability.

## Reproduce

Prerequisites: Docker, Python 3 standard library, OpenSSL, Go build-info inspection, and clean built OC/OtterIO programs with their full source commits. One invocation:

```sh
python3 buildscripts/console_oidc_keycloak_acceptance.py --server /private/tmp/otterio-next-protocols-cbbffbd --server-source cbbffbd40531f101cb67c263c69010a0a1b8ed4e --oc /private/tmp/oc-next-b32781de --oc-source b32781defdcc859081fb7797ea15339c894804b0 --output /private/tmp/oc-keycloak-results.json
```

Add `--public-client` to reproduce the strict discovery rejection. Add `--console /absolute/path/to/integrated/oc-console --console-source FULL_COMMIT` only when the integrated OIDC CLI exists. Its confidential client secret is passed through a temporary 0600 `--oidc-client-secret-file`; credentials never appear in command arguments or reports. Random loopback ports, private temporary files, independent storage data/configuration and a uniquely named IdP container are used and cleaned at completion.

Seven focused tests pass: `python3 -m unittest discover -s buildscripts -p test_console_oidc_keycloak.py`. They cover fixed discovery profile/refusals, login form parsing, callback interception, nonce/identity refresh rules, and dirty/wrong binary-source rejection.

The protocol run does not certify browser session refresh/expiry/logout, simultaneous user preferences/tasks, callback-binding/replay negatives, long-running resource trends, amd64 execution or published OIDC assets. Those remain gates before default-off and deletion.
