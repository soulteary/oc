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

	"github.com/fatih/color"
	"github.com/minio/minio-go/v7/pkg/replication"
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var replicateExportCmd = &cli.Command{
	Name:         "export",
	Usage:        "export server side replication configuration",
	Action:       commandAction(mainReplicateExport),
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
  1. Print replication configuration on bucket "mybucket" for alias "store" to STDOUT.
     {{Prompt}} {{.FullName}} store/mybucket

  2. Export replication configuration on bucket "mybucket" for alias "store" to '/data/replicate/config'.
     {{Prompt}} {{.FullName}} store/mybucket > /data/replicate/config
`,
}

// checkReplicateExportSyntax - validate all the passed arguments
func checkReplicateExportSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "export", 1) // last argument is exit code
	}
}

type replicateExportMessage struct {
	Op                string             `json:"op"`
	Status            string             `json:"status"`
	URL               string             `json:"url"`
	ReplicationConfig replication.Config `json:"config"`
}

func (r replicateExportMessage) JSON() string {
	r.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(r, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(jsonMessageBytes)
}

func (r replicateExportMessage) String() string {
	if r.ReplicationConfig.Empty() {
		return console.Colorize("ReplicateNMessage", "No replication configuration found for "+r.URL+".")
	}
	msgBytes, e := json.MarshalIndent(r.ReplicationConfig, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal replication configuration")
	return string(msgBytes)
}

func mainReplicateExport(cliCtx *cli.Command) error {
	ctx, cancelReplicateExport := context.WithCancel(globalContext)
	defer cancelReplicateExport()

	console.SetColor("replicateExportMessage", color.New(color.FgGreen))
	console.SetColor("replicateExportFailure", color.New(color.FgRed))

	checkReplicateExportSyntax(cliCtx)

	// Get the alias parameter from cli
	args := cliCtx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	// Create a new Client
	client, err := newClient(aliasedURL)
	fatalIf(err, "Unable to initialize connection.")
	rCfg, err := client.GetReplication(ctx)
	fatalIf(err.Trace(args...), "Unable to get replication configuration")
	printMsg(replicateExportMessage{
		Op:                "export",
		Status:            "success",
		URL:               aliasedURL,
		ReplicationConfig: rCfg,
	})
	return nil
}
