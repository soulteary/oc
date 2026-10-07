/*
 * MinIO Client (C) 2015, 2016 MinIO, Inc.
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
	"crypto/sha256"
	"fmt"
	"io"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/wildcard"
	"github.com/urfave/cli/v3"
)

//
//   * MIRROR ARGS - VALID CASES
//   =========================
//   mirror(d1..., d2) -> []mirror(d1/f, d2/d1/f)

// checkMirrorSyntax(URLs []string)
func checkMirrorSyntax(ctx context.Context, cliCtx *cli.Command, encKeyDB map[string][]prefixSSEPair) (srcURL, tgtURL string) {
	if cliCtx.Args().Len() != 2 {
		cli.ShowCommandHelpAndExit(context.Background(), cliCtx, "mirror", 1) // last argument is exit code.
	}

	// extract URLs.
	URLs := cliCtx.Args().Slice()
	srcURL = URLs[0]
	tgtURL = URLs[1]

	if cliCtx.Bool("force") && cliCtx.Bool("remove") {
		errorIf(errInvalidArgument().Trace(URLs...), "`--force` is deprecated, please use `--overwrite` instead with `--remove` for the same functionality.")
	} else if cliCtx.Bool("force") {
		errorIf(errInvalidArgument().Trace(URLs...), "`--force` is deprecated, please use `--overwrite` instead for the same functionality.")
	}

	_, expandedSourcePath, _ := mustExpandAlias(srcURL)
	srcClient := newClientURL(expandedSourcePath)
	_, expandedTargetPath, _ := mustExpandAlias(tgtURL)
	destClient := newClientURL(expandedTargetPath)

	// Mirror with preserve option on windows
	// only works for object storage to object storage
	if runtime.GOOS == "windows" && cliCtx.Bool("a") {
		if srcClient.Type == fileSystem || destClient.Type == fileSystem {
			errorIf(errInvalidArgument(), "Preserve functionality on windows support object storage to object storage transfer only.")
		}
	}

	/****** Generic rules *******/
	if !cliCtx.Bool("watch") && !cliCtx.Bool("active-active") && !cliCtx.Bool("multi-master") {
		_, srcContent, err := url2Stat(ctx, srcURL, "", false, encKeyDB, time.Time{})
		if err != nil {
			fatalIf(err.Trace(srcURL), "Unable to stat source `"+srcURL+"`.")
		}

		if !srcContent.Type.IsDir() {
			fatalIf(errInvalidArgument().Trace(srcContent.URL.String(), srcContent.Type.String()), fmt.Sprintf("Source `%s` is not a folder. Only folders are supported by mirror command.", srcURL))
		}

		if srcClient.Type == fileSystem && !filepath.IsAbs(srcURL) {
			var origSrcURL = srcURL
			var e error
			// Changing relative path to absolute path, if it is a local directory.
			// Save original in case of error
			if srcURL, e = filepath.Abs(srcURL); e != nil {
				srcURL = origSrcURL
			}
		}
	}

	return
}

func matchExcludeOptions(excludeOptions []string, srcSuffix string) bool {
	for _, pattern := range excludeOptions {
		if wildcard.Match(pattern, srcSuffix) {
			return true
		}
	}
	return false
}

