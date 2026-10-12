# Isolated Keycloak OIDC acceptance

Date: 2026-10-12 (Asia/Shanghai). This records real third-party IdP, OtterIO STS and integrated OC Console session acceptance. It is not a published-release certification.

## Profile and sources

The [official downloads page](https://www.keycloak.org/downloads) listed Keycloak 26.8.0 when checked. The fixture uses its fixed official Quay manifest digest `sha256:27b3230fbabb8c9ff3c66c864cf09dcd8a41aca82e5100679d109ef0eeb05741`; this run executed `linux/arm64` (architecture manifest `sha256:cae4f0fe0315f8c259a2e6deba8df93a5f1f297f65aa09198660add1056a8632`). Container configuration follows the official [container](https://www.keycloak.org/server/containers), [hostname](https://www.keycloak.org/server/hostname) and [realm-import](https://www.keycloak.org/server/importExport) documentation.

The supported fixture profile is confidential `client_secret_basic`, authorization code, mandatory PKCE S256, RS256, `openid offline_access`, fixed issuer/redirect URI and HTTPS with a temporary CA. Both imported users explicitly receive the `offline_access` realm role. A mapper supplies each user's distinct storage-policy claim in ID/access tokens. No existing IdP/admin account is accessed. The standalone development-mode IdP is temporary test infrastructure, not a production deployment recommendation.

The native OtterIO program came from clean source `cbbffbd40531f101cb67c263c69010a0a1b8ed4e`; CLI from clean `b32781defdcc859081fb7797ea15339c894804b0`. Both executed `darwin/arm64`. The harness verifies the declared source against binary VCS revision and refuses modified/mismatching programs. Full hashes, discovery capabilities, profile and cleanup are in [raw passing evidence](console-keycloak-sts-results.json).

## Actual results and limits

Both Alice and Bob completed real password-form authorization-code/PKCE login and token refresh. Real STS credentials read that user's permitted bucket prefix and receive 403 for another prefix and the other user's bucket. Refreshed tokens produce STS credentials with the same restrictions. The original raw protocol run used one HTTPS listener. The integrated Console results below identify their actual topology independently.

Keycloak refresh ID tokens omit nonce. This is accepted by existing OC verification when refreshing; present nonce must match, and subject/issuer/audience must remain stable. The harness checks that rule. Its earlier overstrict check produced a [false-negative report](console-keycloak-harness-nonce-failed.json): it is a harness mistake, not a production protocol defect, and was corrected without weakening production validation. [OIDC Core section 12.2](https://openid.net/specs/openid-connect-core-1_0.html#RefreshTokenResponse) specifies the refresh-token ID-token rules. The Python claim inspection is structural; real STS verifies token signatures. The integrated Console runs additionally exercise OC's cryptographic and session validation.

The [public-client negative profile](console-keycloak-public-profile-failed.json) fails discovery because this Keycloak version does not advertise the `none` token authentication method. The existing OC strict discovery rule is retained. No rewritten discovery document, patched token or weakened TLS check is used.

The native macOS OtterIO / Docker Keycloak pairing exposed a real clock boundary: STS rejected a token whose `iat` was about 0.279 seconds ahead of the server (`InvalidParameterValue`, before-issued classification). The [classified failure](console-keycloak-native-clock-failed.json) and [earlier intermittent failure](console-keycloak-sts-intermittent-failed.json) are retained. The harness does not retry or relax verification. The subsequent integrated runs put OtterIO and Keycloak in the same temporary Docker network/clock; this resolves the fixture clock mismatch rather than changing production token checks.

The [first integrated run](console-keycloak-console-first-results.json) uses clean Console `ddcc66bb89d7cc41966286a7a2f887d772fee047`. The [final single HTTPS run](console-keycloak-console-results.json) uses clean Console `51ee915235ac20a3ee49a75fd60136f3d2032b9d`, SHA-256 `73175254cff409f4a60786e67ca0670c8742812a90f365daa99ca8c8c0f25a36`, `darwin/arm64`. Both pass 13 checks and cleanup. They retain two simultaneous cookie jars; prove user prefix/bucket restrictions, preferences and archive-task isolation; reject wrong-browser callback binding and replay; reject explicitly saved Alice cookies after logout while Bob continues; and observe successful real Keycloak refresh events while Bob reads after the original 90-second token lifetime. The final run explicitly replays Bob's saved cookie after the fixed 150-second deadline and gets 401, rather than relying on cookie-jar expiry.

The [final dual HTTPS run](console-keycloak-console-dual-results.json) executes the same final Console/server sources and passes all 14 checks plus cleanup. Admin has a distinct endpoint and independently generated CA: trusting only the S3 CA fails the Admin handshake, while the explicit Admin CA succeeds. Each real authenticated `/api/account` response exposes only its own bucket. Login, callback binding/replay rejection, object permissions, simultaneous preferences/tasks, Alice logout with Bob continuing, observed refresh and explicit saved-cookie rejection after the fixed 150-second deadline all pass in this separate topology. The report records both distinct CA hashes; no private keys or secrets are retained.

The server used in these integrated runs is a clean `linux/arm64` binary from `19560364ec735f5b811964f5199b2b59e7100400`, SHA-256 `08d868f66bd03db38c0d118aad52e93660e13e3f60712eba5f413afb5a954f34`. It is mounted as the entrypoint in the fixed published OtterIO base image; these results certify the recorded development binary, not the published image entrypoint. Declared commits, binary-derived VCS revisions, modified state, hashes, fixture source and executed platforms are recorded separately in each JSON report.

## Reproduce

Prerequisites: Docker, Python 3 standard library, OpenSSL, Go build-info inspection, and clean built OC/OtterIO programs with their full source commits. One invocation:

```sh
python3 buildscripts/console_oidc_keycloak_acceptance.py --server /private/tmp/otterio-next-protocols-cbbffbd --server-source cbbffbd40531f101cb67c263c69010a0a1b8ed4e --oc /private/tmp/oc-next-b32781de --oc-source b32781defdcc859081fb7797ea15339c894804b0 --output /private/tmp/oc-keycloak-results.json
```

Add `--public-client` to reproduce the strict discovery rejection. For the integrated same-clock Console run:

```sh
python3 buildscripts/console_oidc_keycloak_acceptance.py --server /private/tmp/otterio-keycloak-linux-19560364.bin --server-source 19560364ec735f5b811964f5199b2b59e7100400 --server-container-image ghcr.io/soulteary/otterio:RELEASE.2026-10-09T15-22-07Z@sha256:7ddf211748d11bb8f6af23576f19eaf5b2675f70f647e38ed9fdd14912824ba8 --oc /private/tmp/oc-next-b32781de --oc-source b32781defdcc859081fb7797ea15339c894804b0 --console /private/tmp/oc-console-next-51ee9152 --console-source 51ee915235ac20a3ee49a75fd60136f3d2032b9d --console-session-ttl 150s --dual-admin --output /private/tmp/oc-keycloak-console-dual-results.json
```

Omit `--dual-admin` for single HTTPS. The confidential client secret is passed through a temporary 0600 `--oidc-client-secret-file`; credentials never appear in command arguments or reports. Random loopback ports, private temporary files, independent storage data/configuration and uniquely named containers are cleaned at completion. Root bootstrap credentials are removed from the Console process environment. Protected Console logs are checked for fixture secret leakage; the report contains only the pass/fail result.

Eight focused tests pass: `python3 -m unittest discover -s buildscripts -p test_console_oidc_keycloak.py`. They cover fixed discovery profile/refusals, login form parsing, callback interception, nonce/identity refresh rules, and dirty/wrong binary-source rejection.

The session clients are Python HTTPS cookie jars, not graphical browsers. Browser rendering and real browser SameSite enforcement, long-running resource trends, amd64 execution and published OIDC assets remain unverified. These acceptance results alone do not authorize default-off or deletion.
