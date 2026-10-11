# OIDC browser login

OC Console supports independent identity-provider browser sessions with Authorization Code, S256 PKCE, RS256 ID tokens and OtterIO STS. Select `--auth-mode oidc`, a fixed HTTPS `--oidc-issuer`, `--oidc-client-id`, `--s3-url`, browser `--public-url`, `--tls-cert` and `--tls-key`. Register exactly `PUBLIC_URL/api/oidc/callback` at the identity provider. `--admin-url` can select the independent HTTPS management endpoint.

Use `--oidc-ca`, `--s3-ca` and `--admin-ca` for their respective private certificate authorities. The identity provider and storage must use separate HTTPS origins; their CA trust remains independent. The default system certificate trust applies when no CA file is selected. Neither alias configuration nor bootstrap administrator credentials are used in this mode.

A confidential client uses `--oidc-client-secret-file`: a regular file with no group/other permissions, at most 4096 bytes. Its secret must be a single nonempty line; a trailing newline is accepted. A public client requires discovery metadata advertising the `none` token authentication method; the isolated Keycloak 26.8.0 profile instead uses `client_secret_basic`. Console requests `openid offline_access`. Configure provider refresh permissions accordingly.

`--session-ttl` defaults to 30 minutes and accepts 1 minute through 24 hours. Refresh replaces the owned STS client before its credentials expire, while in-flight requests retain their original client and expiry. Refresh never extends the fixed browser session deadline. Logout cancels all generations and tasks belonging to that browser. Favorites are stored per fixed storage target, issuer and verified subject.

Storage operations remain governed by the user's STS policy. Writes require `--allow-writes`; IAM modifications and Console secret rotation are disabled for OIDC identities. Downloads shared through `--allow-sharing` also require an explicit HTTPS `--share-url`. Synchronize the provider and storage clocks: OtterIO rejects a token issued in the future.

This implementation and its isolated development acceptance do not alone authorize removal of OtterIO's embedded Web Console. The capability matrix, release evidence, deployment migration and rollback gates still apply.
