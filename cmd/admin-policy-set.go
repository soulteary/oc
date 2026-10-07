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
	"errors"
	"strings"

	"github.com/fatih/color"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var adminPolicySetCmd = &cli.Command{
	Name:         "set",
	Usage:        "set IAM policy on a user or group",
	Action:       commandAction(mainAdminPolicySet),
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
  1. Set the "readwrite" policy for user "james".
     {{Prompt}} {{.FullName}} store readwrite user=james

  2. Set the "readonly" policy for group "auditors".
     {{Prompt}} {{.FullName}} store readonly group=auditors
`,
}

var (
	errBadUserGroupArg = errors.New("last argument must be of the form user=xx or group=xx")
)

func checkAdminPolicySetSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 3 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "set", 1) // last argument is exit code
	}
}

func parseEntityArg(arg string) (userOrGroup string, isGroup bool, err error) {
	parts := strings.SplitN(arg, "=", 2)
	switch {
	case len(parts) != 2 || parts[1] == "":
		err = errBadUserGroupArg
	case strings.ToLower(parts[0]) == "user":
		userOrGroup = parts[1]
		isGroup = false
	case strings.ToLower(parts[0]) == "group":
		userOrGroup = parts[1]
		isGroup = true
	default:
		err = errBadUserGroupArg

	}
	return
}

// mainAdminPolicySet is the handler for "mc admin policy set" command.
func mainAdminPolicySet(ctx *cli.Command) error {
	checkAdminPolicySetSyntax(ctx)

	console.SetColor("PolicyMessage", color.New(color.FgGreen))
	console.SetColor("Policy", color.New(color.FgBlue))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	policyName := strings.TrimSpace(argumentAt(args, 1))
	entityArg := argumentAt(args, 2)

	userOrGroup, isGroup, e1 := parseEntityArg(entityArg)
	fatalIf(probe.NewError(e1).Trace(args...), "Bad last argument")

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	e := client.SetPolicy(globalContext, policyName, userOrGroup, isGroup)
	if e == nil {
		printMsg(userPolicyMessage{
			op:          "set",
			Policy:      policyName,
			UserOrGroup: userOrGroup,
			IsGroup:     isGroup,
		})
	} else {
		fatalIf(probe.NewError(e), "Unable to set the policy")
	}
	return nil
}
