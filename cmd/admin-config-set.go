/*
 * MinIO Client (C) 2017-2019 MinIO, Inc.
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
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

var adminConfigSetCmd = &cli.Command{
	Name:         "set",
	Usage:        "interactively set a config key parameters",
	Before:       commandBefore(setGlobalsFromContext),
	Action:       commandAction(mainAdminConfigSet),
	OnUsageError: onUsageError,
	Flags:        append(adminConfigEnvFlags, globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Enable webhook notification target for OtterIO server.
     {{Prompt}} {{.FullName}} store/ notify_webhook endpoint="http://localhost:8080/minio/events"

  2. Change region name for the OtterIO server to 'us-west-1'.
     {{Prompt}} {{.FullName}} store/ region name=us-west-1

  3. Change healing settings on a distributed OtterIO server setup.
     {{Prompt}} {{.FullName}} mydist/ heal max_delay=300ms max_io=50
`,
}

// configSetMessage container to hold locks information.
type configSetMessage struct {
	Status      string `json:"status"`
	targetAlias string
	restart     bool
}

// String colorized service status message.
func (u configSetMessage) String() (msg string) {
	msg += console.Colorize("SetConfigSuccess",
		"Successfully applied new settings.")
	if u.restart {
		suggestion := color.RedString("mc admin service restart %s", u.targetAlias)
		msg += console.Colorize("SetConfigSuccess",
			fmt.Sprintf("\nPlease restart your server '%s'.", suggestion))
	}
	return
}

// JSON jsonified service status message.
func (u configSetMessage) JSON() string {
	u.Status = "success"
	statusJSONBytes, e := json.MarshalIndent(u, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(statusJSONBytes)
}

// checkAdminConfigSetSyntax - validate all the passed arguments
func checkAdminConfigSetSyntax(ctx *cli.Command) {
	if !ctx.Args().Present() && ctx.Args().Len() < 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "set", 1) // last argument is exit code
	}
}

// main config set function
func mainAdminConfigSet(ctx *cli.Command) error {

	// Check command arguments
	checkAdminConfigSetSyntax(ctx)

	// Set color preference of command outputs
	console.SetColor("SetConfigSuccess", color.New(color.FgGreen, color.Bold))

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	input := strings.Join(argumentTail(args), " ")

	if !strings.Contains(input, madmin.KvSeparator) {
		// Call get config API
		hr, e := client.HelpConfigKV(globalContext, argumentAt(args, 1), argumentAt(args, 2), ctx.IsSet("env"))
		fatalIf(probe.NewError(e), "Unable to get help for the sub-system")

		// Print
		printMsg(configHelpMessage{
			Value:   hr,
			envOnly: ctx.IsSet("env"),
		})

		return nil

	}

	// Call set config API
	restart, e := client.SetConfigKV(globalContext, input)
	fatalIf(probe.NewError(e), "Unable to set '%s' to server", input)

	// Print set config result
	printMsg(configSetMessage{
		targetAlias: aliasedURL,
		restart:     restart,
	})

	return nil
}
