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

	"github.com/fatih/color"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

var adminGroupRemoveCmd = &cli.Command{
	Name:         "remove",
	Usage:        "remove group or members from a group",
	Action:       commandAction(mainAdminGroupRemove),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET GROUPNAME [USERNAMES...]

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Remove members 'tencent' and 'fivecent' from group 'allcents'.
     {{Prompt}} {{.FullName}} store allcents tencent fivecent

  2. Remove group 'allcents'.
     {{Prompt}} {{.FullName}} store allcents
`,
}

// checkAdminGroupRemoveSyntax - validate all the passed arguments
func checkAdminGroupRemoveSyntax(ctx *cli.Command) {
	if ctx.Args().Len() < 2 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "remove", 1) // last argument is exit code
	}
}

// mainAdminGroupRemove is the handle for "mc admin group remove" command.
func mainAdminGroupRemove(ctx *cli.Command) error {
	checkAdminGroupRemoveSyntax(ctx)

	console.SetColor("GroupMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	members := []string{}
	for i := 2; i < ctx.NArg(); i++ {
		members = append(members, argumentAt(args, i))
	}
	gAddRemove := madmin.GroupAddRemove{
		Group:    argumentAt(args, 1),
		Members:  members,
		IsRemove: true,
	}

	e := client.UpdateGroupMembers(globalContext, gAddRemove)
	fatalIf(probe.NewError(e).Trace(args...), "Could not perform remove operation")

	printMsg(groupMessage{
		op:        "remove",
		GroupName: argumentAt(args, 1),
		Members:   members,
	})

	return nil
}
