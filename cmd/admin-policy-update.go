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
	"errors"
	"fmt"
	"strings"

	"github.com/fatih/color"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var adminPolicyUpdateCmd = &cli.Command{
	Name:         "update",
	Usage:        "Attach new IAM policy to a user or group",
	Action:       commandAction(mainAdminPolicyUpdate),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET POLICYNAME [ user=username1 | group=groupname1 ]

POLICYNAME:
  Name of the policy on the OtterIO server.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Add the "diagnostics" policy for user "james".
     {{Prompt}} {{.FullName}} store diagnostics user=james

  2. add the "diagnostics" policy for group "auditors".
     {{Prompt}} {{.FullName}} store diagnostics group=auditors
`,
}

func checkAdminPolicyUpdateSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 3 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "update", 1) // last argument is exit code
	}
}

func updateCannedPolicies(existingPolicies, policiesToAdd string) (string, error) {
	policiesToAdd = strings.TrimSpace(policiesToAdd)
	if policiesToAdd == "" {
		return "", errors.New("empty policy name is unsupported")
	}
	var updatedPolicies []string
	if existingPolicies != "" {
		updatedPolicies = strings.Split(existingPolicies, ",")
	}

	for _, p1 := range strings.Split(policiesToAdd, ",") {
		found := false
		p1 = strings.TrimSpace(p1)
		for _, p2 := range updatedPolicies {
			if p1 == p2 {
				found = true
				break
			}
		}
		if found {
			return "", fmt.Errorf("policy `%s` already exists", p1)
		}
		updatedPolicies = append(updatedPolicies, p1)
	}

	return strings.Join(updatedPolicies, ","), nil
}

// mainAdminPolicyUpdate is the handler for "mc admin policy update" command.
func mainAdminPolicyUpdate(ctx *cli.Command) error {
	checkAdminPolicyUpdateSyntax(ctx)

	console.SetColor("PolicyMessage", color.New(color.FgGreen))
	console.SetColor("Policy", color.New(color.FgBlue))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	policiesToAdd := argumentAt(args, 1)
	entityArg := argumentAt(args, 2)

	userOrGroup, isGroup, e1 := parseEntityArg(entityArg)
	fatalIf(probe.NewError(e1).Trace(args...), "Bad last argument")

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	var existingPolicies string

	if !isGroup {
		userInfo, e := client.GetUserInfo(globalContext, userOrGroup)
		fatalIf(probe.NewError(e).Trace(args...), "Unable to get user policy info")
		existingPolicies = userInfo.PolicyName
	} else {
		groupInfo, e := client.GetGroupDescription(globalContext, userOrGroup)
		fatalIf(probe.NewError(e).Trace(args...), "Unable to get group policy info")
		existingPolicies = groupInfo.Policy
	}

	updatedPolicies, e := updateCannedPolicies(existingPolicies, policiesToAdd)
	if err != nil {
		fatalIf(probe.NewError(e).Trace(args...), "Unable to update the policy")
	}

	e = client.SetPolicy(globalContext, updatedPolicies, userOrGroup, isGroup)
	if e == nil {
		printMsg(userPolicyMessage{
			op:          "update",
			Policy:      policiesToAdd,
			UserOrGroup: userOrGroup,
			IsGroup:     isGroup,
		})
	} else {
		fatalIf(probe.NewError(e), "Unable to unset the policy")
	}
	return nil
}
