# Console replacement evidence gate

The capability matrix enumerates all 19 old `webAPIHandlers` methods and nine
cross-cutting requirements. All entries remain unverified for this iteration.
A different protocol can replace an old behavior without reproducing insecure
credential handling or granting an ordinary user administrator permissions.
ServerInfo, StorageInfo and prefix policy summary changes require an explicit
scope decision. No CLI exemption or reduction is approved by this matrix.

Run the structural gate with `python3 buildscripts/console_parity_gate.py
--matrix docs/console-capability-matrix.json --report REPORT.json
--server-source EXACT_RELEASE_SOURCE --server-sha256 EXACT_RELEASE_BINARY_HASH`.
Arguments refer to the independently selected server release; mismatched evidence
fails. Output is machine-readable and exit 1 means retirement is blocked.

The supported existing producer is `test-real-protocol-ci.py`, schemaVersion 1.
It records named checks, source state, binary digests and harness digests. Its
range, diagnostics and oidc-sts checks can be inspected without rewriting their
historic reports. They do not by themselves certify browser OIDC or all matrix
items. Older integration reports with free-form `checks` strings are rejected;
a pass string is insufficient to map a capability or prove a required profile ran.
Future producers must emit stable matrix check IDs and profile names using this
structural schema. A failed or skipped check/profile, missing profile, duplicate
check, missing digest, dirty source, wrong version, or non-release fixture blocks
retirement. Tests exercise these negative cases.

This is a necessary evidence check, not release authorization. Hashes supplied
by a report are declarations, not signatures. Independent client/harness version
binding, real IdP runs, migration notices and a completed rollback window remain
coordinator/release requirements. Until those are verified, leave old Web enabled.
Do not infer success from previous reports, retry a Range failure to conceal it,
or reuse a version-enabled bucket for a null-version fixture.
