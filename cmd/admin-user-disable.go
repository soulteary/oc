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
	"context"

	"github.com/fatih/color"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

var adminUserDisableCmd = &cli.Command{
	Name:         "disable",
	Usage:        "disable user",
	Action:       commandAction(mainAdminUserDisable),
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
  1. Disable a user 'foobar' on OtterIO server.
     {{Prompt}} {{.FullName}} store foobar
`,
}

// checkAdminUserDisableSyntax - validate all the passed arguments
func checkAdminUserDisableSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "disable", 1) // last argument is exit code
	}
}

// mainAdminUserDisable is the handle for "mc admin user disable" command.
func mainAdminUserDisable(ctx *cli.Command) error {
	checkAdminUserDisableSyntax(ctx)

	console.SetColor("UserMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	e := client.SetUserStatus(globalContext, argumentAt(args, 1), madmin.AccountDisabled)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to disable user")

	printMsg(userMessage{
		op:        "disable",
		AccessKey: argumentAt(args, 1),
	})

	return nil
}
