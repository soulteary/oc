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
	"path/filepath"
	"strings"
	"time"

	"github.com/fatih/color"
	"github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

var (
	lhSetFlags = []cli.Flag{
		&cli.BoolFlag{
			Name: "recursive", Aliases: []string{"r"},
			Usage: "apply legal hold recursively",
		},
		&cli.StringFlag{
			Name: "version-id", Aliases: []string{"vid"},
			Usage: "apply legal hold to a specific object version",
		},
		&cli.StringFlag{
			Name:  "rewind",
			Usage: "apply legal hold on an object version at specified time",
		},
		&cli.BoolFlag{
			Name:  "versions",
			Usage: "apply legal hold on multiple versions of an object",
		},
	}
)
var legalHoldSetCmd = &cli.Command{
	Name:         "set",
	Usage:        "set legal hold for object(s)",
	Action:       commandAction(mainLegalHoldSet),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        append(lhSetFlags, globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} [FLAGS] TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}

EXAMPLES:
   1. Enable legal hold on a specific object
      $ {{.FullName}} store/mybucket/prefix/obj.csv

   2. Enable legal hold on a specific object version
      $ {{.FullName}} store/mybucket/prefix/obj.csv --version-id "HiMFUTOowG6ylfNi4LKxD3ieHbgfgrvC"

   3. Enable object legal hold recursively for all objects at a prefix
      $ {{.FullName}} store/mybucket/prefix --recursive

   4. Enable object legal hold recursively for all objects versions older than one year
      $ {{.FullName}} store/mybucket/prefix --recursive --rewind 365d --versions
`,
}

// setLegalHold - Set legalhold for all objects within a given prefix.
func setLegalHold(ctx context.Context, urlStr, versionID string, timeRef time.Time, withOlderVersions, recursive bool, lhold minio.LegalHoldStatus) error {

	clnt, err := newClient(urlStr)
	if err != nil {
		fatalIf(err.Trace(), "Unable to parse the provided url.")
	}

	prefixPath := clnt.GetURL().Path
	prefixPath = filepath.ToSlash(prefixPath)
	if !strings.HasSuffix(prefixPath, "/") {
		prefixPath = prefixPath[:strings.LastIndex(prefixPath, "/")+1]
	}
	prefixPath = strings.TrimPrefix(prefixPath, "./")

	if !recursive && !withOlderVersions {
		err = clnt.PutObjectLegalHold(ctx, versionID, lhold)
		if err != nil {
			errorIf(err.Trace(urlStr), "Failed to set legal hold on `%s` successfully", urlStr)
		} else {
			contentURL := filepath.ToSlash(clnt.GetURL().Path)
			key := strings.TrimPrefix(contentURL, prefixPath)

			printMsg(legalHoldCmdMessage{
				LegalHold: lhold,
				Status:    "success",
				URLPath:   clnt.GetURL().String(),
				Key:       key,
				VersionID: versionID,
			})
		}
		return nil
	}

	alias, _, _ := mustExpandAlias(urlStr)
	var cErr error
	objectsFound := false
	lstOptions := ListOptions{Recursive: recursive, ShowDir: DirNone}
	if !timeRef.IsZero() {
		lstOptions.WithOlderVersions = withOlderVersions
		lstOptions.TimeRef = timeRef
	}
	for content := range clnt.List(ctx, lstOptions) {
		if content.Err != nil {
			errorIf(content.Err.Trace(clnt.GetURL().String()), "Unable to list folder.")
			cErr = exitStatus(globalErrorExitStatus) // Set the exit status.
			continue
		}

		if !recursive && alias+getKey(content) != getStandardizedURL(urlStr) {
			break
		}

		objectsFound = true

		newClnt, perr := newClientFromAlias(alias, content.URL.String())
		if perr != nil {
			errorIf(content.Err.Trace(clnt.GetURL().String()), "Invalid URL")
			continue
		}

		probeErr := newClnt.PutObjectLegalHold(ctx, content.VersionID, lhold)
		if probeErr != nil {
			errorIf(probeErr.Trace(content.URL.Path), "Failed to set legal hold on `%s` successfully", content.URL.Path)
		} else {
			if !globalJSON {
				contentURL := filepath.ToSlash(content.URL.Path)
				key := strings.TrimPrefix(contentURL, prefixPath)

				printMsg(legalHoldCmdMessage{
					LegalHold: lhold,
					Status:    "success",
					URLPath:   content.URL.String(),
					Key:       key,
					VersionID: content.VersionID,
				})
			}
		}
	}

	if cErr == nil && !globalJSON {
		if !objectsFound {
			console.Print(console.Colorize("LegalHoldMessageFailure",
				fmt.Sprintf("No objects/versions found while setting legal hold on `%s`. \n", urlStr)))
		}
	}
	return cErr
}

// Validate command line arguments.
func parseLegalHoldArgs(cliCtx *cli.Command) (targetURL, versionID string, timeRef time.Time, recursive, withVersions bool) {
	args := cliCtx.Args().Slice()
	if len(args) != 1 {
		cli.ShowCommandHelpAndExit(context.Background(), cliCtx, cliCtx.Name, 1)
	}

	targetURL = args[0]
	if targetURL == "" {
		fatalIf(errInvalidArgument(), "You cannot pass an empty target url.")
	}

	versionID = cliCtx.String("version-id")
	recursive = cliCtx.Bool("recursive")
	withVersions = cliCtx.Bool("versions")
	rewind := cliCtx.String("rewind")

	if versionID != "" && (recursive || withVersions || rewind != "") {
		fatalIf(errInvalidArgument(), "You cannot pass --version-id with any of --versions, --recursive and --rewind flags.")
	}

	timeRef = parseRewindFlag(rewind)
	return
}

// main for legalhold set command.
func mainLegalHoldSet(cliCtx *cli.Command) error {
	console.SetColor("LegalHoldSuccess", color.New(color.FgGreen, color.Bold))
	console.SetColor("LegalHoldFailure", color.New(color.FgRed, color.Bold))
	console.SetColor("LegalHoldPartialFailure", color.New(color.FgRed, color.Bold))
	console.SetColor("LegalHoldMessageFailure", color.New(color.FgYellow))

	targetURL, versionID, timeRef, recursive, withVersions := parseLegalHoldArgs(cliCtx)
	if timeRef.IsZero() && withVersions {
		timeRef = time.Now().UTC()
	}

	ctx, cancelLegalHold := context.WithCancel(globalContext)
	defer cancelLegalHold()

	enabled, err := isBucketLockEnabled(ctx, targetURL)
	if err != nil {
		fatalIf(err, "Unable to set legalhold on `%s`", targetURL)
	}
	if !enabled {
		fatalIf(errDummy().Trace(), "Bucket lock needs to be enabled in order to use this feature.")
	}

	return setLegalHold(ctx, targetURL, versionID, timeRef, withVersions, recursive, minio.LegalHoldEnabled)
}
