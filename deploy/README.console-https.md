# Independent-user Console over direct HTTPS

Use either [native IAM](compose.console.native.yaml) or
[OIDC](compose.console.oidc.yaml) with one OC instance and an existing compatible
OtterIO deployment. These recipes start only `oc-console`; they do not initialize
an administrator alias, change backend credentials, replace server data or select
a new OtterIO image. The old [local bridge recipe](compose.console.yaml) retains
its pinned published pair and one shared alias identity.

## Select exact programs first

Read the [candidate identity and acceptance requirements](../docs/console-migration-candidate.md).
Set `OC_CANDIDATE_IMAGE` to an image reference including its `@sha256:` digest,
verified against the candidate or final release manifest. Confirm both executable
hashes, source revision, version and architecture; a digest pins bytes but does
not establish compatibility. If the candidate report names only test-built
programs and has no OC/OtterIO image identity, build and verify a matching image separately
before using this recipe. No new published image or digest is asserted here.

The recipes are templates, not a recorded container or platform acceptance run.
First-rollout scope is a single instance with TLS handled directly by OC.
Proxy TLS termination, forwarded-header trust, multiple replicas, alternate
browser engines and native execution on every archive platform need separate
verification before being promised.

## Preserve endpoints and trust

Use the deployment's actual HTTPS S3 and Admin root URLs. OtterIO's legacy
`--console-address` selects the Admin listener, not a browser page;
`--console-certs-dir` selects that listener's certificate directory. Do not
replace existing server ports or proxy mappings with the example browser port.
OC's `9090` listener is independent. If S3 and Admin share one HTTPS endpoint,
set both URL variables to that same root URL. With different CAs, keep the two
PEM trust files separate. These files add private trust to system roots; they do
not disable certificate verification. URLs must be reachable from the container
and match certificate hostnames; container `localhost` is not the host backend.

Place the Compose file and a private `.env` in a dedicated deployment directory.
Supply paths to existing PEM files and a browser certificate matching the exact
`OC_PUBLIC_URL` hostname. The browser must trust that certificate as well.
The following values are placeholders to replace, not released artifact IDs:

```dotenv
OC_CANDIDATE_IMAGE=ghcr.io/soulteary/oc@sha256:<verified-manifest-digest>
OC_PUBLIC_URL=https://console.example.com:9090
OC_BROWSER_BIND=127.0.0.1
OC_BROWSER_PORT=9090
OC_S3_URL=https://s3.example.com
OC_ADMIN_URL=https://admin.example.com
OC_S3_CA_FILE=/absolute/path/s3-ca.pem
OC_ADMIN_CA_FILE=/absolute/path/admin-ca.pem
OC_BROWSER_CERT_FILE=/absolute/path/console.crt
OC_BROWSER_KEY_FILE=/absolute/path/console.key
```

The default host binding is loopback. Choose a specific reachable host address
for remote users and configure network access accordingly. `OC_PUBLIC_URL`
must include the actual browser-facing port and must be used unchanged by users.
The container itself binds `0.0.0.0:9090`; native/OIDC modes do not use the local
mode's `--container-listen` option. Both recipes default to read-only. Add
`--allow-writes` to the command only after validating that user's intended
mutations and required backend capabilities. Sharing also needs
`--allow-sharing` and an explicit HTTPS `--share-url` reachable by recipients.

## Credentials and OIDC

Native users enter their own enabled IAM key and secret through the HTTPS login
form. Root, service-account, STS and directory identities cannot use native
browser login. No user secret or administrator credential belongs in `.env`.

For the OIDC recipe, add these non-secret values and file paths:

```dotenv
OC_OIDC_ISSUER=https://idp.example.com/realms/storage
OC_OIDC_CLIENT_ID=oc-console
OC_OIDC_CA_FILE=/absolute/path/idp-ca.pem
OC_OIDC_CLIENT_SECRET_FILE=/absolute/path/oc-client.secret
OC_SESSION_TTL=30m
```

Register exactly `OC_PUBLIC_URL/api/oidc/callback`. Configure the compatible
server's OpenID/STS policy mapping and the IdP's supported Authorization Code,
S256 PKCE, RS256 and `client_secret_basic` profile. The IdP must have a different
HTTPS origin from both S3 and Admin. OC requests `openid offline_access`; verify
refresh permission and synchronized clocks. See [OIDC configuration](../docs/console-oidc-login.md).
Each browser gets its own STS credentials and server permissions; OIDC IAM
mutations and Console secret rotation remain disabled.

