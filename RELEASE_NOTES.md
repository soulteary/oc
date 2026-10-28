# Coordinated Console release acceptance update

This release follows [RELEASE.2026-10-22T16-25-21Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-22T16-25-21Z) and provides the packaged OC partner for the coordinated OtterIO OIDC validation hardening release.

## Changes

- Verify cross-repository draft visibility before official packaged-pair acceptance and document the required repository selection, Contents read, Actions read and draft access.
- Run platform images through child manifests pinned by the accepted image index. Acceptance continues to bind the actual archive programs and immutable image identities.
- Wait for a write-ready cluster before creating the Range test fixture, avoiding fixture setup against a server that has not finished initialization.

These changes improve release acceptance and test setup; they do not add new Console login modes or change the CLI protocol. The OtterIO server update preserves its OIDC signing-algorithm allowlist after JWKS refresh. OC continues to use its existing supported OIDC flow.

## Compatibility and release verification

The independently deployed Console, supported HTTPS/native/OIDC profiles and existing migration/rollback boundaries remain as documented in the [Console guide](docs/console.md) and [packaged-pair acceptance guide](docs/console-release-acceptance.md). Keep programs/images pinned, back up configuration, and validate the actual deployment before rollout.

Publication requires a successful official acceptance run for this exact OC package and its selected OtterIO partner. That run binds release notes, archives, extracted programs, manifests and immutable images. These source notes do not certify future artifacts or expand the runtime/platform scope of the eventual acceptance report.

---

# Console 配套发布验收更新

本次更新接续 [RELEASE.2026-10-22T16-25-21Z](https://github.com/soulteary/oc/releases/tag/RELEASE.2026-10-22T16-25-21Z)，为 OtterIO OIDC 验签重试加固版本提供配套发行包。

- 在官方配对验收前检查跨仓库草稿可见性，明确凭据的仓库选择、Contents/Actions 读取权限及草稿访问要求。
- 按已验收镜像索引固定的子清单执行各平台镜像，继续绑定实际归档程序与不可变镜像身份。
- Range 测试创建 fixture 前等待集群可写，避免初始化未完成导致的测试失败。

本次变化集中在发行验收及测试准备，不增加新的 Console 登录模式，不改变 CLI 协议。现有独立部署、HTTPS/native/OIDC 支持范围与迁移回退边界继续适用。正式发布必须通过实际 OC/OtterIO 发行产物的官方配对验收；本文不将源码检查等同于产物认证。
