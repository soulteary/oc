/*
 * MinIO Client (C) 2019 MinIO, Inc.
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

	"github.com/fatih/color"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var adminPolicyInfoCmd = &cli.Command{
	Name:         "info",
	Usage:        "show info on a policy",
	Action:       commandAction(mainAdminPolicyInfo),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET POLICYNAME

POLICYNAME:
  Name of the policy on the OtterIO server.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Show information on a given policy.
     {{Prompt}} {{.FullName}} store writeonly
`,
}

// checkAdminPolicyInfoSyntax - validate all the passed arguments
func checkAdminPolicyInfoSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "info", 1) // last argument is exit code
	}
}

// mainAdminPolicyInfo is the handler for "mc admin policy info" command.
func mainAdminPolicyInfo(ctx *cli.Command) error {
	checkAdminPolicyInfoSyntax(ctx)

	console.SetColor("PolicyMessage", color.New(color.FgGreen))
	console.SetColor("Policy", color.New(color.FgBlue))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	policyName := argumentAt(args, 1)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection")

	policies, e := client.ListCannedPolicies(globalContext)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to list policy")

	iamp, ok := policies[policyName]
	if ok {
		printMsg(userPolicyMessage{
			op:         "info",
			Policy:     policyName,
			PolicyJSON: iamp,
		})
	} else {
		fatalIf(probe.NewError(fmt.Errorf("%s is not found", policyName)), "Unable to display policy")
	}
	return nil
}
