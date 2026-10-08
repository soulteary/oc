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
	"os"

	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio-sdk/v7/pkg/lifecycle"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var ilmImportCmd = &cli.Command{
	Name:         "import",
	Usage:        "import lifecycle configuration in JSON format",
	Action:       commandAction(mainILMImport),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET

DESCRIPTION:
  Import entire lifecycle configuration from STDIN, input file is expected to be in JSON format.

EXAMPLES:
  1. Set lifecycle configuration for the mybucket on alias 'store' to the rules imported from lifecycle.json
     {{Prompt}} {{.FullName}} store/mybucket < lifecycle.json

  2. Set lifecycle configuration for the mybucket on alias 'store'. User is expected to enter the JSON contents on STDIN
     {{Prompt}} {{.FullName}} store/mybucket
`,
}

type ilmImportMessage struct {
	Status string `json:"status"`
	Target string `json:"target"`
}

func (i ilmImportMessage) String() string {
	return console.Colorize(ilmThemeResultSuccess, "Lifecycle configuration imported successfully to `"+i.Target+"`.")
}

func (i ilmImportMessage) JSON() string {
	msgBytes, e := json.MarshalIndent(i, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(msgBytes)
}

// readILMConfig read from stdin, returns XML.
func readILMConfig() (*lifecycle.Configuration, *probe.Error) {
	// User is expected to enter the lifecycleConfiguration instance contents in JSON format
	var cfg = lifecycle.NewConfiguration()

	// Consume json from STDIN
	dec := json.NewDecoder(os.Stdin)
	if e := dec.Decode(cfg); e != nil {
		return cfg, probe.NewError(e)
	}

	return cfg, nil
}

// checkILMImportSyntax - validate arguments passed by user
func checkILMImportSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "import", globalErrorExitStatus)
	}
}

func mainILMImport(cliCtx *cli.Command) error {
	ctx, cancelILMImport := context.WithCancel(globalContext)
	defer cancelILMImport()

	checkILMImportSyntax(cliCtx)
	setILMDisplayColorScheme()

	args := cliCtx.Args().Slice()
	urlStr := argumentAt(args, 0)

	client, err := newClient(urlStr)
	fatalIf(err.Trace(urlStr), "Unable to initialize client for "+urlStr)

	ilmCfg, err := readILMConfig()
	fatalIf(err.Trace(args...), "Unable to read ILM configuration")

	if len(ilmCfg.Rules) == 0 {
		// Abort here, otherwise client.SetLifecycle will remove the lifecycle configuration
		// since no rules are provided and we will show a success message.
		fatalIf(errDummy(), "The provided ILM configuration does not contain any rule, aborting.")
	}

	fatalIf(client.SetLifecycle(ctx, ilmCfg).Trace(urlStr), "Unable to set new lifecycle rules")

	printMsg(ilmImportMessage{
		Status: "success",
		Target: urlStr,
	})
	return nil
}
