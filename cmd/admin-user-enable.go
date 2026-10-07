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

var adminUserEnableCmd = &cli.Command{
	Name:         "enable",
	Usage:        "enable user",
	Action:       commandAction(mainAdminUserEnable),
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
  1. Enable a disabled user 'foobar' on OtterIO server.
     {{Prompt}} {{.FullName}} store foobar
`,
}

// checkAdminUserEnableSyntax - validate all the passed arguments
func checkAdminUserEnableSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 2 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "enable", 1) // last argument is exit code
	}
}

// mainAdminUserEnable is the handle for "mc admin user enable" command.
func mainAdminUserEnable(ctx *cli.Command) error {
	checkAdminUserEnableSyntax(ctx)

	console.SetColor("UserMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	e := client.SetUserStatus(globalContext, argumentAt(args, 1), madmin.AccountEnabled)
	fatalIf(probe.NewError(e).Trace(args...), "Unable to enable user")

	printMsg(userMessage{
		op:        "enable",
		AccessKey: argumentAt(args, 1),
	})

	return nil
}
