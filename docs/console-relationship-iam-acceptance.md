# Isolated native relationship IAM acceptance

Date: 2026-10-12 (Asia/Shanghai). The fixture uses disposable local storage, real CLI bootstrap, verified HTTPS and actual native Console sessions. It never adds root credentials to a browser session, mutates existing users or claims that unsupported service/STS browser sessions were tested.

The [known-denial-fixed positive results](console-relationship-supported-results.json) pass 22 acceptance groups in each of single HTTPS and independent S3/Admin HTTPS/CA topologies. The [same-runtime legacy capability results](console-relationship-legacy-fixed-results.json) pass 12 groups per topology. These runs use clean Console `52310833272b25c9ef146562eb8470d4e2dafb1a`, CLI `2a5a0f2152190ae438583a219f05cac84ddd3a9b`, final positive server `ff6e66f3fbd7e57198ef5df016fdd79e5545fc7b` and negative legacy server `cbbffbd40531f101cb67c263c69010a0a1b8ed4e`. All execute `darwin/arm64`. [Independent program identities](console-relationship-program-identities.json) verify declared full commits against embedded VCS, require `modified=false` and bind actual hashes; report filenames are not source evidence.

The successful positive runs cover administrator secret rotation of self/other users, disabled users remaining disabled after secret replacement, ordinary/explicit-deny/readonly/CSRF negatives, stale user/group binding revisions preserving the newer state and sessions, fresh bindings and group membership/status changes retiring only affected native users, and real child signed-S3 disable/enable/rotate/policy/delete semantics. Service policy edits are accepted for the authoritative parent and rejected for a general manager.

These reports predate explicit `processCleanup` instrumentation in the shared wrapper. The cleanup finally paths returned without secondary errors, but these reports do not assert a recorded cleanup status. The later browser callback landing-page change is a separate runtime and is not certified by these native reports.

## Same-build browser-callback core rerun

The [final-at-this-round positive report](console-relationship-final-core-results.json) and [legacy report](console-relationship-final-core-legacy-results.json) run clean Console `4f32ddba0e5a69fd9c6bf4646fdff99889c6ffca`, SHA-256 `595e6f5f60442f862a2eb22c68f2e4b969413f433015f0e8b9c74669fee52d9d` ([independent binding](console-relationship-final-core-identity.json)). CLI and server/legacy sources remain exactly those above. Supported single/dual HTTPS each pass 22 groups; legacy each passes 12. Both helper-owned native writer/readonly processes and the shared wrapper process cleanup explicitly report `passed` in every scenario. This records bounded process and fixture-resource cleanup, not an instrumented claim about the outer temporary-directory context.

The strengthened secret-rotation tests restore the exact original credentials before either original browser requests again. Both old cookies remain 401, proving explicit Console retirement rather than simply observing upstream secret mismatch. New-secret login succeeds before the restoration; unrelated sessions continue.

An [intermediate fixture-rate-limit result](console-relationship-login-rate-limit-failed.json) hit the production 30-logins-per-minute ceiling at the unnecessary 31st final login. The redundant login was removed; positive new-secret login and the stronger original-cookie non-revival proof remain. No rate limiter was disabled, spoofed or relaxed. The complete fixture subsequently passed unchanged production limits.

The new coupled parent-user lifecycle protocol identified by later review is outside this round's certified positive actions. User disable/enable/delete with child identities must be separately tested against its forthcoming contract/binaries; this report does not claim that coverage.

## Retained failures

The [first fixture failure](console-relationship-fixture-policy-failed.json) occurs while importing a fixture policy: the policy parser forbids combining admin and S3 actions in one statement. The harness was corrected to use separate statements. This is a harness error, not a production IAM defect.

The [real known-denial failure](console-relationship-known-denial-failed.json) uses clean Console `fcb9a168d0c1ffa0ac7e7a88acfee714eefe7293` and clean OtterIO `ff6e66f3fbd7e57198ef5df016fdd79e5545fc7b`. After successful other-user secret rotation, disabled-account secret rotation and user/group binding checks, a group denial returns Console HTTP 502 `outcome_unknown` instead of 403. Source inspection identifies `XOtterioIAMActionNotAllowed` as the server code for this atomic authorization refusal; the client lacked its precise mapping. The original report retains the observable Console response rather than a raw upstream body. These failures remain separate from later passing evidence, and unexecuted later cases are not counted.

## Scope

