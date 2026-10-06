#!/usr/bin/env bash
set -euo pipefail

# Build locally by default. Publishing requires an explicit --push argument.
image=${OC_IMAGE:-soulteary/oc}
release=${OC_TAG:-$(git describe --tags --always)}
docker buildx build --platform="${OC_PLATFORMS:-linux/amd64,linux/arm64}" \
  -t "${image}:${release}" -f Dockerfile "$@" .
