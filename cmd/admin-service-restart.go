/*
 * MinIO Client (C) 2016 MinIO, Inc.
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
	"time"

	"github.com/fatih/color"
	"github.com/minio/cli"
	json "github.com/soulteary/mc/pkg/colorjson"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"
)

var adminServiceRestartCmd = cli.Command{
	Name:         "restart",
	Usage:        "restart all OtterIO servers",
	Action:       mainAdminServiceRestart,
	OnUsageError: onUsageError,
	Before:       setGlobalsFromContext,
	Flags:        append([]cli.Flag{cli.DurationFlag{Name: "timeout", Value: time.Minute, Usage: "maximum time to wait for restart readiness"}}, globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Restart OtterIO server represented by its alias 'store'.
     {{.Prompt}} {{.HelpName}} store/
`,
}

// serviceRestartCommand is container for service restart command success and failure messages.
type serviceRestartCommand struct {
	Status    string `json:"status"`
	ServerURL string `json:"serverURL"`
}

// String colorized service restart command message.
func (s serviceRestartCommand) String() string {
	return console.Colorize("ServiceRestart", "Restart command successfully sent to `"+s.ServerURL+"`. Type Ctrl-C or wait to see the status of the restart.")
}

// JSON jsonified service restart command message.
func (s serviceRestartCommand) JSON() string {
	serviceRestartJSONBytes, e := json.MarshalIndent(s, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(serviceRestartJSONBytes)
}

// serviceRestartMessage is container for service restart success and failure messages.
type serviceRestartMessage struct {
	Status    string `json:"status"`
	ServerURL string `json:"serverURL"`
	Err       error  `json:"error,omitempty"`
}

// String colorized service restart message.
func (s serviceRestartMessage) String() string {
	if s.Err == nil {
		return console.Colorize("ServiceRestart", "\nRestarted `"+s.ServerURL+"` successfully.")
	}
	return console.Colorize("FailedServiceRestart", "Failed to restart `"+s.ServerURL+"`. error: "+s.Err.Error())
}

// JSON jsonified service restart message.
func (s serviceRestartMessage) JSON() string {
	serviceRestartJSONBytes, e := json.MarshalIndent(s, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")

	return string(serviceRestartJSONBytes)
}

// checkAdminServiceRestartSyntax - validate all the passed arguments
func checkAdminServiceRestartSyntax(ctx *cli.Context) {
	if len(ctx.Args()) == 0 || len(ctx.Args()) > 2 {
		cli.ShowCommandHelpAndExit(ctx, "restart", 1) // last argument is exit code
	}
}

func mainAdminServiceRestart(ctx *cli.Context) error {

	// Validate serivce restart syntax.
	checkAdminServiceRestartSyntax(ctx)

	// Set color.
	console.SetColor("ServiceOffline", color.New(color.FgRed, color.Bold))
	console.SetColor("ServiceInitializing", color.New(color.FgYellow, color.Bold))
	console.SetColor("ServiceRestart", color.New(color.FgGreen, color.Bold))
	console.SetColor("FailedServiceRestart", color.New(color.FgRed, color.Bold))

	// Get the alias parameter from cli
	args := ctx.Args()
	aliasedURL := args.Get(0)

	client, err := newAdminClient(aliasedURL)
	fatalIf(err, "Unable to initialize admin connection.")

	if ctx.Duration("timeout") <= 0 {
		fatalIf(probe.NewError(fmt.Errorf("restart timeout must be positive")), "Invalid restart timeout.")
	}
	waitCtx, waitCancel := context.WithTimeout(globalContext, ctx.Duration("timeout"))
	defer waitCancel()

	before, beforeErr := client.ServerInfo(waitCtx)
	fatalIf(probe.NewError(beforeErr), "Unable to capture server identity before restart.")
	if len(before.Servers) == 0 {
		fatalIf(probe.NewError(fmt.Errorf("server returned no instances")), "Cannot confirm restart identity.")
	}
	observedAt := time.Now()
	// Uptime is reported in whole seconds. Allow newly started instances to
	// age before restarting so the old and new boot intervals cannot overlap.
	for _, server := range before.Servers {
		if server.Uptime < 2 {
			timer := time.NewTimer(2 * time.Second)
			select {
			case <-timer.C:
			case <-waitCtx.Done():
				timer.Stop()
				fatalIf(probe.NewError(waitCtx.Err()), "Restart was not sent: identity observation timed out.")
			}
			break
		}
	}

	// Restart the specified OtterIO server
	fatalIf(probe.NewError(client.ServiceRestart(waitCtx)), "Unable to restart the server.")

	// Success..
	printMsg(serviceRestartCommand{Status: "success", ServerURL: aliasedURL})

	coloring := color.New(color.FgRed)
	mark := "."

	// Print restart progress
	printProgress := func() {
		if !globalQuiet && !globalJSON {
			coloring.Print(mark)
		}
	}

	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-waitCtx.Done():
			fatalIf(probe.NewError(waitCtx.Err()), "Restart readiness was not confirmed; inspect the server before retrying.")
			return waitCtx.Err()
		case <-ticker.C:
			ctx, cancel := context.WithTimeout(waitCtx, time.Second)
			// Fetch the service status of the specified OtterIO server
			info, e := client.ServerInfo(ctx)
			cancel()
			switch {
			case e == nil && restartedServers(before, info, time.Since(observedAt)):
				printMsg(serviceRestartMessage{Status: "success", ServerURL: aliasedURL})
				return nil
			case e == nil && info.Mode == string(madmin.ItemInitializing):
				coloring = color.New(color.FgYellow)
				mark = "!"
				fallthrough
			default:
				printProgress()
			}
		}
	}
}

// A ready response alone can belong to the old process. Confirm that every
// original endpoint reports a boot interval later than the observed one.
func restartedServers(before, after madmin.InfoMessage, elapsed time.Duration) bool {
	if after.Mode != string(madmin.ItemOnline) || len(before.Servers) == 0 || len(before.Servers) != len(after.Servers) {
		return false
	}
	old := make(map[string]int64, len(before.Servers))
	for _, server := range before.Servers {
		if server.Endpoint == "" || server.Uptime < 0 {
			return false
		}
		old[server.Endpoint] = server.Uptime
	}
	if len(old) != len(before.Servers) {
		return false
	}
	for _, server := range after.Servers {
		uptime, ok := old[server.Endpoint]
		if !ok || server.State != string(madmin.ItemOnline) || server.Uptime < 0 || float64(uptime)+elapsed.Seconds()-float64(server.Uptime) < 2 {
			return false
		}
		delete(old, server.Endpoint)
	}
	return len(old) == 0
}
