# Administering OtterIO with OC

[中文](zh_CN/administration.md) · [Documentation](README.md)

`oc admin` uses the OtterIO management API and administrative credentials. It does not provide a generic administration API for every S3 service. Check the [compatibility baseline](compatibility.md) before managing a deployment.

## Configure the object and management endpoints

An alias holds the S3 object endpoint and credentials. An OtterIO deployment can expose management on the same listener or a separate listener. The port numbers below are examples; use the addresses configured on your server.

This command prompts for the access and secret keys:

```sh
oc alias set --api s3v4 --path on \
  --admin-url https://admin.example.com:9001 \
  --admin-ca /path/to/admin-ca.pem \
  store https://s3.example.com:9000
```

Object commands such as `ls`, `cp` and `mirror` use the S3 endpoint. Management commands such as `admin info` use `--admin-url`. Without a management override, OC falls back to the S3 endpoint; that works only when the server exposes management there too. The credentials belong to the alias and must be authorized for the requested management operation.

The management URL must be an HTTP or HTTPS root URL. Embedded credentials, query parameters, fragments and path prefixes are rejected. Configure a reverse proxy to expose OtterIO's management routes directly on that host. OC refuses management redirects rather than forwarding signed requests or encrypted configuration/IAM bodies.

Without an explicit management CA, management uses the shared trust pool loaded from system roots and the configuration directory's `certs/CAs/`. Specifying `--admin-ca` replaces that pool for management with system roots plus the given PEM file, excluding the extra certificates in `certs/CAs/`. It does not change S3 trust. When the object listener also uses a private certificate, place its CA in `certs/CAs/`. Keep certificate verification enabled; see [security](../SECURITY.md).

An alias created with `--admin-ca` stores an absolute CA path. Temporary command-line or environment paths are resolved from the current working directory. Management URL and CA overrides are resolved independently, using the first nonempty value in this order:

1. Explicit `--admin-url` or `--admin-ca`.
2. Alias-specific `OC_ADMIN_URL_store` or `OC_ADMIN_CA_store`.
3. Global `OC_ADMIN_URL` or `OC_ADMIN_CA`.
4. The alias's saved `adminURL` or `adminCAFile`.
5. For the URL only, the alias's S3 URL.

The alias suffix is case-sensitive. Temporary overrides do not rewrite saved configuration:

```sh
oc --admin-url https://admin.example.com:9001 \
  --admin-ca /path/to/admin-ca.pem admin info store
```

See [configuration](configuration.md) for object endpoint environment variables and [migration](migration.md) for importing an existing configuration.

## Inspect a deployment

Start with diagnostics and server information:

```sh
oc --json doctor store
oc --json doctor --online store
oc admin info store
oc --json admin info store
```

`doctor` is offline by default. It reports the client, Go and SDK versions, protocols, whether management is separate, custom management CA presence and whether certificate verification is enabled. It omits endpoint hosts, credentials and configuration paths. `--online` makes a read-only management `ServerInfo` request with a 15-second deadline. Neither mode tests the alias's S3 read/write permissions.

`admin info` displays server information. Its JSON error path returns a nonzero exit status and includes an error category, with an error code when available. When scripting, check the exit status before parsing successful output; do not assume all administrative commands have identical result schemas.

For a local health report, run:

```sh
oc admin subnet health --deadline 5m store
```

The command queries the configured management endpoint and writes a compressed JSON report in the current directory. It may run drive and network tests, so schedule it with the server operator. The report can contain server configuration and sensitive environment details. Inspect it before sharing. `--json` writes the health report to standard output instead of creating the compressed file. In that mode, inspect the report's `status` (`Success` or `Error`) and `error`; its exit status alone is not proof that every health check passed.

The historical `subnet` name remains for this local report. Uploads are disabled; supplying `--license` or `--dev`, even empty or false values, fails before contacting the server. The deprecated `admin health`/`admin obd` entry only directs users to `admin subnet health`.

## Manage policies, users and groups

Use a least-privilege management identity for these operations. `admin user` and `admin group` change server-side identities; [client configuration](configuration.md) controls local aliases.

Inspect existing state first:

```sh
oc admin policy list store
oc admin policy info store archive-reader
oc admin user list store
oc admin user info store analyst
oc admin group list store
oc admin group info store readers
```

To create a read-only policy for an existing `archive` bucket, save this example as `archive-reader.json` and adjust the bucket and actions for your deployment:

```json
{
  "Version": "2012-10-17",
  "Statement": [
    {
      "Effect": "Allow",
      "Action": ["s3:ListBucket"],
      "Resource": ["arn:aws:s3:::archive"]
    },
    {
      "Effect": "Allow",
      "Action": ["s3:GetObject"],
      "Resource": ["arn:aws:s3:::archive/*"]
    }
  ]
}
```

