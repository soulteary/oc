# Isolated native relationship IAM acceptance

Date: 2026-10-12 (Asia/Shanghai). The fixture uses disposable local storage, real CLI bootstrap, verified HTTPS and actual native Console sessions. It never adds root credentials to a browser session, mutates existing users or claims that unsupported service/STS browser sessions were tested.

The [known-denial-fixed positive results](console-relationship-supported-results.json) pass 22 acceptance groups in each of single HTTPS and independent S3/Admin HTTPS/CA topologies. The [same-runtime legacy capability results](console-relationship-legacy-fixed-results.json) pass 12 groups per topology. These runs use clean Console `52310833272b25c9ef146562eb8470d4e2dafb1a`, CLI `2a5a0f2152190ae438583a219f05cac84ddd3a9b`, final positive server `ff6e66f3fbd7e57198ef5df016fdd79e5545fc7b` and negative legacy server `cbbffbd40531f101cb67c263c69010a0a1b8ed4e`. All execute `darwin/arm64`. [Independent program identities](console-relationship-program-identities.json) verify declared full commits against embedded VCS, require `modified=false` and bind actual hashes; report filenames are not source evidence.

The successful positive runs cover administrator secret rotation of self/other users, disabled users remaining disabled after secret replacement, ordinary/explicit-deny/readonly/CSRF negatives, stale user/group binding revisions preserving the newer state and sessions, fresh bindings and group membership/status changes retiring only affected native users, and real child signed-S3 disable/enable/rotate/policy/delete semantics. Service policy edits are accepted for the authoritative parent and rejected for a general manager.

These reports predate explicit `processCleanup` instrumentation in the shared wrapper. The cleanup finally paths returned without secondary errors, but these reports do not assert a recorded cleanup status. The later browser callback landing-page change is a separate runtime and is not certified by these native reports.

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
