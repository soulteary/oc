# Run OC in a container

[Documentation index](README.md) · [简体中文](zh_CN/containers.md)

The release workflow builds Linux amd64 and arm64 images from the same executables as the release archives. GHCR is the primary registry; Docker Hub is optional. Container publication was added after the binary-only `RELEASE.2026-10-07T14-10-00Z` release. That older release has no `images` entry in its manifest and must not be used as an image availability example.

## Select an available image

Choose a [published release](https://github.com/soulteary/oc/releases) whose `release-manifest.json` contains `images`. Each entry gives the repository and verified index digest. If the release has no image entries, install its [native archive](installation.md) or build a local image from source; do not infer an image from the archive tag.

The following commands use a placeholder that must be replaced with an image-enabled release:

```sh
TAG=RELEASE.YYYY-MM-DDTHH-MM-SSZ
IMAGE="ghcr.io/soulteary/oc:$TAG"
docker run --rm "$IMAGE" --version
docker run --rm "$IMAGE" --help
```

For repeatable automation, combine an image entry's `repository` and `digest` fields from the release manifest as `repository@digest`, and set `IMAGE` to that reference (for example, `ghcr.io/soulteary/oc@sha256:...`). `latest` follows the newest stable release that completed digest promotion; it is a moving alias and can be absent before the first successful promotion. First-time GHCR package publication requires appropriate visibility or authenticated pulls; see [release setup](releasing.md#container-images).

Docker Hub uses `DOCKERHUB_USERNAME/oc` only when both publishing secrets were set for that release. Use the manifest's repository list rather than assuming a Docker Hub mirror exists.

## Persist client configuration

The image entrypoint is `oc`; put client arguments after the image name. There is no server process or port to publish from an OC container.

Create a private host directory for aliases, certificates and saved client state. The release image runs as root by default and uses `/root/.oc`:

```sh
mkdir -p "$HOME/.oc"
chmod 700 "$HOME/.oc"
docker run --rm -it \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  "$IMAGE" alias set store https://s3.example.com \
  --api s3v4 --path on --admin-url https://admin.example.com

docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  "$IMAGE" ls store
```

Replace both example endpoints and enter your deployment's credentials at the prompt. Files created by the default root user can be owned by root on Linux hosts. If sharing with a native OC installation, verify ownership/permissions instead of making the configuration directory public.

For a non-root Linux container, provide a writable explicit directory and choose the matching host UID/GID:

```sh
mkdir -p ./oc-config
chmod 700 ./oc-config
docker run --rm --user "$(id -u):$(id -g)" \
  --mount "type=bind,src=$(pwd)/oc-config,dst=/oc-config" \
  "$IMAGE" --config-dir /oc-config --json doctor
```

Use that same `--config-dir /oc-config` and mount for subsequent commands. Rootless operation does not automatically make an existing root-owned configuration writable.

## Mount data and certificates

Host paths become visible only through mounts. To upload a file from the current directory, mount it read-only:

```sh
docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  --mount "type=bind,src=$(pwd),dst=/work,readonly" \
  "$IMAGE" cp /work/hello.txt store/example/hello.txt
```

For downloads, use a writable destination mount:

```sh
mkdir -p ./downloads
docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  --mount "type=bind,src=$(pwd)/downloads,dst=/downloads" \
  "$IMAGE" cp store/example/hello.txt /downloads/hello.txt
```

S3 CA files belong under the mounted configuration's `certs/CAs/`. A saved absolute `adminCAFile` path is interpreted inside the container: mount the certificate at that same container path or override `--admin-ca` with a mounted path. Importing a host configuration does not make arbitrary host paths visible. See [TLS configuration](configuration.md).

## Reach the server

`127.0.0.1` inside the container refers to the container itself. An alias created for a host-native OC invocation may therefore need a different endpoint in Docker.

- For servers in another container, put both on a user-defined Docker network and use the server's service/container DNS name and internal S3/admin ports.
- Docker Desktop normally provides `host.docker.internal` for host services. Ensure the server listens on an address reachable from the container.
- On Linux Docker Engine, supported installations can add `--add-host=host.docker.internal:host-gateway` and use that hostname. This mapping does not make a server bound only to the host's loopback address reachable.
- Host networking is another Linux option, but changes isolation and is platform-dependent. It is not necessary when the server has an address reachable on the container network.

Use an HTTPS hostname covered by the certificate. A host-networking workaround does not change TLS hostname validation. For two-port OtterIO, configure both S3 and management endpoints; [administration](administration.md) explains their roles.

Docker's [networking overview](https://docs.docker.com/engine/network/) and [`--add-host` reference](https://docs.docker.com/reference/cli/docker/container/run/#add-host) describe network names and host gateway mappings.

## Automate without a terminal

Use `-it` for interactive setup only. For scripts, run without a terminal, check the exit status and use the command's JSON schema where needed:

```sh
docker run --rm \
  --mount "type=bind,src=$HOME/.oc,dst=/root/.oc" \
  "$IMAGE" --json doctor store
```

Do not pass a full secret-bearing configuration in an issue or bake credentials into an image layer. Runtime `OC_*` environment variables are supported, but administrators who can inspect the container can also inspect its environment. [Security](../SECURITY.md) covers sensitive outputs and credential handling.

## Build a local image

From a Git checkout of OC, with Docker Buildx available:

```sh
docker buildx build --platform linux/amd64 --load -t oc:local .
docker run --rm oc:local --help
```

Use `linux/arm64` on an ARM64 machine. `--load` here selects one platform for local use. This source build uses development version metadata; setting an image tag does not turn it into a verified release.

`docker-buildx.sh` defaults to building amd64/arm64 and only publishes when explicitly given `--push`. `Dockerfile.release` expects the prepared archive-based context generated by the release workflow; it is not the standalone source-build recipe. Publication, immutable tags and failure recovery belong in [the release guide](releasing.md).
