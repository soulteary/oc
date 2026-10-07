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

var adminUserSvcAcctDisableCmd = &cli.Command{
	Name:         "disable",
	Usage:        "Disable a services account",
	Action:       commandAction(mainAdminUserSvcAcctDisable),
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
  1. Disable the service account 'J123C4ZXEQN8RK6ND35I' in OtterIO server.
     {{Prompt}} {{.FullName}} store/ J123C4ZXEQN8RK6ND35I
`,
}

// checkAdminUserSvcAcctDisableSyntax - validate all the passed arguments
func checkAdminUserSvcAcctDisableSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		fatalIf(errInvalidArgument().Trace(ctx.Args().Tail()...),
			"Incorrect number of arguments for user svcacct disable command.")
	}
}

// mainAdminUserSvcAcctDisable is the handle for "mc admin user svcacct disable" command.
func mainAdminUserSvcAcctDisable(ctx *cli.Command) error {
	checkAdminUserSvcAcctDisableSyntax(ctx)

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	svcAccount := argumentAt(args, 1)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	opts := madmin.UpdateServiceAccountReq{
		NewStatus: "off",
	}

	e := client.UpdateServiceAccount(globalContext, svcAccount, opts)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to get disable the specified service account")

	printMsg(svcAcctMessage{
		op:        "disable",
		AccessKey: svcAccount,
	})

	return nil
}
