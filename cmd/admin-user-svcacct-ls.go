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

var adminUserSvcAcctListCmd = &cli.Command{
	Name:         "ls",
	Usage:        "List services accounts",
	Action:       commandAction(mainAdminUserSvcAcctList),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} ALIAS TARGET-ACCOUNT

TARGET-ACCOUNT:
  Could be a OtterIO user, STS or LDAP account.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. List all service accounts for user 'foobar'.
     {{Prompt}} {{.FullName}} store/ foobar
`,
}

// checkAdminUserSvcAcctListSyntax - validate all the passed arguments
func checkAdminUserSvcAcctListSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		fatalIf(errInvalidArgument().Trace(ctx.Args().Tail()...),
			"Incorrect number of arguments for user svcacct ls command.")
	}
}

// mainAdminUserSvcAcctList is the handle for "mc admin user svcacct ls" command.
func mainAdminUserSvcAcctList(ctx *cli.Command) error {
	checkAdminUserSvcAcctListSyntax(ctx)

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	user := argumentAt(args, 1)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	svcList, e := client.ListServiceAccounts(globalContext, user)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to add a new service account")

	for _, svc := range svcList.Accounts {
		printMsg(svcAcctMessage{
			op:        "ls",
			AccessKey: svc,
		})
	}

	return nil
}