Use a private regular secret file containing one nonempty line, no more than
4096 bytes. Set its host permissions to `0600` or `0400`, along with the browser
private key. File-backed Compose secrets can retain host permissions instead of
applying the requested `mode`; verify the mounted file is private. OC rejects
an OIDC client secret accessible to group/others. Do not paste secrets into
Compose, command arguments or environment variables. The confidential-client
permission check requires POSIX semantics; this recipe does not certify Windows
execution. For a public OIDC client, remove the secret flag and corresponding
service/top-level secret, and use a provider advertising token authentication
method `none`; validate that profile separately.

## Start, persistence and rollback

From that deployment directory, select one file and keep a stable project name:

```sh
docker compose -p oc-console -f compose.console.native.yaml config --quiet
docker compose -p oc-console -f compose.console.native.yaml pull
docker compose -p oc-console -f compose.console.native.yaml up -d
```

Use `compose.console.oidc.yaml` in all three commands for OIDC. Confirm the exact
program/image identities, successful independent-user login and a denied
operation using an identity that lacks permission. Then verify object bytes,
required IAM/settings/version protocols and preference persistence on restart.
A started container alone is not feature acceptance.

`oc-console-data` preserves language, favorites and recent visits per identity;
sessions and storage credentials stay in process memory and restart requires
fresh login. `oc-console-archives` is private working storage for complete ZIP
files, not a backup. Normal cleanup removes archives, but forced termination can
leave `0600` files requiring controlled cleanup. Keep both volumes private and
do not use `down -v` when retaining preferences. Existing backend data and
configuration stay in their existing deployment.

Retain the previous OC image, Compose configuration and backend configuration.
Returning to an earlier Console cannot undo object writes, IAM changes or
lifecycle transitions. A server without embedded Web cannot restore it by setting
`OTTERIO_BROWSER=on`: that setting fails startup. Restoring an older server needs
a separately verified data/configuration upgrade and rollback path. The public
notification/fallback policy is still a release gate, not completed by this recipe.

## 中文说明

选择 native 或 OIDC 的单实例配方，连接现有 HTTPS S3/Admin，不创建管理员
alias、不更改服务端数据或凭据。镜像必须使用候选或正式 manifest 中核验过的
摘要；仅有开发程序验收、没有 OC/OtterIO 镜像身份时，还需单独构建和核验对应镜像。
这里不预设新的已发布版本，也不把配方解析当成容器运行通过。

保留现有实际端点、端口、代理映射及独立 S3/Admin CA。`--console-address`
是服务端 Admin 入口，网页 HTTPS 由 OC 单独监听；两个接口共用同一 HTTPS
端点时，将两个 URL 设置成同一根地址。浏览器地址、证书主机名和端口必须对应，
所有用户使用同一个精确的 `OC_PUBLIC_URL`。默认宿主机仅绑定回环地址，远程
用户需要显式选择可达监听地址。首批范围为 OC 直接处理 TLS 的单实例，不承诺
代理 TLS 终止、多副本或所有平台原生运行。

native 用户在网页提交自己的 IAM 凭据，不共享管理员 alias。OIDC 用户通过
身份提供方得到自己的 STS 权限；登记精确的 `/api/oidc/callback`，保留 IdP、
S3、Admin 的独立 CA，配置 refresh 权限及服务端策略。浏览器私钥和 OIDC
client secret 使用私有文件，不写入 `.env` 或命令。secret 必须为至多 4096
字节的单行普通文件，组和其他用户不可读；Compose 文件挂载可能保留宿主机
权限，应检查容器内权限。两种配方默认只读，写入和分享需单独显式开启。

保持项目名与数据卷，重启后偏好保留，会话需重新登录。ZIP 工作卷不是备份，
异常退出后可能需要清理残留私有文件。回退界面不会撤销写入、改密和生命周期
转换；删除版服务端不能靠 `OTTERIO_BROWSER=on` 恢复旧网页。跨版本服务端
回退及已公布的通知窗口仍需独立验证和完成。
