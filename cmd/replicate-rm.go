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
	"errors"

	"github.com/fatih/color"
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio-sdk/v7/pkg/replication"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var replicateRemoveFlags = []cli.Flag{
	&cli.StringFlag{
		Name:  "id",
		Usage: "id for the rule, should be a unique value",
	},
	&cli.BoolFlag{
		Name:  "force",
		Usage: "force remove all the replication configuration rules on the bucket",
	},
	&cli.BoolFlag{
		Name:  "all",
		Usage: "remove all replication configuration rules of the bucket, force flag enforced",
	},
}

var replicateRemoveCmd = &cli.Command{
	Name:         "rm",
	Usage:        "remove a server side replication configuration rule",
	Action:       commandAction(mainReplicateRemove),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        append(globalFlags, replicateRemoveFlags...),
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}
   
USAGE:
  {{.FullName}} TARGET
	   
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Remove replication configuration rule on bucket "mybucket" for alias "store" with rule id "bsib5mgt874bi56l0fmg".
     {{Prompt}} {{.FullName}} --id "bsib5mgt874bi56l0fmg" store/mybucket

  2. Remove all the replication configuration rules on bucket "mybucket" for alias "store". --force flag is required.
     {{Prompt}} {{.FullName}} --all --force store/mybucket
`,
}

// checkReplicateRemoveSyntax - validate all the passed arguments
func checkReplicateRemoveSyntax(ctx *cli.Command) {
	if ctx.Args().Len() != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "rm", 1) // last argument is exit code
	}
	rmAll := ctx.Bool("all")
	rmForce := ctx.Bool("force")
	rID := ctx.String("id")

	rmChk := (rmAll && rmForce) || (!rmAll && !rmForce)
	if !rmChk {
		fatalIf(errInvalidArgument(),
			"It is mandatory to specify --all and --force flag together for mc "+ctx.FullName()+".")
	}
	if rmAll && rmForce {
		return
	}

	if rID == "" {
		fatalIf(errInvalidArgument().Trace(rID), "rule ID cannot be empty")
	}
}

type replicateRemoveMessage struct {
	Op     string `json:"op"`
	Status string `json:"status"`
	URL    string `json:"url"`
	ID     string `json:"id"`
}

func (l replicateRemoveMessage) JSON() string {
	l.Status = "success"
	jsonMessageBytes, e := json.MarshalIndent(l, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(jsonMessageBytes)
}

func (l replicateRemoveMessage) String() string {
	if l.ID != "" {
		return console.Colorize("replicateRemoveMessage", "Replication configuration rule with ID `"+l.ID+"`removed from "+l.URL+".")
	}
	return console.Colorize("replicateRemoveMessage", "Replication configuration removed from "+l.URL+" successfully.")
}

func mainReplicateRemove(cliCtx *cli.Command) error {
	ctx, cancelReplicateRemove := context.WithCancel(globalContext)
	defer cancelReplicateRemove()

	console.SetColor("replicateRemoveMessage", color.New(color.FgGreen))

	checkReplicateRemoveSyntax(cliCtx)

	// Get the alias parameter from cli
	args := cliCtx.Args().Slice()
	aliasedURL := argumentAt(args, 0)
	// Create a new Client
	client, err := newClient(aliasedURL)
	fatalIf(err, "Unable to initialize connection.")
	rcfg, err := client.GetReplication(ctx)
	fatalIf(err.Trace(args...), "Unable to get replication configuration")

	if rcfg.Empty() {
		fatalIf(probe.NewError(errors.New("replication configuration not set")).Trace(aliasedURL),
			"Unable to remove replication configuration")
	}
	rmAll := cliCtx.Bool("all")
	rmForce := cliCtx.Bool("force")
	ruleID := cliCtx.String("id")
	if rmAll && rmForce {
		fatalIf(client.RemoveReplication(ctx), "Unable to remove replication configuration")
	} else {
		opts := replication.Options{
			ID: ruleID,
			Op: replication.RemoveOption,
		}
		fatalIf(client.SetReplication(ctx, &rcfg, opts), "Could not remove replication rule")
	}
	printMsg(replicateRemoveMessage{
		Op:     "rm",
		Status: "success",
		URL:    aliasedURL,
		ID:     ruleID,
	})
	return nil
}
