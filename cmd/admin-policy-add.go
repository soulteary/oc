/*
 * MinIO Client (C) 2018 MinIO, Inc.
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
	"bytes"
	"context"
	"fmt"
	"os"

	"github.com/fatih/color"
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/urfave/cli/v3"
)

var adminPolicyAddCmd = &cli.Command{
	Name:         "add",
	Usage:        "add new policy",
	Action:       commandAction(mainAdminPolicyAdd),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET POLICYNAME POLICYFILE

POLICYNAME:
  Name of the canned policy on OtterIO server.

POLICYFILE:
  Name of the policy file associated with the policy name.

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Add a new canned policy 'writeonly'.
     {{Prompt}} {{.FullName}} store writeonly /tmp/writeonly.json
 `,
}

// checkAdminPolicyAddSyntax - validate all the passed arguments
func checkAdminPolicyAddSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 3 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "add", 1) // last argument is exit code
	}
}

// userPolicyMessage container for content message structure
type userPolicyMessage struct {
	op          string
	Status      string            `json:"status"`
	Policy      string            `json:"policy,omitempty"`
	PolicyJSON  *iampolicy.Policy `json:"policyJSON,omitempty"`
	UserOrGroup string            `json:"userOrGroup,omitempty"`
	IsGroup     bool              `json:"isGroup"`
}

func (u userPolicyMessage) accountType() string {
	switch u.op {
	case "set", "unset", "update":
		if u.IsGroup {
			return "group"
		}
		return "user"
	}
	return ""
}

func (u userPolicyMessage) String() string {
	switch u.op {
	case "info":
		buf, e := json.MarshalIndent(u.PolicyJSON, "", " ")
		fatalIf(probe.NewError(e), "Unable to parse policy")
		return string(buf)
	case "list":
		policyFieldMaxLen := 20
		// Create a new pretty table with cols configuration
		return newPrettyTable("  ",
			Field{"Policy", policyFieldMaxLen},
		).buildRow(u.Policy)
	case "remove":
		return console.Colorize("PolicyMessage", "Removed policy `"+u.Policy+"` successfully.")
	case "add":
		return console.Colorize("PolicyMessage", "Added policy `"+u.Policy+"` successfully.")
	case "set", "unset":
		return console.Colorize("PolicyMessage",
			fmt.Sprintf("Policy `%s` is %s on %s `%s`", u.Policy, u.op, u.accountType(), u.UserOrGroup))
	case "update":
		return console.Colorize("PolicyMessage",
			fmt.Sprintf("Policy `%s` is added to %s `%s`", u.Policy, u.accountType(), u.UserOrGroup))
	}

	return ""
}

func (u userPolicyMessage) JSON() string {
	u.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(u, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(jsonMessageBytes)
}

// mainAdminPolicyAdd is the handle for "mc admin policy add" command.
func mainAdminPolicyAdd(ctx *cli.Command) error {
	checkAdminPolicyAddSyntax(ctx)

	console.SetColor("PolicyMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	policy, e := os.ReadFile(argumentAt(args, 2))
	fatalIf(probe.NewError(e).Trace(args...), "Unable to get policy")

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	iamp, e := iampolicy.ParseConfig(bytes.NewReader(policy))
	fatalIf(probe.NewError(e).Trace(args...), "Unable to parse the input policy")

	fatalIf(probe.NewError(client.AddCannedPolicy(globalContext, argumentAt(args, 1), iamp)).Trace(args...), "Unable to add new policy")

	printMsg(userPolicyMessage{
		op:     "add",
		Policy: argumentAt(args, 1),
	})

	return nil
}
