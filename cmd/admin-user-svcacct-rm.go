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
	"github.com/urfave/cli/v3"
)

var adminUserSvcAcctRemoveCmd = &cli.Command{
	Name:         "rm",
	Usage:        "Remove a service account",
	Action:       commandAction(mainAdminUserSvcAcctRemove),
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
  1. Remove the service account 'J123C4ZXEQN8RK6ND35I' from OtterIO server.
     {{Prompt}} {{.FullName}} store/ J123C4ZXEQN8RK6ND35I
`,
}

// checkAdminUserSvcAcctRemoveSyntax - validate all the passed arguments
func checkAdminUserSvcAcctRemoveSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		fatalIf(errInvalidArgument().Trace(ctx.Args().Tail()...),
			"Incorrect number of arguments for user svcacct rm command.")
	}
}

// mainAdminUserSvcAcctRemove is the handle for "mc admin user svcacct rm" command.
func mainAdminUserSvcAcctRemove(ctx *cli.Command) error {
	checkAdminUserSvcAcctRemoveSyntax(ctx)

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	svcAccount := argumentAt(args, 1)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	e := client.DeleteServiceAccount(globalContext, svcAccount)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to remove a new service account")

	printMsg(svcAcctMessage{
		op:        "ls",
		AccessKey: svcAccount,
	})

	return nil
}
