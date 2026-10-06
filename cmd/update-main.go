/*
 * MinIO Cloud Storage, (C) 2015, 2016, 2017 MinIO, Inc.
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

import "github.com/minio/cli"

const selfUpdateDisabledMessage = "OC self-update is disabled until an independent release channel is available; install a reviewed OC release manually."

// Keep the command so existing scripts receive an explicit failure, without
// checking a remote release or replacing this executable with MinIO mc.
var updateCmd = cli.Command{
	Name:         "update",
	Usage:        "self-update is disabled; install an OC release manually",
	Action:       mainUpdate,
	OnUsageError: onUsageError,
	Flags:        []cli.Flag{cli.BoolFlag{Name: "json", Usage: "enable JSON lines formatted output"}},
}

func mainUpdate(ctx *cli.Context) error {
	return cli.NewExitError(selfUpdateDisabledMessage, 1)
}
