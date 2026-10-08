# Try OC with local OtterIO

[Documentation index](README.md) · [简体中文](zh_CN/quickstart.md)

This example runs a single-node test server in Docker and uses a native OC client to create a bucket, upload and download a file, and query server information. You need [OC installed](installation.md), Docker, OpenSSL, `curl` and a POSIX shell. Ports 9000 and 9001 must be available. OC is the client; installing it does not start an OtterIO server.

For an existing deployment, skip server startup and use its addresses and credentials in [configuration](configuration.md). The server's [Quick Start](https://github.com/soulteary/otterio#quick-start) and [Docker security guide](https://github.com/soulteary/otterio/blob/main/README_DOCKER_SECURITY.md) cover server setup in more detail.

## Start the test server

Run these commands in one shell. Generate the credentials once, save them securely, and retain the same values for connecting or restarting:

```sh
export OTTERIO_ROOT_USER=otterio-admin
OTTERIO_ROOT_PASSWORD="$(openssl rand -hex 32)"
export OTTERIO_ROOT_PASSWORD

docker run --detach --name otterio-oc-example \
  -p 127.0.0.1:9000:9000 -p 127.0.0.1:9001:9001 \
  -e OTTERIO_ROOT_USER -e OTTERIO_ROOT_PASSWORD \
  -v otterio-oc-example-data:/data \
  soulteary/otterio:latest server --console-address ":9001" /data
docker logs otterio-oc-example
curl --fail http://127.0.0.1:9000/otterio/health/ready
```

Wait for startup before the readiness request; if it fails, inspect the logs and retry when the server is ready. These HTTP listeners are bound to the host's loopback interface for local evaluation. The named Docker volume retains data after the container stops. `latest` is a moving server image tag; for repeatable deployment, choose a reviewed release tag or digest. This example does not extend OC's [recorded compatibility scope](compatibility.md) to every version of that image.

The S3 API uses port 9000. The Web console and management API use port 9001. The management root URL is `http://127.0.0.1:9001`, with no `/otterio/` console path. For a single-listener server, omit both the server's `--console-address` and the client's `--admin-url`.

## Configure OC and verify a transfer

Keep using the same shell so OC receives the credentials passed to the server. Use a separate client configuration directory for this example:

```sh
mkdir -p ./oc-example-config
chmod 700 ./oc-example-config
export OC_CONFIG_DIR="$PWD/oc-example-config"

oc --version
oc alias set store http://127.0.0.1:9000 \
  "$OTTERIO_ROOT_USER" "$OTTERIO_ROOT_PASSWORD" \
  --api s3v4 --path on --admin-url http://127.0.0.1:9001
oc ls store
oc mb store/oc-example
printf 'Hello from OC\n' > hello.txt
oc cp hello.txt store/oc-example/hello.txt
oc stat store/oc-example/hello.txt
oc cp store/oc-example/hello.txt downloaded.txt
cmp hello.txt downloaded.txt
oc admin info store
```

`cmp` exits successfully when the downloaded bytes match. Successful `alias set` only saves configuration with these explicit signature settings; the following S3 and management commands verify access. Re-running `mb` for an existing bucket can report that the bucket already exists.

The same-shell alias command keeps literal secrets out of shell history, but expanded credentials can still be visible in process arguments. On a shared machine, omit the two credential arguments and enter the saved credentials at OC's prompts. `config.json` contains credentials in either case; keep the example configuration private. Creating a client alias does not create a server account. Use a restricted identity for normal application work rather than distributing this test's root credentials.

## Stop the example and continue

Stop the test server and clear the example's shell overrides:

```sh
docker stop otterio-oc-example
unset OC_CONFIG_DIR OTTERIO_ROOT_USER OTTERIO_ROOT_PASSWORD
```

The container, named volume, local files and example configuration remain. Preserve the saved credentials if you intend to restart with `docker start otterio-oc-example`; generating a new shell password does not change the existing container's credentials. Retain or remove these example resources according to whether you still need their data.

Continue with [copying and mirroring](usage.md), [TLS and endpoint configuration](configuration.md), or [OtterIO administration](administration.md). To call S3 from a Go application, use [OtterIO SDK](https://github.com/soulteary/otterio-sdk); the SDK, OC and server have separate release versions.
