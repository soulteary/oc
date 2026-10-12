# Console migration candidate identity and verification

This prepares an installable independent-user Console and OtterIO Web removal
pair. It does not declare a new published release or complete the public
notification/transition/fallback policy. The stable baselines are OC
`RELEASE.2026-10-10T11-42-18Z` (`ee1846d587a6548db4e595d653ca4f54baea2b1a`)
and OtterIO `RELEASE.2026-10-10T16-38-05Z`
(`8cc518b31ac8b69c8f855e19b4bd027f44275c47`).

Read the [28-capability scope mapping](console-migration-capabilities.md) and
[Chinese verification record](console-migration-verification.md) for the current
replacement decisions and the limits of actual recorded runs. These documents
do not change the historical capability matrix's release-gate status.

## Clean source-pair CI

The [migration workflow](../.github/workflows/console-migration.yml) builds this
OC checkout and the exact declared OtterIO source, then runs
[the pair runner](../buildscripts/test-console-migration-pair.py). The workflow
records its selected sources rather than following a moving remote branch.
Reproduce with clean binaries containing the declared embedded VCS revisions:

```sh
python3 buildscripts/test-console-migration-pair.py \
  --cli /absolute/path/oc --console /absolute/path/oc-console \
  --server /absolute/path/otterio \
  --oc-source FULL_OC_COMMIT --server-source FULL_OTTERIO_COMMIT \
  --output /private/tmp/console-migration-pair.json
```

`console-migration-pair.json` is a candidate source manifest/report:
`schemaVersion: 1`, `evidenceKind: candidate-source-pair` and
`releaseAssetCertification: false`. It binds each program's actual binary
SHA-256, embedded source revision/state, executed platform and SDK pin; harness
hashes and each check's result/report hash remain separate. Its `passed` result
requires the requested checks, not merely successful builds or skipped opt-in
tests. Child reports retain their own topology and operation scope.

The checks cover object/settings/version/copy behavior across listener
arrangements, native and relationship-conditioned IAM over single/independent
dual HTTPS, Range/diagnostics/STS protocols, and actual isolated Keycloak
Chromium login over both HTTPS arrangements. Read the generated report for the
exact checks executed and failures.

The [recorded Darwin arm64 source pair](evidence/console-migration/source-pair-darwin-arm64.json)
passed all eight required profiles for clean OC source
`26a0fa6c4d2f380ff9eba84c9dcb91a378b73c27` and OtterIO source
`c3b3665154264993fa06a28e95ab25fdd03d7c0c`: 360 functional acceptance groups,
three real protocol checks, 27 OIDC protocol checks and 22 actual Chromium
checks. The original child reports remain alongside the manifest with their
recorded hashes. This local result does not certify a new PR head or Linux CI.

The source report does not bind OC/OtterIO image digests or certify a new release
archive, container image or all supported architectures. Its Keycloak image
identity belongs only to the IdP test fixture. The [HTTPS templates](../deploy/README.console-https.md)
therefore require an independently verified digest matching the selected source
and program checksums. A stable tag or digest alone does not prove native/OIDC
or protected IAM compatibility.

## Real upgrade and rollback

The separate [upgrade runner](../buildscripts/console_upgrade_acceptance.py) starts actual
previous-release OC/Console/OtterIO programs, verifies official manifests,
checksums and the OC archive's extracted executable bytes, then upgrades to the
specified candidate programs and rolls back in disposable storage. It uses
single and independent dual TLS/CA arrangements. Supply all six program paths,
the previous source/release identities, official manifest/checksum files and an
independently trusted previous OtterIO manifest SHA-256; run its `--help` for the
exact arguments.

