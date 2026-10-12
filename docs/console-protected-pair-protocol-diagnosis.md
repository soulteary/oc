# Protected-pair protocol failure diagnosis

Initial evidence is preserved in [the structured STS first-failure extraction](evidence/console-protected-pair-sts-first-failure.json) and [the original safe wrapper report](console-protected-pair-protocols-wrapper-failed.json). The STS extraction omits every raw Output field, preserves diagnostic codes/actions, and records the original file SHA256. The latter records range and diagnostics success, then `PermissionError`; it has no traceback or syscall identity. The former records STS HTTP 400 / `InvalidParameterValue` without XML Message. Neither original record establishes a cause.

The test-only observer now classifies XML error messages into fixed reason labels. It never exports the raw message, URL, token, secret, or response body. Production JWT clocks, signature validation and policy enforcement remain unchanged. Invalid-token readiness returning any HTTP 400 is insufficient to prove that IAM policy initialization completed. Policy-not-loaded and token-validation errors remain distinct hypotheses; neither was established by the original evidence.

New isolated current f2bdc234 and older clean ff6e66f runs both passed the real STS test, each with three successful exchanges. These are controlled diagnostic comparisons, not retries replacing the original result. The modified full wrapper also passed range, diagnostics and STS. A separate owned-process-group STS run passed. The first failure was not reproduced; it is not diagnosed or resolved.

The wrapper now records process-group signal phase, errno, owned PID / expected PGID and observed PGID when a PermissionError occurs. It continues to abort on that failure. It does not blanket-ignore PermissionError or claim descendant cleanup on failed signalling. Current runs produced no such error, so the original syscall and ownership cause remain unknown.

The accompanying JSON records fixed binary identities, harness hashes and test outcomes. It is diagnostic evidence only, not release acceptance for the subsequently discovered native STS parent-revocation defect or the long-key service-creation collision defect.
