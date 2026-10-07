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
	"context"
	"fmt"
	"strings"

	"github.com/fatih/color"
	jsoniter "github.com/json-iterator/go"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/urfave/cli/v3"
)

var adminUserPolicyCmd = &cli.Command{
	Name:         "policy",
	Usage:        "export user policies in JSON format",
	Action:       commandAction(mainAdminUserPolicy),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET USERNAME

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Display the policy document of a user "foobar" in JSON format.
     {{Prompt}} {{.FullName}} store foobar

`,
}

// checkAdminUserPolicySyntax - validate all the passed arguments
func checkAdminUserPolicySyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "policy", 1) // last argument is exit code
	}
}

// mainAdminUserPolicy is the handler for "mc admin user policy" command.
func mainAdminUserPolicy(ctx *cli.Command) error {
	checkAdminUserPolicySyntax(ctx)

	console.SetColor("UserMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	user, e := client.GetUserInfo(globalContext, argumentAt(args, 1))
	fatalIf(probe.NewError(e).Trace(args...), "Unable to get user info")

	var combinedPolicy iampolicy.Policy

	policies := strings.Split(user.PolicyName, ",")

	for _, p := range policies {
		policy, e := client.InfoCannedPolicy(globalContext, p)
		fatalIf(probe.NewError(e).Trace(args...), "Unable to get user's policy document")
		combinedPolicy = combinedPolicy.Merge(*policy)
	}

	var jsoniter = jsoniter.ConfigCompatibleWithStandardLibrary
	policyJSON, e := jsoniter.MarshalIndent(combinedPolicy, "", "   ")
	fatalIf(probe.NewError(e).Trace(args...), "Unable to parse user's policy document")

	fmt.Println(string(policyJSON))

	return nil
}
