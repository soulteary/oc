/*
 * MinIO Client (C) 2018 MinIO, Inc.
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
	"strings"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio-sdk/v7/pkg/set"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"
	"github.com/urfave/cli/v3"
)

var adminProfileStartFlags = []cli.Flag{
	&cli.StringFlag{
		Name:  "type",
		Usage: "start profiler type, possible values are 'cpu', 'mem', 'block', 'mutex', 'trace', 'threads' and 'goroutines'",
		Value: "cpu,mem,block,goroutines",
	},
}

var adminProfileStartCmd = &cli.Command{
	Name:            "start",
	Usage:           "start recording profile data",
	Action:          commandAction(mainAdminProfileStart),
	OnUsageError:    onUsageError,
	Before:          commandBefore(setGlobalsFromContext),
	Flags:           append(adminProfileStartFlags, globalFlags...),
	HideHelpCommand: true,
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} [FLAGS] TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
    1. Start CPU profiling only
       {{Prompt}} {{.FullName}} --type cpu store/

    2. Start CPU, Memory and Block profiling concurrently
       {{Prompt}} {{.FullName}} --type cpu,mem,block store/
`,
}

func checkAdminProfileStartSyntax(ctx *cli.Command) {
	// Check flags combinations
	if ctx.Args().Len() != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "start", 1) // last argument is exit code
	}

	s := set.NewStringSet()
	supportedProfilerTypes := []madmin.ProfilerType{
		madmin.ProfilerCPU,
		madmin.ProfilerMEM,
		madmin.ProfilerBlock,
		madmin.ProfilerMutex,
		madmin.ProfilerTrace,
		madmin.ProfilerThreads,
		madmin.ProfilerGoroutines,
	}
	for _, profilerType := range supportedProfilerTypes {
		s.Add(string(profilerType))
	}
	// Check if the provided profiler type is known and supported
	supportedProfiler := false
	profilers := strings.Split(strings.ToLower(ctx.String("type")), ",")
	for _, profiler := range profilers {
		if profiler != "" {
			if s.Contains(profiler) {
				supportedProfiler = true
				break
			}
		}
	}
	if !supportedProfiler {
		fatalIf(errDummy().Trace(ctx.String("type")),
			"Profiler type unrecognized. Possible values are: %v.", supportedProfilerTypes)
	}
}

// mainAdminProfileStart - the entry function of profile command
func mainAdminProfileStart(ctx *cli.Command) error {
	// Check for command syntax
	checkAdminProfileStartSyntax(ctx)

	// Get the alias parameter from cli
	args := ctx.Args().Slice()
	aliasedURL := argumentAt(args, 0)

	profilers := ctx.String("type")

	// Create a new MinIO Admin Client
	client, err := newAdminClient(aliasedURL)
	if err != nil {
		fatalIf(err.Trace(aliasedURL), "Unable to initialize admin client.")
		return nil
	}

	// Start profile
	_, cmdErr := client.StartProfiling(globalContext, madmin.ProfilerType(profilers))
	fatalIf(probe.NewError(cmdErr), "Unable to start profile.")

	console.Infoln("Profile data successfully started.")
	return nil
}
