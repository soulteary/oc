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

import "github.com/urfave/cli/v3"

var encryptSubcommands = []*cli.Command{
	encryptSetCmd,
	encryptClearCmd,
	encryptInfoCmd,
}

var encryptCmd = &cli.Command{
	Name:            "encrypt",
	Usage:           "manage bucket encryption config",
	HideHelpCommand: true,
	Action:          commandAction(mainEncrypt),
	Before:          commandBefore(setGlobalsFromContext),
	Flags:           globalFlags,
	Commands:        encryptSubcommands,
}

// mainEncrypt is the handle for "mc encrypt" command.
func mainEncrypt(ctx *cli.Command) error {
	commandNotFound(ctx, encryptSubcommands)
	return nil
	// Sub-commands like "info", "set", "clear" have their own main.
}
