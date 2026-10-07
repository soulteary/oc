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

	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/urfave/cli/v3"
)

var adminConfigExportCmd = &cli.Command{
	Name:         "export",
	Usage:        "export all config keys to STDOUT",
	Before:       commandBefore(setGlobalsFromContext),
	Action:       commandAction(mainAdminConfigExport),
	OnUsageError: onUsageError,
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Export the current config from OtterIO server
     {{Prompt}} {{.FullName}} store/ > config.txt
`,
}

// configExportMessage container to hold locks information.
type configExportMessage struct {
	Status string `json:"status"`
	Value  []byte `json:"value"`
}

// String colorized service status message.
func (u configExportMessage) String() string {
	return string(u.Value)
}

// JSON jsonified service status Message message.
func (u configExportMessage) JSON() string {
	u.Status = "success"
	statusJSONBytes, e := json.MarshalIndent(u, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(statusJSONBytes)
}

// checkAdminConfigExportSyntax - validate all the passed arguments
func checkAdminConfigExportSyntax(ctx *cli.Command) {
	if !ctx.Args().Present() || ctx.Args().Len() > 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "export", 1) // last argument is exit code
	}
}

func mainAdminConfigExport(ctx *cli.Command) error {

	checkAdminConfigExportSyntax(ctx)

	// Export the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	// Call get config API
	buf, e := client.GetConfig(globalContext)
	fatalIf(probe.NewError(e), "Unable to get server config")

	// Print
	printMsg(configExportMessage{
		Value: buf,
	})

	return nil
}