Native login accepts enabled IAM users; service/STS browser login is rejected. The passing positive checks separately exercise a real service child with signed S3 requests: parent secret rotation does not imply upstream service-credential revocation; parent policy narrowing must restrict child permissions. Service disable/enable, old/new secret use, policy intersection and deletion are checks independent of browser session lifecycle checks.

Service policy changes are submitted by the authoritative native parent with an explicit policy granting the required management actions and bucket read access. A general manager with UpdateServiceAccount and data permissions must still receive 403 when editing another parent's child policy. No root Console session is used.

The [legacy capability results](console-relationship-legacy-results.json) use the same recorded final-at-that-point Console and older clean server `cbbffbd40531f101cb67c263c69010a0a1b8ed4e`: single HTTPS and separate S3/Admin HTTPS both pass 12 acceptance groups. This proves rejection when coupled relationship capabilities are absent and preservation of original credentials/sessions. It does not count as positive mutation coverage. On this profile a capability rejection can precede ordinary-user/explicit-deny authorization, so the report does not claim those paths prove permission handling.

## Reproduce

The integrated wrapper runs single HTTPS and independent S3/Admin CA profiles by default. Pass clean CLI, Console and server binaries explicitly:

```sh
python3 buildscripts/test-console-integration.py --relationship-iam --cli /absolute/path/to/oc --console /absolute/path/to/oc-console --server /absolute/path/to/otterio --server-source FULL_SERVER_COMMIT --output /private/tmp/relationship-results.json
```

For the negative legacy capability profile, add `--expect-relationship-unsupported` and supply the older server. The report binds program hashes, server build metadata, wrapper/helper digests and the executed architecture. Dedicated helper contract tests: `python3 -m unittest discover -s buildscripts -p test_console_relationship_iam.py` (three tests).

Long-running resource trends, amd64 execution, graphical native UI and published release assets remain outside this fixture. Deleting OtterIO Console or changing deployment defaults requires the broader plan gates.

## Coupled user lifecycle preflight failures

The initial b8404f29 Console / 4b2ac9ce server preflight failed because the fixture created two unnecessary target sessions and reached the production eight-active-session limit. The correction reuses existing live target cookies; it does not change either the active-session or login-rate limit. The second preflight reached a confirmed disable response, but the fixture secret scanner mistakenly classified the STS access-key identifier in the impact acknowledgement as a secret. The correction permits that public identifier only; STS secret keys and session tokens remain forbidden. Both unmodified failed reports are retained. Neither is evidence of lifecycle acceptance or of the later service-create freshness fix.

The failed reports record shared process cleanup as passed. Since the helper raised before returning its metrics, they do not include helper process/resource cleanup metrics; no such field has been retroactively added. Final supported and legacy acceptance still requires the final clean server and both HTTPS topologies.

## Protected creation and coupled users: a7ebefb0 / f2bdc234

Independent Go build metadata and SHA-256 binding confirm the clean OC CLI and Console pair at a7ebefb03f8669cb93eb927a96f320a6e20229d4, server at f2bdc23403ee0c4b35f9ce4092c68b80074349e1, and legacy server at cbbffbd40531f101cb67c263c69010a0a1b8ed4e. The identity report records each declared revision separately from the revision extracted from the binary.

The final legacy run passed 13 acceptance groups in both single and independent dual HTTPS topologies. Protected native service creation rejects missing capability before writing; a management user's before/after service-account lists remain identical. Both shared and helper process cleanup report passed. The first list probe incorrectly used the root account, which is not eligible for this operation; the corrected probe uses a temporary 0600 management alias and restores the original fixture configuration.

Supported acceptance is FAILED, not complete. The first collision probe supplied a 22-character access key and 48-character secret, outside service creation's 20/40 limits, and received Console 502. That report does not prove a server collision acknowledgement failure. With an existing 19-character native access key and a 40-character secret, protected creation correctly returned 409 identity_revision_conflict and existing user sessions remained available.

The corrected supported run then reached a confirmed parent disable. Real service-account signed S3 reads were denied with 403, but the real native AssumeRole STS child still read the same object with 200. A separate single HTTPS diagnostic reproduced expected403/got200, recording only numeric HTTP status. This is a real authorization failure and the assertion has not been relaxed. No supported dual HTTPS acceptance or subsequent lifecycle deletion/self-target claims are counted. Failed reports preserve actual shared cleanup results; helper metrics are absent because the helper raised before returning, and have not been fabricated. Service-side remediation and a new clean server followed by both supported HTTPS runs remain required.
