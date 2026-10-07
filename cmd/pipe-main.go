/*
 * MinIO Client, (C) 2015, 2016, 2017 MinIO, Inc.
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
	"os"
	"syscall"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/urfave/cli/v3"
)

var (
	pipeFlags = []cli.Flag{
		&cli.StringFlag{
			Name:  "encrypt",
			Usage: "encrypt objects (using server-side encryption with server managed keys)",
		},
		&cli.StringFlag{
			Name: "storage-class", Aliases: []string{"sc"},
			Usage: "set storage class for new object(s) on target",
		},
	}
)

// Display contents of a file.
var pipeCmd = &cli.Command{
	Name:         "pipe",
	Usage:        "stream STDIN to an object",
	Action:       commandAction(mainPipe),
	OnUsageError: onUsageError,
	Before:       commandBefore(setGlobalsFromContext),
	Flags:        append(append(pipeFlags, ioFlags...), globalFlags...),
	CustomHelpTemplate: `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}} [FLAGS] [TARGET]
{{if .VisibleFlags}}
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
ENVIRONMENT VARIABLES:
  OC_ENCRYPT (MC_ENCRYPT):      list of comma delimited prefix values
  OC_ENCRYPT_KEY (MC_ENCRYPT_KEY):  list of comma delimited prefix=secret values

EXAMPLES:
  1. Write contents of stdin to a file on local filesystem.
     {{Prompt}} {{.FullName}} /tmp/hello-world.go

  2. Write contents of stdin to an object on Amazon S3 cloud storage.
     {{Prompt}} {{.FullName}} s3/personalbuck/meeting-notes.txt

  3. Copy an ISO image to an object on Amazon S3 cloud storage.
     {{Prompt}} cat debian-8.2.iso | {{.FullName}} s3/opensource-isos/gnuos.iso

  4. Stream MySQL database dump to Amazon S3 directly.
     {{Prompt}} mysqldump -u root -p ******* accountsdb | {{.FullName}} s3/sql-backups/backups/accountsdb-oct-9-2015.sql

  5. Write contents of stdin to an object on Amazon S3 cloud storage and assign REDUCED_REDUNDANCY storage-class to the uploaded object.
     {{Prompt}} {{.FullName}} --storage-class REDUCED_REDUNDANCY s3/personalbuck/meeting-notes.txt
`,
}

func pipe(targetURL string, encKeyDB map[string][]prefixSSEPair, storageClass string) *probe.Error {
	input, closeInput, inputErr := openPipeInput(globalContext)
	if inputErr != nil {
		return probe.NewError(inputErr)
	}
	defer closeInput()
	if targetURL == "" {
		// When no target is specified, pipe cat's stdin to stdout.
		return catOut(input, -1).Trace()
	}
	alias, _ := url2Alias(targetURL)
	sseKey := getSSE(targetURL, encKeyDB[alias])

	// Stream from stdin to multiple objects until EOF.
	// Ignore size, since os.Stat() would not return proper size all the time
	// for local filesystem for example /proc files.
	opts := PutOptions{
		sse:          sseKey,
		storageClass: storageClass,
	}
	_, err := putTargetStreamWithURL(globalContext, targetURL, input, -1, opts)
	// TODO: See if this check is necessary.
	switch e := err.ToGoError().(type) {
	case *os.PathError:
		if e.Err == syscall.EPIPE {
			// stdin closed by the user. Gracefully exit.
			return nil
		}
	}
	return err.Trace(targetURL)
}

// check pipe input arguments.
func checkPipeSyntax(ctx *cli.Command) {
	if ctx.Args().Len() > 1 {
		cli.ShowCommandHelpAndExit(context.Background(), ctx, "pipe", 1) // last argument is exit code.
	}
}

// mainPipe is the main entry point for pipe command.
func mainPipe(ctx *cli.Command) error {
	// Parse encryption keys per command.
	encKeyDB, err := getEncKeys(ctx)
	fatalIf(err, "Unable to parse encryption keys.")

	// validate pipe input arguments.
	checkPipeSyntax(ctx)

	if ctx.Args().Len() == 0 {
		err = pipe("", nil, ctx.String("storage-class"))
		if globalContext.Err() != nil {
			reportPipeCleanupError(err)
			return globalContext.Err()
		}
		fatalIf(err.Trace("stdout"), "Unable to write to one or more targets.")
	} else {
		// extract URLs.
		URLs := ctx.Args().Slice()
		err = pipe(URLs[0], encKeyDB, ctx.String("storage-class"))
		if globalContext.Err() != nil {
			reportPipeCleanupError(err)
			return globalContext.Err()
		}
		fatalIf(err.Trace(URLs[0]), "Unable to write to one or more targets.")
	}

	// Done.
	return nil
}

// Signal cancellation must not hide a failed best-effort server-side cleanup.
func reportPipeCleanupError(err *probe.Error) {
	if err == nil {
		return
	}
	var cleanup multipartCleanupError
	if errors.As(err.ToGoError(), &cleanup) {
		errorIf(probe.NewError(cleanup), "Unable to clean up canceled multipart upload.")
	}
}