The following commands modify IAM state. `user add store` prompts for the new user's access key and secret key; enter `analyst` as the access key to use the subsequent examples:

```sh
oc admin policy add store archive-reader ./archive-reader.json
oc admin user add store
oc admin policy set store archive-reader user=analyst
```

Alternatively, assign permissions through a group:

```sh
oc admin group add store readers analyst
oc admin policy set store archive-reader group=readers
```

`policy set` replaces the entity's assigned policies. `policy update` adds a policy to an existing assignment; `policy unset` removes a named assignment. `policy remove` deletes the policy definition. This version uses `add`, `set`, `update` and `unset`; use its own `--help` rather than assuming another client's `create`/`attach`/`detach` syntax.

Other available operations are `user disable`, `user enable`, `user remove` and `user policy`; and `group disable`, `group enable` and `group remove`. Removing a group member takes `ALIAS GROUP MEMBER...`; omitting members requests deletion of an empty group. Server-side permissions and state still determine whether an operation is allowed. Verify the resulting access with a restricted test identity before rolling out changes.

## Manage service accounts

Service accounts are managed under `admin user svcacct`. Tests of the pinned server baseline use a permitted ordinary management user because the server rejects root for some service-account operations. OC does not bypass that restriction.

Create an account for `analyst` and restrict it with a policy file:

```sh
oc --json admin user svcacct add --policy ./archive-reader.json store analyst
```

The JSON result includes the generated access key and secret key. Capture it in your secret-management system and keep it out of CI logs, issues and chat transcripts. Supplying explicit credentials requires both `--access-key` and `--secret-key`; the current creation limits are 3–20 and 8–40 bytes respectively. Credentials in command arguments can appear in shell history or process listings.

Inspect and manage the account using its access key:

```sh
oc admin user svcacct ls store analyst
oc admin user svcacct info store SERVICE_ACCESS_KEY
oc admin user svcacct disable store SERVICE_ACCESS_KEY
oc admin user svcacct enable store SERVICE_ACCESS_KEY
```

`svcacct set` edits its policy or secret key; a supplied nonempty replacement secret must contain at least 8 bytes. `svcacct rm ALIAS SERVICE_ACCESS_KEY` deletes it. Plan a secret rotation with consumers, verify the replacement credential, then confirm the old credential is rejected. Use `oc admin user svcacct set --help` for the available options.

## Metrics, configuration and service control

Generate Prometheus configuration for the object listener:

```sh
oc admin prometheus generate store
oc admin prometheus generate --metrics-type node store
oc admin prometheus generate --metrics-type legacy store
oc admin prometheus generate --metrics-ca /etc/prometheus/s3-ca.pem store
```

The cluster, node and legacy metrics paths are `/otterio/v2/metrics/cluster`, `/otterio/v2/metrics/node` and `/otterio/prometheus/metrics`. OC queries server information through management but configures scraping through the S3 listener. `--metrics-ca` refers to the S3 CA path on the Prometheus host and requires HTTPS. `--public` omits authentication only for a server explicitly configured for public metrics. The default configuration contains a bearer token generated from static credentials; temporary session credentials cannot generate a long-lived metrics token. Protect the generated file.

`admin config get`, `set`, `export`, `import`, `reset`, `history` and `restore` operate on server configuration. Exported configuration may contain secrets. Review changes, preserve a protected backup and follow the server's restart requirements. These operations are separate from `oc config import`, which imports the client's version 10 alias configuration.

Service control affects the target deployment and can interrupt applications:

```sh
oc admin service restart --timeout 2m store
```

The default readiness timeout is one minute. OC sends one restart request and waits for all original nodes to return with changed startup times. A timeout means recovery was not confirmed; it does not send another restart. `oc admin service stop store` stops the service; bring it back using the server's process manager.

## Check capability and deployment requirements

The CLI also exposes `admin trace`, `admin console`, `admin profile`, `admin heal`, `admin top`, `admin kms` and `admin bucket`. `admin heal` and its legacy options are marked deprecated in current help. Availability in help is not evidence that every server or deployment can execute them. For example, distributed lock queries require a distributed deployment, KMS operations require a configured external KMS, and notification targets or replication require their corresponding server configuration and reachable services.

The recorded acceptance covers selected operations against the pinned single-node OtterIO baseline. Its source includes the former compatibility fixes without additional patches. Heal status checks do not prove recovery from failed disks; lifecycle configuration round trips do not prove timed expiration; live event streams do not prove delivery to an external notification target. Consult [compatibility](compatibility.md) for those boundaries and [troubleshooting](troubleshooting.md) when a server reports an unsupported operation or a permission error.
