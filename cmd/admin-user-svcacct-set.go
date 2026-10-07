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
	"fmt"
	"os"

	"github.com/soulteary/mc/pkg/probe"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

var adminUserSvcAcctSetFlags = []cli.Flag{
	&cli.StringFlag{
		Name:  "secret-key",
		Usage: "set a secret key for the service account",
	},
	&cli.StringFlag{
		Name:  "policy",
		Usage: "path to a JSON policy file",
	},
}

var adminUserSvcAcctSetCmd = &cli.Command{
	Name:         "set",
	Usage:        "edit an existing service account",
	Action:       commandAction(mainAdminUserSvcAcctSet),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        append(adminUserSvcAcctSetFlags, globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} ALIAS SERVICE-ACCOUNT

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Change the secret key of the service account 'J123C4ZXEQN8RK6ND35I' in OtterIO server.
     {{Prompt}} {{.FullName}} store/ 'J123C4ZXEQN8RK6ND35I' --secret-key 'xxxxxxxx'
`,
}

// checkAdminUserSvcAcctSetSyntax - validate all the passed arguments
func checkAdminUserSvcAcctSetSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		fatalIf(errInvalidArgument().Trace(ctx.Args().Tail()...),
			"Incorrect number of arguments for user svcacct set command.")
	}
}

// mainAdminUserSvcAcctSet is the handle for "mc admin user svcacct set" command.
func mainAdminUserSvcAcctSet(ctx *cli.Command) error {
	checkAdminUserSvcAcctSetSyntax(ctx)

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	svcAccount := argumentAt(args, 1)

	secretKey := ctx.String("secret-key")
	if secretKey != "" && len(secretKey) < 8 {
		fatalIf(probe.NewError(fmt.Errorf("service secret key must contain at least 8 bytes")), "Invalid service account credentials.")
	}
	policyPath := ctx.String("policy")

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	var policy *iampolicy.Policy
	if policyPath != "" {
		var e error
		f, e := os.Open(policyPath)
		fatalIf(probe.NewError(e), "Unable to open the policy document.")
		policy, e = iampolicy.ParseConfig(f)
		fatalIf(probe.NewError(e), "Unable to parse the policy document.")
	}

	opts := madmin.UpdateServiceAccountReq{
		NewPolicy:    policy,
		NewSecretKey: secretKey,
	}

	e := client.UpdateServiceAccount(globalContext, svcAccount, opts)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to update the service account")

	printMsg(svcAcctMessage{
		op:        "set",
		AccessKey: svcAccount,
	})

	return nil
}
