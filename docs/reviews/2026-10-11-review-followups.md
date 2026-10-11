# Merged PR review follow-ups

Rechecked the nine concrete review suggestions on PRs #1–#30 against main
`f71de60f9ae1a47a950edc6c3686c731c3b49534`. The original comments are linked
below. Review-thread resolution state is not used to decide whether the code
is fixed.

## Already addressed

- [#8 dependency/manifest mismatch](https://github.com/soulteary/oc/pull/8#discussion_r4214193643): the current dependency and compatibility identities match. Release-boundary regression checks pass.
- [#9 SDK source identity](https://github.com/soulteary/oc/pull/9#discussion_r4214756285): an independent reviewed tag/source mapping rejects a full-length but wrong source SHA. The corresponding regression still passes.
- [#20 release warning](https://github.com/soulteary/oc/pull/20#discussion_r4237270639): the published-release preparation notes retain the bilingual warning and workaround. Those historical notes remain unchanged.

## Problems still present and corrected here

- The defect behind #20 still checked a stale client-initialization error after `updateCannedPolicies`. It now stops on the actual policy error before `SetPolicy`. CLI regressions cover duplicate, empty, whitespace-only and partly duplicate input for both users and groups, plus a successful append preserving the existing assignment. The administration guides distinguish the fixed source from earlier published binaries.
- [#29 creation permissions](https://github.com/soulteary/oc/pull/29#discussion_r4239880207): conditional user/group creation no longer performs `ListUsers`/`ListGroups` preflights. The server still authorizes creation and checks absence atomically; there is no legacy upsert fallback. Regressions cover denied list permissions, denied creation, existing-target conflicts, missing protocol, secret handling and local caller-group retirement.
- [#17 current server coverage](https://github.com/soulteary/oc/pull/17#discussion_r4230231343): separate current-pin CI profiles exercise protected settings/own-secret rotation and native storage features/version copy. The feature profile explicitly requires missing IAM create-only support to refuse user/group creation without creating a target. Other feature failures remain failures. Full IAM mutation/conditional-binding acceptance is recorded as unexercised on this pin; the complete patched fixture remains independent.
- [#22 route-removal evidence](https://github.com/soulteary/oc/pull/22#discussion_r4238399530): Compose probes inspect response bodies using the shared native-acceptance rule. Plain or JSON authentication errors and HTTP 405 cannot prove a disabled route. HTTP 404 or a 400/403 storage XML error is required, with a successful probe process.
- [#22 platform evidence](https://github.com/soulteary/oc/pull/22#discussion_r4238399537): each running container's actual image provides OS/architecture/variant. The report records host and Docker operating system separately and no longer labels every execution as Docker Desktop arm64. `--output` permits a new evidence file without overwriting historical results.
- [#25 port zero](https://github.com/soulteary/oc/pull/25#discussion_r4239065026): native listen ports must be 1–65535. IPv4/IPv6 zero ports and other invalid bounds are rejected before listening.
- [#26 write help](https://github.com/soulteary/oc/pull/26#discussion_r4239160544): `--allow-writes` now describes the local/native identity-operation distinction and the current native creation, activation and own-secret rotation scope.

## Validation and limits

The policy CLI, creation-permission and port-zero regressions were first run
against the original main behavior and failed, then passed after correction.
Full local `go test -race ./...`, `go vet ./...` and the configured linter pass.
The frontend's 80 behavior groups, 19 Console helper tests, 65 release helper
tests, workflow actionlint and whitespace checks pass.

[Local acceptance evidence](../console-review-followups-results.json) records
the final tested binary hashes and actual harness hashes. With the current
unpatched server module `v0.0.0-20261009091443-bcc238bc4c2c`:

- `single-http`, `dual-http` and `dual-tls` each pass 21 protected-settings groups.
- The same three profiles each pass 19 storage-feature/version-copy groups, including explicit missing-IAM-create refusal.
- The published-digest Compose pair passes login, persistence, browser-off route checks, same-volume rollback and return to browser-off. Actual runtime is Fedora with Linux arm64 containers, on a macOS arm64 host.

The server binary is built from the recorded Go module archive and lacks VCS
settings; the report labels its source as declared module source, not a verified
binary VCS revision. This does not establish arbitrary release compatibility,
native execution on all platforms or full IAM support on the current pin.
Platform CI runs these profiles independently with cgo on and off. Historical
release notes, prior acceptance evidence and dependency pins are preserved.
