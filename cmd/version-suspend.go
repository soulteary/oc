/*
 * MinIO Client (C) 2020 MinIO, Inc.
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

var versionSuspendCmd = &cli.Command{
	Name:         "suspend",
	Usage:        "suspend bucket versioning",
	Action:       commandAction(mainVersionSuspend),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} ALIAS/BUCKET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Suspend versioning on bucket "mybucket" for alias "store".
     {{Prompt}} {{.FullName}} store/mybucket
`,
}

// checkVersionSuspendSyntax - validate all the passed arguments
func checkVersionSuspendSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "suspend", 1) // last argument is exit code
	}
}

type versionSuspendMessage struct {
	Op         string
	Status     string `json:"status"`
	URL        string `json:"url"`
	Versioning struct {
		Status    string `json:"status"`
		MFADelete string `json:"MFADelete"`
	} `json:"versioning"`
}

func (v versionSuspendMessage) JSON() string {
	v.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(v, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(jsonMessageBytes)
}

func (v versionSuspendMessage) String() string {
	return console.Colorize("versionSuspendMessage", fmt.Sprintf("%s versioning is suspended", v.URL))
}

func mainVersionSuspend(cliCtx *cli.Command) error {
	ctx, cancelVersionSuspend := context.WithCancel(globalContext)
	defer cancelVersionSuspend()

	console.SetColor("versionSuspendMessage", color.New(color.FgGreen))

	checkVersionSuspendSyntax(cliCtx)

	// Get the alias parameter from cli
	args := cliCtx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	// Create a new Client
	client, err := newClient(aliasedURL)
	fatalIf(err, "Unable to initialize connection.")
	fatalIf(client.SetVersion(ctx, "suspend"), "Unable to suspend versioning")
	printMsg(versionSuspendMessage{
		Op:     "suspend",
		Status: "success",
		URL:    aliasedURL,
	})
	return nil
}
