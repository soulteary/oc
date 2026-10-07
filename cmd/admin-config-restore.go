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
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var adminConfigRestoreCmd = &cli.Command{
	Name:         "restore",
	Usage:        "rollback back changes to a specific config history",
	Before:       commandBefore(setGlobalsFromContext),
	Action:       commandAction(mainAdminConfigRestore),
	OnUsageError: onUsageError,
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET RESTOREID

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Restore 'restore-id' history key value on OtterIO server.
     {{Prompt}} {{.FullName}} store/ <restore-id>
`,
}

// configRestoreMessage container to hold locks information.
type configRestoreMessage struct {
	Status      string `json:"status"`
	RestoreID   string `json:"restoreID"`
	targetAlias string
}

// String colorized service status message.
func (u configRestoreMessage) String() (msg string) {
	suggestion := fmt.Sprintf("mc admin service restart %s", u.targetAlias)
	msg += console.Colorize("ConfigRestoreMessage",
		fmt.Sprintf("Please restart your server with `%s`.\n", suggestion))
	msg += console.Colorize("ConfigRestoreMessage", "Restored "+u.RestoreID+" kv successfully.")
	return msg
}

// JSON jsonified service status Message message.
func (u configRestoreMessage) JSON() string {
	u.Status = "success"
	statusJSONBytes, e := json.MarshalIndent(u, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(statusJSONBytes)
}

// checkAdminConfigRestoreSyntax - validate all the passed arguments
func checkAdminConfigRestoreSyntax(ctx *cli.Command) {
	if !ctx.Args().Present() || ctx.Args().Len() > 2 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "restore", 1) // last argument is exit code
	}
}

func mainAdminConfigRestore(ctx *cli.Command) error {

	checkAdminConfigRestoreSyntax(ctx)

	console.SetColor("ConfigRestoreMessage", color.New(color.FgGreen))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	// Call get config API
	fatalIf(probe.NewError(client.RestoreConfigHistoryKV(globalContext, argumentAt(args, 1))), "Unable to restore server configuration.")

	// Print
	printMsg(configRestoreMessage{
		RestoreID:   argumentAt(args, 1),
		targetAlias: aliasedURL,
	})

	return nil
}
