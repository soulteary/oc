# OC Console release and migration gates

This is current release preparation guidance. It does not publish artifacts,
change a backend deployment, or declare the notification/fallback window complete.
The next release notes use OC `RELEASE.2026-10-10T11-42-18Z` (source
`ee1846d587a6548db4e595d653ca4f54baea2b1a`) as the stable baseline.

## Distribution contract

Timestamp releases package `oc` and `oc-console` in the eleven archives listed
in `compatibility.json`, with `.exe` names on Windows. License/checksum layout
remains; both programs identify the release. Linux amd64/arm64 images contain
both verified programs and default to `oc`; select `--entrypoint oc-console`.
The release workflow checks packaged Linux amd64 version/help and image bytes
against archives. Cross-compilation is not proof of native target execution.
Verify the thirteen published assets and manifest/image identities before rollout.
Older published artifacts remain unchanged.

The existing Release workflow publishes stable even when latest promotion is
disabled. A formal timestamp tag does not merely produce an RC or private
candidate. Follow [release procedure](releasing.md); do not use release dispatch
as a substitute for local/CI candidate preparation.

## Current deployment scope

The current source provides local alias, native IAM and OIDC authentication.
Local alias mode is still a single shared identity. Native users must be enabled
IAM users; root, directory, service-account and STS browser logins are refused.
OIDC uses each user's verified subject and WebIdentity STS policy. Never replace
independent browser users with a shared administrator alias.

Initial independent-user deployment is one instance with direct browser HTTPS,
fixed HTTPS S3/Admin endpoints and independent CA configuration. The IdP uses a
separate HTTPS origin and trust pool. Sessions are in memory; restarts require
fresh login. Preferences persist per identity. Writes and sharing remain opt-ins.
See [Console guide](console.md), [OIDC configuration](console-oidc-login.md) and
[native/OIDC Compose recipes](../deploy/README.console-https.md).

Native IAM user/group/service-account and binding mutations need the exact
relationship/capability protocols and backend permissions. Confirmed/uncertain
mutations retire their captured local identity scope; definitive refusals do not.
Missing protocol support is a refusal or documented read-only fallback, not
permission to use an older unsafe mutation route. OIDC IAM writes and Console
secret rotation remain disabled. Native identity revalidation occurs before
requests, not proactively for idle sessions or continuously for operations
already in flight. Proxy TLS termination, multiple replicas, alternate browser
engines, long-running resource behavior and every native platform are outside
this first candidate's certification unless separately verified.

## Candidate pair and exact evidence

The [candidate guide](console-migration-candidate.md) names the clean source-pair
CI and separate real cross-version upgrade acceptance. Candidate reports record
actual binary SHA-256, embedded source state, platform, SDK and harness hashes.
A report for test-built programs is not release-asset or image certification.
After any source change, rebuild and check the precise programs to be shipped.
CI results from a previous PR/main revision do not certify a new revision.

SDK/Admin module pins in `compatibility.json` are build dependencies, not remote
server identities. Protected settings, historical version authorization and IAM
must be verified against the actual paired server. Published/third-party/older
backends need their own scope, including negative permission cases.

The existing [local bridge recipe](../deploy/compose.console.yaml) retains OC
`RELEASE.2026-10-10T11-42-18Z` and OtterIO `RELEASE.2026-10-09T15-22-07Z`
with recorded image digests. It provides local HTTP and one alias, not new
native/OIDC deployment. Its [published-pair evidence](console-released-pair.md)
and [arm64 deployment evidence](console-next-deployment-release.md) keep their
original limited scope. All historical JSON and failed reports remain unchanged;
do not reinterpret them as proof for the new pair.

## Migration notice and fallback policy

The publicly documented OtterIO policy requires at least two notified release
cycles, a separately announced default-off transition and a bounded fallback
window before deleting old Web code. The recorded first notice does not complete
those gates. The source removal has merged; merging code is not fulfillment of
published notification promises. Before a formal removal release, either satisfy
the remaining sequence or explicitly revise and announce that migration policy.
Do not silently remove the policy or describe unfinished windows as completed.
See the [server migration record](https://github.com/soulteary/otterio/blob/main/docs/web-console-deprecation.md).

On an older server that still includes the UI, `OTTERIO_BROWSER=off` may opt out
while preserving S3/Admin/STS; same-image restoration uses that version's
supported configuration. On the removal source, `OTTERIO_BROWSER=on` rejects
startup. The legacy `--console-address` and `--console-certs-dir` names configure
the Admin listener/certificates; they are not the OC browser listener.

Retain the previous exact images/programs, deployment configuration and backup.
Verify at least one declared supported old-release-to-new-pair upgrade and an
actual rollback, preserving object bytes and configuration. Restarting the same
version is not a cross-version upgrade. OC local preferences may migrate in
local mode; independent native/OIDC users must not inherit shared alias data.
Returning to old UI/programs does not undo writes, IAM changes, secret rotation
or lifecycle transitions. Never assume a server downgrade understands new data
or references without explicit compatibility validation.

## Formal release decision

1. Correct release notes and declared supported scope, then pass required CI on
   clean synchronized main at the exact commit.
2. Verify candidate-pair protocols and the promised upgrade/rollback path;
   preserve genuine failed runs rather than accepting partial/retried success.
3. Publish the compatible OC release first and verify its actual archives,
   checksums and immutable image contents.
4. Validate the final OC/OtterIO program/image pair for the declared identity,
   endpoint, CA and permission modes. Source acceptance does not certify assets.
5. Satisfy or explicitly revise the public notification/transition/fallback policy.
6. Publish OtterIO only after those decisions and formal artifact verification.

The initial supported scope can be limited. Do not claim all gateways, proxies,
federated/distributed topologies or platforms from one local run. S3, Admin, STS,
health, metrics, node communication and shared authentication remain server
features; terminal console printing and Admin log streams are separate from the
removed embedded Web.

## 中文要点

以 OC 2026-10-10 稳定版为基线准备新发行，不预设新镜像或宣布迁移窗口完成。
新的单实例配方由 OC 直接处理 HTTPS，native/OIDC 分别使用每个真实用户的
IAM/STS 权限，保留固定后端、独立 CA、身份偏好及重启后重新登录的规则。
local alias 配方和历史报告保留各自的旧版本范围，不能当作新组合认证。

候选源码配对与跨版本升级需要记录实际程序、源码和校验值；正式附件及镜像还
需发行时核验。新提交必须重新检查，不能沿用旧 CI。公开的两次通知、默认关闭
过渡和回退窗口仍需完成或明确公告修订；删除版设置 `OTTERIO_BROWSER=on`
会失败，回退旧服务端需要实际数据/配置兼容验证。现有 Release 流程发布 stable，
关闭 latest promotion 也不是 RC。