func deltaSourceTarget(ctx context.Context, sourceURL, targetURL string, opts mirrorOptions, URLsCh chan<- URLs) {
	send := func(value URLs) bool {
		select {
		case URLsCh <- value:
			return true
		case <-ctx.Done():
			return false
		}
	}

	// source and targets are always directories
	sourceSeparator := string(newClientURL(sourceURL).Separator)
	if !strings.HasSuffix(sourceURL, sourceSeparator) {
		sourceURL = sourceURL + sourceSeparator
	}
	targetSeparator := string(newClientURL(targetURL).Separator)
	if !strings.HasSuffix(targetURL, targetSeparator) {
		targetURL = targetURL + targetSeparator
	}

	// Extract alias and expanded URL
	sourceAlias, sourceURL, _ := mustExpandAlias(sourceURL)
	targetAlias, targetURL, _ := mustExpandAlias(targetURL)

	defer close(URLsCh)

	sourceClnt, err := newClientFromAlias(sourceAlias, sourceURL)
	if err != nil {
		if !send(URLs{Error: err.Trace(sourceAlias, sourceURL)}) {
			return
		}
		return
	}

	targetClnt, err := newClientFromAlias(targetAlias, targetURL)
	if err != nil {
		if !send(URLs{Error: err.Trace(targetAlias, targetURL)}) {
			return
		}
		return
	}

	// List both source and target, compare and return values through channel.
	diffs := difference(ctx, sourceClnt, targetClnt, sourceURL, targetURL, opts.isMetadata, true, (opts.reconcile || opts.verifyContents) && !opts.activeActive, DirNone)
	for diffMsg := range diffs {
		if diffMsg.Error != nil {
			// Send all errors through the channel
			if !send(URLs{Error: diffMsg.Error, ErrorCond: differInUnknown}) {
				return
			}
			continue
		}

		srcSuffix := strings.TrimPrefix(diffMsg.FirstURL, sourceURL)
		//Skip the source object if it matches the Exclude options provided
		if matchExcludeOptions(opts.excludeOptions, srcSuffix) {
			continue
		}

		tgtSuffix := strings.TrimPrefix(diffMsg.SecondURL, targetURL)
		//Skip the target object if it matches the Exclude options provided
		if matchExcludeOptions(opts.excludeOptions, tgtSuffix) {
			continue
		}

		if source := diffMsg.firstContent; source != nil && (isOlder(source.Time, opts.olderThan) || isNewer(source.Time, opts.newerThan)) {
			continue
		}
		switch diffMsg.Diff {
		case differInNone:
			if (!opts.reconcile && !opts.verifyContents) || opts.activeActive {
				continue
			}
			if opts.verifyContents && !opts.reconcile {
				equal, err := mirrorContentsEqual(ctx, sourceAlias, targetAlias, diffMsg, opts.encKeyDB)
				if err != nil {
					if !send(URLs{Error: probe.NewError(err), ErrorCond: differInUnknown}) {
						return
					}
					continue
				}
				if equal {
					continue
				}
			}
			// Recopy even equal-size/equal-time files after event loss: a
			// metadata comparison cannot prove their bytes are unchanged.
			fallthrough
		case differInSize, differInMetadata, differInAASourceMTime:
			if !opts.isOverwrite && !opts.isFake && !opts.activeActive {
				// Size or time or etag differs but --overwrite not set.
				if !send(URLs{
					Error:     errOverWriteNotAllowed(diffMsg.SecondURL),
					ErrorCond: diffMsg.Diff,
				}) {
					return
				}
				continue
			}

			sourceSuffix := strings.TrimPrefix(diffMsg.FirstURL, sourceURL)
			// Either available only in source or size differs and force is set
			targetPath := urlJoinPath(targetURL, sourceSuffix)
			sourceContent := diffMsg.firstContent
			targetContent := &ClientContent{URL: *newClientURL(targetPath)}
			if !send(URLs{
				SourceAlias:   sourceAlias,
				SourceContent: sourceContent,
				TargetAlias:   targetAlias,
				TargetContent: targetContent,
			}) {
				return
			}
		case differInType:
			if !send(URLs{Error: errInvalidTarget(diffMsg.SecondURL)}) {
				return
			}
		case differInFirst:
			// Only in first, always copy.
			sourceSuffix := strings.TrimPrefix(diffMsg.FirstURL, sourceURL)
			targetPath := urlJoinPath(targetURL, sourceSuffix)
			sourceContent := diffMsg.firstContent
			targetContent := &ClientContent{URL: *newClientURL(targetPath)}
			if !send(URLs{
				SourceAlias:   sourceAlias,
				SourceContent: sourceContent,
				TargetAlias:   targetAlias,
				TargetContent: targetContent,
			}) {
				return
			}
		case differInSecond:
			if !opts.isRemove && !opts.isFake {
				continue
			}
			if !send(URLs{
				TargetAlias:   targetAlias,
				TargetContent: diffMsg.secondContent,
			}) {
				return
			}
		default:
			if !send(URLs{
				Error:     errUnrecognizedDiffType(diffMsg.Diff).Trace(diffMsg.FirstURL, diffMsg.SecondURL),
				ErrorCond: diffMsg.Diff,
			}) {
				return
			}
		}
	}
}

type mirrorOptions struct {
	watchRescanInterval               time.Duration
	watchVerifyInterval               time.Duration
	nextVerify                        time.Time
	reconcile                         bool
	verifyContents                    bool
	isFake, isOverwrite, activeActive bool
	isWatch, isRemove, isMetadata     bool
	excludeOptions                    []string
	encKeyDB                          map[string][]prefixSSEPair
	md5, disableMultipart             bool
	olderThan, newerThan              string
	storageClass                      string
	userMetadata                      map[string]string
}

// Prepares urls that need to be copied or removed based on requested options.
func prepareMirrorURLs(ctx context.Context, sourceURL string, targetURL string, opts mirrorOptions) <-chan URLs {
	URLsCh := make(chan URLs)
	go deltaSourceTarget(ctx, sourceURL, targetURL, opts, URLsCh)
	return URLsCh
}

// Periodic checks read equal-metadata objects, but only rewrite changed bytes.
func mirrorContentsEqual(ctx context.Context, sourceAlias, targetAlias string, diff diffMessage, keys map[string][]prefixSSEPair) (bool, error) {
	hash := func(alias, path string) ([32]byte, error) {
		var result [32]byte
		reader, err := getSourceStreamFromURL(ctx, filepath.ToSlash(filepath.Join(alias, path)), "", keys)
		if err != nil {
			return result, err.ToGoError()
		}
		defer reader.Close()
		stop := context.AfterFunc(ctx, func() { _ = reader.Close() })
		defer stop()
		h := sha256.New()
		if _, err := io.Copy(h, fsContextReader{ctx: ctx, reader: reader}); err != nil {
			return result, err
		}
		copy(result[:], h.Sum(nil))
		return result, nil
	}
	source, err := hash(sourceAlias, diff.firstContent.URL.Path)
	if err != nil {
		return false, err
	}
	target, err := hash(targetAlias, diff.secondContent.URL.Path)
	return source == target, err
}
