#!/usr/bin/env bash
# Check the published immutable index and smoke-test its Linux amd64 image.
set -euo pipefail

if [ "$#" -lt 3 ] || [ "$#" -gt 5 ]; then
  echo 'Usage: verify-release-image.sh IMAGE@sha256:DIGEST RELEASE_TAG SOURCE_SHA [SOURCE_URL [IMAGE_CONTEXT]]' >&2
  exit 1
fi
image="$1"
tag="$2"
source_sha="$3"
source_url="${4:-https://github.com/soulteary/oc}"
context="${5:-${OC_IMAGE_CONTEXT:-}}"
if [[ ! "$image" =~ ^[^[:space:]@]+@sha256:[a-f0-9]{64}$ ]] || [[ ! "$source_sha" =~ ^[a-f0-9]{40}$ ]]; then
  echo 'Require an immutable image digest and a full source commit SHA.' >&2
  exit 1
fi
python3 - "$tag" <<'PY'
import datetime
import re
import sys
tag = sys.argv[1]
if not re.fullmatch(r'RELEASE\.[0-9]{4}-[0-9]{2}-[0-9]{2}T[0-9]{2}-[0-9]{2}-[0-9]{2}Z', tag):
    raise SystemExit('Invalid timestamp release tag')
datetime.datetime.strptime(tag, 'RELEASE.%Y-%m-%dT%H-%M-%SZ')
PY
if [ -z "$context" ] || [ ! -f "$context/dist/oc-linux-amd64" ]; then
  echo 'Supply the verified image context as the fifth argument or OC_IMAGE_CONTEXT.' >&2
  exit 1
fi
for license in LICENSE NOTICE CREDITS notify-LICENSE; do
  test -s "$context/licenses/$license"
done

temporary="$(mktemp -d)"
container=''
cleanup() {
  if [ -n "$container" ]; then docker rm -f "$container" >/dev/null 2>&1 || true; fi
  rm -rf "$temporary"
}
trap cleanup EXIT

docker buildx imagetools inspect "$image" --raw > "$temporary/index.json"
python3 - "$temporary/index.json" <<'PY'
import json
import sys
with open(sys.argv[1], encoding='utf-8') as data:
    index = json.load(data)
platforms = []
for manifest in index.get('manifests', []):
    if manifest.get('annotations', {}).get('vnd.docker.reference.type') == 'attestation-manifest':
        continue
    platform = manifest.get('platform', {})
    platforms.append((platform.get('os'), platform.get('architecture')))
if sorted(platforms) != [('linux', 'amd64'), ('linux', 'arm64')]:
    raise SystemExit('Image index must contain exactly linux/amd64 and linux/arm64')
PY
docker pull --platform linux/amd64 "$image"
label() { docker image inspect "$image" --format "{{ index .Config.Labels \"$1\" }}"; }
test "$(label org.opencontainers.image.title)" = OC
test "$(label org.opencontainers.image.source)" = "$source_url"
test "$(label org.opencontainers.image.version)" = "$tag"
test "$(label org.opencontainers.image.revision)" = "$source_sha"
test "$(label org.opencontainers.image.licenses)" = 'Apache-2.0 AND MIT'
test "$(docker image inspect "$image" --format '{{json .Config.Entrypoint}}')" = '["oc"]'
version="$(docker run --rm --platform linux/amd64 "$image" --version)"
test "$version" = "oc version $tag"
docker run --rm --platform linux/amd64 "$image" --help >/dev/null
test "$(docker run --rm --platform linux/amd64 --entrypoint oc-console "$image" --version)" = "oc-console $tag"
docker run --rm --platform linux/amd64 --entrypoint oc-console "$image" --help >/dev/null

container="$(docker create --platform linux/amd64 "$image")"
docker cp "$container:/usr/bin/oc" "$temporary/oc"
cmp "$temporary/oc" "$context/dist/oc-linux-amd64"
docker cp "$container:/usr/bin/oc-console" "$temporary/oc-console"
cmp "$temporary/oc-console" "$context/dist/oc-console-linux-amd64"
for license in LICENSE NOTICE CREDITS notify-LICENSE; do
  docker cp "$container:/licenses/$license" "$temporary/$license"
  cmp "$temporary/$license" "$context/licenses/$license"
done
docker cp -L "$container:/etc/pki/tls/certs/ca-bundle.crt" "$temporary/ca-bundle.crt"
test -s "$temporary/ca-bundle.crt"

mkdir "$temporary/data"
printf 'OC release image local filesystem smoke test\n' > "$temporary/data/input"
docker run --rm --platform linux/amd64 \
  --mount "type=bind,src=$temporary/data,dst=/smoke" \
  "$image" --config-dir /oc-config cp /smoke/input /smoke/output
cmp "$temporary/data/input" "$temporary/data/output"
echo "Verified $image for $tag ($source_sha)"
