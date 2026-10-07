/*
 * MinIO Client (C) 2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"context"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/auth"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

var adminBucketRemoteEditFlags = []cli.Flag{
	&cli.StringFlag{
		Name:  "arn",
		Usage: "ARN of target",
	},
	&cli.StringFlag{
		Name:  "sync",
		Usage: "enable synchronous replication for this target. Valid values are enable,disable.Defaults to disable if unset",
	},
	&cli.StringFlag{
		Name:  "bandwidth",
		Usage: "Set bandwidth limit in bits per second (K,B,G,T for metric and Ki,Bi,Gi,Ti for IEC units)",
	},
	&cli.UintFlag{
		Name:  "healthcheck-seconds",
		Usage: "health check duration in seconds",
		Value: 60,
	},
	&cli.StringFlag{
		Name:  "path",
		Value: "auto",
		Usage: "bucket path lookup supported by the server. Valid options are '[on,off,auto]'",
	},
}
var adminBucketRemoteEditCmd = &cli.Command{
	Name:         "edit",
	Usage:        "edit remote target",
	Action:       commandAction(mainAdminBucketRemoteEdit),
	Before:       commandBefore(setGlobalsFromContext),
	OnUsageError: onUsageError,
	Flags:        append(globalFlags, adminBucketRemoteEditFlags...),
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET http(s)://ACCESSKEY:SECRETKEY@DEST_URL/DEST_BUCKET --arn arn

TARGET:
  Also called as alias/sourcebucketname

DEST_BUCKET:
  Also called as remote target bucket.

DEST_URL:
  Also called as remote endpoint.

ACCESSKEY:
  Also called as username.

SECRETKEY:
  Also called as password.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Edit credentials for existing remote target with arn where a remote target has been configured between sourcebucket on sitea to targetbucket on siteb.
    {{DisableHistory}}
  {{"\t"}}{{Prompt}} {{.FullName}} sitea/sourcebucket \
                 https://foobar:newpassword@minio.siteb.example.com/targetbucket \
				 --arn "arn:minio:replication:us-west-1:993bc6b6-accd-45e3-884f-5f3e652aed2a:dest1"
    {{EnableHistory}}

  2. Edit remote target for sourceBucket on sitea with specified ARN to enable synchronous replication
	   {{Prompt}} {{.FullName}} sitea/sourcebucket --sync "enable" \
				--arn "arn:minio:replication:us-west-1:993bc6b6-accd-45e3-884f-5f3e652aed2a:dest1"
`,
}

// checkAdminBucketRemoteEditSyntax - validate all the passed arguments
func checkAdminBucketRemoteEditSyntax(ctx *cli.Command) {
	argsNr := ctx.Args().Len()
	if argsNr > 2 || argsNr == 0 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, ctx.Name, 1) // last argument is exit code
	}
	if !ctx.IsSet("arn") {
		fatalIf(errInvalidArgument().Trace(ctx.Args().Slice()...), "--arn flag needs to be set")
	}
}

// modifyRemoteTarget - modifies the dest credentials or updates sync settings
func modifyRemoteTarget(cli *cli.Command, targets []madmin.BucketTarget) *madmin.BucketTarget {
	args := cli.Args().Slice()
	foundIdx := -1
	arn := cli.String("arn")
	for i, t := range targets {
		if t.Arn == arn {
			foundIdx = i
			break
		}
	}
	if foundIdx < 0 {
		fatalIf(errInvalidArgument().Trace(args...), "Unable to edit remote target - `"+arn+"` not found")
	}
	bktTarget := targets[foundIdx].Clone()
	if cli.IsSet("sync") {
		syncState := strings.ToLower(cli.String("sync"))
		switch syncState {
		case "enable", "disable":
			bktTarget.ReplicationSync = syncState == "enable"
		default:
			fatalIf(errInvalidArgument().Trace(args...), "--sync can be either [enable|disable]")
		}
	}

	if len(args) == 2 {
		_, sourceBucket := url2Alias(args[0])
		TargetURL := args[1]
		parts := targetKeys.FindStringSubmatch(TargetURL)
		if len(parts) != 6 {
			fatalIf(probe.NewError(fmt.Errorf("invalid url format")), "Malformed Remote target URL")
		}
		accessKey := parts[2]
		secretKey := parts[3]
		parsedURL := fmt.Sprintf("%s%s", parts[1], parts[4])
		targetBucket := strings.TrimSuffix(parts[5], slashSeperator)
		targetBucket = strings.TrimPrefix(targetBucket, slashSeperator)
		u, cerr := url.Parse(parsedURL)
		if cerr != nil {
			fatalIf(probe.NewError(cerr), "Malformed Remote target URL")
		}
		secure := u.Scheme == "https"
		host := u.Host
		if u.Port() == "" {
			port := 80
			if secure {
				port = 443
			}
			host = host + ":" + strconv.Itoa(port)
		}
		console.SetColor(cred, color.New(color.FgYellow, color.Italic))
		creds := &auth.Credentials{AccessKey: accessKey, SecretKey: secretKey}
		if host != bktTarget.Endpoint {
			fatalIf(errInvalidArgument().Trace(args...), "configured Endpoint `"+host+"` does not match "+bktTarget.Endpoint+"` for this ARN `"+bktTarget.Arn+"`")
		}
		if targetBucket != bktTarget.TargetBucket {
			fatalIf(errInvalidArgument().Trace(args...), "configured remote target bucket `"+targetBucket+"` does not match "+bktTarget.TargetBucket+"` for this ARN `"+bktTarget.Arn+"`")
		}
		if sourceBucket != bktTarget.SourceBucket {
			fatalIf(errInvalidArgument().Trace(args...), "configured source bucket `"+sourceBucket+"` does not match "+bktTarget.SourceBucket+"` for this ARN `"+bktTarget.Arn+"`")
		}
		bktTarget.TargetBucket = targetBucket
		bktTarget.Secure = secure
		bktTarget.Credentials = creds
		bktTarget.Endpoint = host
	}
	if cli.IsSet("bandwidth") {
		bandwidthStr := cli.String("bandwidth")
		bandwidth, err := getBandwidthInBytes(bandwidthStr)
		if err != nil {
			fatalIf(errInvalidArgument().Trace(bandwidthStr), "Invalid bandwidth number")
		}
		bktTarget.BandwidthLimit = int64(bandwidth)
	}
	if cli.IsSet("healthcheck-seconds") {
		bktTarget.HealthCheckDuration = time.Duration(cli.Uint("healthcheck-seconds")) * time.Second
	}
	if cli.IsSet("path") {
		bktTarget.Path = cli.String("path")
	}
	return &bktTarget
}

// mainAdminBucketRemoteEdit is the handle for "mc admin bucket remote edit" command.
func mainAdminBucketRemoteEdit(ctx *cli.Command) error {
	checkAdminBucketRemoteEditSyntax(ctx)

	console.SetColor("RemoteMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	// Create a new MinIO Admin Client
	client, cerr := newAdminClient(aliasedURL)
	fatalIf(cerr, "Unable to initialize admin connection.")
	_, sourceBucket := url2Alias(args[0])

	targets, e := client.ListRemoteTargets(globalContext, sourceBucket, "")
	fatalIf(probe.NewError(e).Trace(args...), "Unable to fetch remote target.")

	bktTarget := modifyRemoteTarget(ctx, targets)

	arn, e := client.UpdateRemoteTarget(globalContext, bktTarget)
	if e != nil {
		fatalIf(probe.NewError(e).Trace(args...), "Unable to update remote target `"+bktTarget.Endpoint+"` from `"+bktTarget.SourceBucket+"` -> `"+bktTarget.TargetBucket+"`")
	}

	printMsg(RemoteMessage{
		op:           ctx.Name,
		TargetURL:    bktTarget.URL().String(),
		TargetBucket: bktTarget.TargetBucket,
		AccessKey:    bktTarget.Credentials.AccessKey,
		SourceBucket: bktTarget.SourceBucket,
		RemoteARN:    arn,
	})

	return nil
}
