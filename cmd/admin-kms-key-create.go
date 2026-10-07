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
	"os"

	"github.com/fatih/color"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
	"golang.org/x/term"
)

var adminKMSCreateKeyCmd = &cli.Command{
	Name:         "create",
	Usage:        "creates a new master key at the KMS",
	Action:       commandAction(mainAdminKMSCreateKey),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        globalFlags,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} TARGET [KEY_NAME]

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Create a new master key named 'my-key' default master key.
     $ {{.FullName}} play my-key
`,
}

// adminKMSCreateKeyCmd is the handler for the "mc admin kms key create" command.
func mainAdminKMSCreateKey(ctx *cli.Command) error {
	if ctx.Args().Len() != 2 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "create", 1) // last argument is exit code
	}

	client, err := newAdminClient(ctx.Args().Get(0))
	fatalIf(err, "Cannot get a configured admin connection.")

	keyID := ctx.Args().Get(1)
	e := client.CreateKey(globalContext, keyID)
	fatalIf(probe.NewError(e), "Failed to create master key")

	if term.IsTerminal(int(os.Stdout.Fd())) {
		console.Println(color.GreenString(fmt.Sprintf("Created master key `%s` successfully", keyID)))
	}
	return nil
}
