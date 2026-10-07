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
	"fmt"
	"strings"
	"time"

	"github.com/fatih/color"
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

const logTimeFormat string = "15:04:05 MST 01/02/2006"

var adminConsoleFlags = []cli.Flag{
	&cli.IntFlag{
		Name: "limit", Aliases: []string{"l"},
		Usage: "show last n log entries",
		Value: 10,
	},
	&cli.StringFlag{
		Name: "type", Aliases: []string{"t"},
		Usage: "list error logs by type. Valid options are '[otterio, application, all]' (minio remains an alias)",
		Value: "all",
	},
}

var adminConsoleCmd = &cli.Command{
	Name:            "console",
	Usage:           "show console logs for OtterIO server",
	Action:          commandAction(mainAdminConsole),
	OnUsageError:    onUsageError,
	Before:          commandBefore(setGlobalsFromContext),
	Flags:           append(adminConsoleFlags, globalFlags...),
	HideHelpCommand: true,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} [FLAGS] TARGET [NODENAME]

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Show console logs for a OtterIO server with alias 'store'
     {{Prompt}} {{.FullName}} play

  2. Show last 5 log entries for node 'node1' on OtterIO server with alias 'cluster1'
     {{Prompt}} {{.FullName}} --limit 5 cluster1 node1

  3. Show application error logs on OtterIO server with alias 'store'
     {{Prompt}} {{.FullName}} --type application play
`,
}

func checkAdminLogSyntax(ctx *cli.Command) {
	if ctx.Args().Len() == 0 || ctx.Args().Len() > 3 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "console", 1) // last argument is exit code
	}
}

// Extend madmin.LogInfo to add String() and JSON() methods
type logMessage struct {
	Status string `json:"status"`
	madmin.LogInfo
}

// JSON - jsonify loginfo
func (l logMessage) JSON() string {
	l.Status = "success"
	logJSON, err := json.MarshalIndent(&l, "", " ")
	fatalIf(probe.NewError(err), "Unable to marshal into JSON.")

	return string(logJSON)

}
func getLogTime(lt string) string {
	tm, err := time.Parse(time.RFC3339Nano, lt)
	if err != nil {
		return lt
	}
	return tm.Format(logTimeFormat)
}

// String - return colorized loginfo as string.
func (l logMessage) String() string {
	var hostStr string
	var b = &strings.Builder{}
	if l.NodeName != "" {
		hostStr = fmt.Sprintf("%s ", colorizedNodeName(l.NodeName))
	}
	log := l.LogInfo
	if log.ConsoleMsg != "" {
		if strings.HasPrefix(log.ConsoleMsg, "\n") {
			fmt.Fprintf(b, "%s\n", hostStr)
			log.ConsoleMsg = strings.TrimPrefix(log.ConsoleMsg, "\n")
		}
		fmt.Fprintf(b, "%s %s", hostStr, log.ConsoleMsg)
		return b.String()
	}
	if l.API != nil {
		apiString := "API: " + l.API.Name + "("
		if l.API.Args != nil && l.API.Args.Bucket != "" {
			apiString = apiString + "bucket=" + l.API.Args.Bucket
		}
		if l.API.Args != nil && l.API.Args.Object != "" {
			apiString = apiString + ", object=" + l.API.Args.Object
		}
		apiString += ")"
		fmt.Fprintf(b, "\n%s %s", hostStr, console.Colorize("API", apiString))
	}
	if l.Time != "" {
		fmt.Fprintf(b, "\n%s Time: %s", hostStr, getLogTime(l.Time))
	}
	if l.DeploymentID != "" {
		fmt.Fprintf(b, "\n%s DeploymentID: %s", hostStr, l.DeploymentID)
	}
	if l.RequestID != "" {
		fmt.Fprintf(b, "\n%s RequestID: %s", hostStr, l.RequestID)
	}
	if l.RemoteHost != "" {
		fmt.Fprintf(b, "\n%s RemoteHost: %s", hostStr, l.RemoteHost)
	}
	if l.UserAgent != "" {
		fmt.Fprintf(b, "\n%s UserAgent: %s", hostStr, l.UserAgent)
	}
	if l.Trace != nil {
		if l.Trace.Message != "" {
			fmt.Fprintf(b, "\n%s Error: %s", hostStr, console.Colorize("LogMessage", l.Trace.Message))
		}
		if l.Trace.Variables != nil {
			for key, value := range l.Trace.Variables {
				if value != "" {
					fmt.Fprintf(b, "\n%s %s=%s", hostStr, key, value)
				}
			}
		}
		if l.Trace.Source != nil {
			traceLength := len(l.Trace.Source)
			for i, element := range l.Trace.Source {
				fmt.Fprintf(b, "\n%s %8v: %s", hostStr, traceLength-i, element)
			}
		}
	}
	logMsg := strings.TrimPrefix(b.String(), "\n")
	return fmt.Sprintf("%s\n", logMsg)
}

// mainAdminConsole - the entry function of console command
func mainAdminConsole(ctx *cli.Command) error {
	// Check for command syntax
	checkAdminLogSyntax(ctx)
	console.SetColor("LogMessage", color.New(color.Bold, color.FgRed))
	console.SetColor("Api", color.New(color.Bold, color.FgWhite))
	for _, c := range colors {
		console.SetColor(fmt.Sprintf("Node%d", c), color.New(c))
	}
	aliasedURL := ctx.Args().Get(0)
	var node string
	if ctx.Args().Len() > 1 {
		node = ctx.Args().Get(1)
	}
	var limit int
	if ctx.IsSet("limit") {
		limit = ctx.Int("limit")
		if limit <= 0 {
			fatalIf(errInvalidArgument().Trace(ctx.Args().Slice()...), "please set a proper limit, for example: '--limit 5' to display last 5 logs, omit this flag to display all available logs")
		}
	}
	logType, logErr := normalizeConsoleLogType(ctx.String("type"))
	fatalIf(probe.NewError(logErr), "Invalid log type.")
	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	if err != nil {
		fatalIf(err.Trace(aliasedURL), "Unable to initialize admin client.")
		return nil
	}

	ctxt, cancel := context.WithCancel(globalContext)
	defer cancel()

	// Start listening on all console log activity.
	stream, cleanup := consoleLogs(ctxt, client, node, limit, logType)
	defer cleanup()
	logCh := stream.records
	for logInfo := range logCh {
		if ctxt.Err() != nil {
			return nil
		}
		if logInfo.Err != nil {
			errorIf(probe.NewError(logInfo.Err), "Unable to listen to console logs.")
			return logInfo.Err
		}
		// drop nodeName from output if specified as cli arg
		if node != "" {
			logInfo.NodeName = ""
		}
		printMsg(logMessage{LogInfo: logInfo})
	}
	if ctxt.Err() != nil {
		return ctxt.Err()
	}
	var streamErr error
	select {
	case streamErr = <-stream.failures:
	default:
		streamErr = fmt.Errorf("console log stream closed unexpectedly")
	}
	errorIf(probe.NewError(streamErr), "Unable to listen to console logs.")
	return streamErr
}

func normalizeConsoleLogType(value string) (string, error) {
	value = strings.ToLower(value)
	if value == "minio" {
		value = "otterio"
	}
	switch value {
	case "otterio", "application", "all":
		return value, nil
	default:
		return "", fmt.Errorf("valid log types are otterio, application and all")
	}
}
