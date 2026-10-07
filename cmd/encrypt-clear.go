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

var encryptClearCmd = &cli.Command{
	Name:         "clear",
	Usage:        "clear encryption config",
	Action:       commandAction(mainEncryptClear),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}
   
USAGE:
  {{.FullName}} TARGET
   
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Remove auto encryption config on bucket "mybucket" for alias "store".
     {{Prompt}} {{.FullName}} store/mybucket
`,
}

// checkEncryptClearSyntax - validate all the passed arguments
func checkEncryptClearSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "clear", 1) // last argument is exit code
	}
}

type encryptClearMessage struct {
	Op     string `json:"op"`
	Status string `json:"status"`
	URL    string `json:"url"`
}

func (v encryptClearMessage) JSON() string {
	v.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(v, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(jsonMessageBytes)
}

func (v encryptClearMessage) String() string {
	return console.Colorize("encryptClearMessage", fmt.Sprintf("Auto encryption configuration has been cleared successfully for %s", v.URL))
}

func mainEncryptClear(cliCtx *cli.Command) error {
	ctx, cancelencryptClear := context.WithCancel(globalContext)
	defer cancelencryptClear()

	console.SetColor("encryptClearMessage", color.New(color.FgGreen))

	checkEncryptClearSyntax(cliCtx)

	// Get the alias parameter from cli
	args := cliCtx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	// Create a new Client
	client, err := newClient(aliasedURL)
	fatalIf(err, "Unable to initialize connection.")
	fatalIf(client.DeleteEncryption(ctx), "Unable to clear auto encryption configuration")
	printMsg(encryptClearMessage{
		Op:     "clear",
		Status: "success",
		URL:    aliasedURL,
	})
	return nil
}
