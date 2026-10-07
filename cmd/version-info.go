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
	"strings"

	"github.com/fatih/color"
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var versionInfoCmd = &cli.Command{
	Name:         "info",
	Usage:        "show bucket versioning status",
	Action:       commandAction(mainVersionInfo),
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
   1. Display bucket versioning status for bucket "mybucket".
      {{Prompt}} {{.FullName}} store/mybucket
`,
}

// checkVersionInfoSyntax - validate all the passed arguments
func checkVersionInfoSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "info", 1) // last argument is exit code
	}
}

type versioningInfoMessage struct {
	Op         string
	Status     string `json:"status"`
	URL        string `json:"url"`
	Versioning struct {
		Status    string `json:"status"`
		MFADelete string `json:"MFADelete"`
	} `json:"versioning"`
}

func (v versioningInfoMessage) JSON() string {
	v.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(v, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(jsonMessageBytes)
}

func (v versioningInfoMessage) String() string {
	msg := ""
	switch v.Versioning.Status {
	case "":
		msg = fmt.Sprintf("%s is un-versioned", v.URL)
	default:
		msg = fmt.Sprintf("%s versioning is %s", v.URL, strings.ToLower(v.Versioning.Status))
	}
	return console.Colorize("versioningInfoMessage", msg)
}

func mainVersionInfo(cliCtx *cli.Command) error {
	ctx, cancelVersioningInfo := context.WithCancel(globalContext)
	defer cancelVersioningInfo()

	console.SetColor("versioningInfoMessage", color.New(color.FgGreen))

	checkVersionInfoSyntax(cliCtx)

	// Get the alias parameter from cli
	args := cliCtx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	// Create a new Client
	client, err := newClient(aliasedURL)
	fatalIf(err, "Unable to initialize connection.")
	vConfig, e := client.GetVersion(ctx)
	fatalIf(e, "Unable to get versioning info")
	vMsg := versioningInfoMessage{
		Op:     "info",
		Status: "success",
		URL:    aliasedURL,
	}
	vMsg.Versioning.Status = vConfig.Status
	vMsg.Versioning.MFADelete = vConfig.MFADelete
	printMsg(vMsg)
	return nil
}