The Oct 10 OtterIO checksum file covers six executables, not
`release-manifest.json`: that manifest was generated after the binary checksums.
Obtain its expected hash independently from the official release's
[`release-manifest.json` asset 628471077 metadata](https://api.github.com/repos/soulteary/otterio/releases/assets/628471077),
whose `digest` is
`sha256:2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3`.
Supply the 64 hex digits without the `sha256:` prefix; do not derive the trusted
expectation by hashing the same unverified local manifest. This value belongs to
this exact previous release and is not a default for arbitrary releases.

The runner checks the expected digest before parsing manifests or starting any
profile. `previousReleaseManifests.server` records `expectedManifestSHA256` and
`verification: matches-expected-sha256`. This says the supplied bytes match the
trusted expectation; the runner itself does not contact or authenticate the
GitHub API. OC's previous manifest remains bound to its own release checksum.
For the stable baselines above:

```sh
python3 buildscripts/console_upgrade_acceptance.py \
  --previous-server /absolute/path/previous-otterio \
  --previous-cli /absolute/path/previous-oc \
  --previous-console /absolute/path/previous-oc-console \
  --server /absolute/path/candidate-otterio \
  --cli /absolute/path/candidate-oc --console /absolute/path/candidate-oc-console \
  --previous-source 8cc518b31ac8b69c8f855e19b4bd027f44275c47 \
  --server-source FULL_OTTERIO_COMMIT --cli-source FULL_OC_COMMIT \
  --previous-release RELEASE.2026-10-10T16-38-05Z \
  --previous-oc-release RELEASE.2026-10-10T11-42-18Z \
  --previous-manifest /absolute/path/previous-otterio-manifest.json \
  --previous-manifest-sha256 2fe6f658e2c0b757ff44962041ff3575adab1620109b40f848654530697f2ba3 \
  --previous-checksums /absolute/path/previous-otterio-checksums.txt \
  --previous-oc-manifest /absolute/path/previous-oc-manifest.json \
  --previous-oc-checksums /absolute/path/previous-oc-checksums.txt \
  --previous-oc-archive /absolute/path/previous-oc-archive.tar.gz \
  --profiles single-tls dual-tls \
  --output /private/tmp/console-migration-upgrade.json
```

The [recorded Darwin arm64 upgrade](evidence/console-migration/upgrade-darwin-arm64.json)
passed both storage profiles: one HTTPS S3/Admin listener, and separate HTTPS
S3/Admin listeners with independent CAs. It uses the published baselines above,
OC candidate source `26a0fa6c4d2f380ff9eba84c9dcb91a378b73c27` and OtterIO
candidate source `c3b3665154264993fa06a28e95ab25fdd03d7c0c`, on disposable
single-node erasure storage using four temporary storage directories. Its JSON records the exact executable
hashes and harness versions. A new PR head must produce its own evidence.
The manifest-bound rerun began at `2026-10-12T19:38:38.732664+00:00`; its report
SHA-256 is `4d948227dec0576b31af97875e821ddcd650667518ecbf11e510b63af4a03991`.
It records the independently supplied previous manifest expectation and
`matches-expected-sha256`. The [earlier passing report](evidence/console-migration/upgrade-before-manifest-binding.json)
remains unchanged with its original, weaker provenance checks. Fifteen focused
upgrade regression tests pass; the three candidate program hashes and nineteen
source-pair harness hashes are unchanged.

The run preserves object bytes, historical versions, tags, versioning, group
policy allow/deny behavior and CLI configuration. It actually returns to the
previous programs before a new write, after one new object write, and after
restoring the pre-upgrade snapshot. Old and new local Console preferences use
loopback HTTP; new native-user preferences use browser HTTPS and remain isolated
from the shared local preferences across restart. This is not an OIDC
old-to-new upgrade test. Genuine failed runs remain alongside the passing
report; missing/failed reports or same-version restarts do not prove an upgrade.

This evidence applies only to the named programs and declared path. It is not
general server downgrade compatibility, all-platform execution, arbitrary proxy
TLS termination or distributed/federated/gateway support. The candidate server
has no embedded Web; `OTTERIO_BROWSER=on` must fail rather than create a fallback.
Fallback to embedded Web requires the verified previous server, not a flag on
the candidate.

## Final release validation

After clean synchronized main passes the required CI, prepare verified stable
drafts for both repositories. The [official packaged-pair gate](console-release-acceptance.md)
checks the thirteen OC/eight OtterIO assets, checksum/manifest identities,
extracted programs, immutable image contents and actual declared runtime/upgrade
scope. Source and local packaged reports cannot become official acceptance by
renaming files or attaching them to a release.

Before publishing the OtterIO removal release, include the explicit accelerated
notice plus verified older-server/snapshot rollback decision.
The [English announcement](https://github.com/soulteary/otterio/blob/main/docs/releases/console-migration-announcement.en.md)
records that approved decision; its revision becomes effective at the first
approved public Web-free release containing the notice. Manually publish OC first and then OtterIO with
the same successful official main run and independently reviewed attestation
hash. Recheck the unchanged accepted bytes; neither stage rebuilds or invokes
latest promotion. Stable promotion is separately requested after publication.
A stable draft is not an RC, and fixed images may already be public. See
[release and migration gates](console-release-migration.md).

## 中文说明

候选源码报告固定真实程序 SHA-256、嵌入的源码状态、运行平台、SDK 与验收脚本
摘要；它不认证 OC/OtterIO 镜像身份，IdP 的 Keycloak 摘要仅属于测试样本，
`releaseAssetCertification` 明确为 false。已落盘的 Darwin arm64 完整报告
八项通过；新 PR head 的 CI 和新 HTTPS 配方对应程序/镜像仍需独立核验，
不预设新的已发布标签。

旧 OtterIO checksum 仅包含六个程序，没有后生成的 manifest。当前升级 runner
要求 `--previous-manifest-sha256`，期望值须独立取得；2026-10-10 正式资产的
GitHub asset 628471077 的 `digest` 为上述 `sha256:2fe6…f2ba3`，传入完整 64 位 hex，
不能由待验证本地文件给自己生成信任值。准备阶段先比对字节，再解析身份和
启动 profile；报告记录 `expectedManifestSHA256` 与
`verification: matches-expected-sha256`，不声称 runner 内执行了 API 验证。

已落盘的 Darwin arm64 跨版本报告通过单 HTTPS 和独立双 HTTPS/CA 存储入口，
使用两个项目的 2026-10-10 正式程序与报告所列候选程序，真实升级后再回退。
旧、新 local 偏好兼容在回环 HTTP 验证，新 native 偏好隔离和重启持久在
HTTPS 验证；没有测试 OIDC 旧版到新版升级。报告只证明所列程序、源码、脚本
与单节点四存储目录纠删码路径，不是四物理盘认证；新 PR head 和正式附件、
镜像仍需重新验证。已公布通知和
回退窗口必须完成或明确公告修订，不能由代码合并替代。
