/*
 * MinIO Client (C) 2021 MinIO, Inc.
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
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

var adminUserSvcAcctEnableCmd = &cli.Command{
	Name:         "enable",
	Usage:        "Enable a service account",
	Action:       commandAction(mainAdminUserSvcAcctEnable),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} ALIAS SERVICE-ACCOUNT

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Enable the service account 'J123C4ZXEQN8RK6ND35I' in OtterIO server.
     {{Prompt}} {{.FullName}} store/ J123C4ZXEQN8RK6ND35I
`,
}

// checkAdminUserSvcAcctEnableSyntax - validate all the passed arguments
func checkAdminUserSvcAcctEnableSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		fatalIf(errInvalidArgument().Trace(ctx.Args().Tail()...),
			"Incorrect number of arguments for user svcacct enable command.")
	}
}

// mainAdminUserSvcAcctEnable is the handle for "mc admin user svcacct enable" command.
func mainAdminUserSvcAcctEnable(ctx *cli.Command) error {
	checkAdminUserSvcAcctEnableSyntax(ctx)

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	svcAccount := argumentAt(args, 1)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	opts := madmin.UpdateServiceAccountReq{
		NewStatus: "on",
	}

	e := client.UpdateServiceAccount(globalContext, svcAccount, opts)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to get enable the specified service account")

	printMsg(svcAcctMessage{
		op:        "enable",
		AccessKey: svcAccount,
	})

	return nil
}
