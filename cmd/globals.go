/*
 * MinIO Client (C) 2015-2020 MinIO, Inc.
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

// Package cmd contains all the global variables and constants. ONLY TO BE ACCESSED VIA GET/SET FUNCTIONS.
package cmd

import (
	"context"
	"crypto/x509"

	"github.com/soulteary/otterio/pkg/console"
	"github.com/urfave/cli/v3"
)

const (
	globalMCConfigVersion = "10"

	globalMCConfigFile = "config.json"
	globalMCCertsDir   = "certs"
	globalMCCAsDir     = "CAs"

	// session config and shared urls related constants
	globalSessionDir           = "session"
	globalSharedURLsDataDir    = "share"
	globalSessionConfigVersion = "8"

	// Profile directory for dumping profiler outputs.
	globalProfileDir = "profile"

	// Global error exit status.
	globalErrorExitStatus = 1

	// Global CTRL-C (SIGINT, #2) exit status.
	globalCancelExitStatus = 130

	// Global SIGKILL (#9) exit status.
	globalKillExitStatus = 137

	// Global SIGTERM (#15) exit status
	globalTerminatExitStatus = 143
)

var (
	globalAdminURL string
	globalAdminCA  string
	globalQuiet    = false // Quiet flag set via command line
	globalJSON     = false // Json flag set via command line
	globalDebug    = false // Debug flag set via command line
	globalNoColor  = false // No Color flag set via command line
	globalInsecure = false // Insecure flag set via command line

	globalContext, globalCancel = context.WithCancel(context.Background())
)

var (
	// Terminal width
	globalTermWidth int

	// CA root certificates, a nil value means system certs pool will be used
	globalRootCAs *x509.CertPool
)

// Set global states. NOTE: It is deliberately kept monolithic to ensure we dont miss out any flags.
func setGlobals(quiet, debug, json, noColor, insecure bool) {
	globalQuiet = globalQuiet || quiet
	globalDebug = globalDebug || debug
	globalJSON = globalJSON || json
	globalNoColor = globalNoColor || noColor
	globalInsecure = globalInsecure || insecure

	// Disable colorified messages if requested.
	if globalNoColor || globalQuiet {
		console.SetColorOff()
	}
}

// Set global states. NOTE: It is deliberately kept monolithic to ensure we dont miss out any flags.
func setGlobalsFromContext(ctx *cli.Command) error {
	quiet := ctx.IsSet("quiet") || commandGlobalIsSet(ctx, "quiet")
	debug := ctx.IsSet("debug") || commandGlobalIsSet(ctx, "debug")
	json := ctx.IsSet("json") || commandGlobalIsSet(ctx, "json")
	noColor := ctx.IsSet("no-color") || commandGlobalIsSet(ctx, "no-color")
	insecure := ctx.IsSet("insecure") || commandGlobalIsSet(ctx, "insecure")
	globalAdminURL = commandStringOverride(ctx, "admin-url")
	globalAdminCA = commandStringOverride(ctx, "admin-ca")
	setGlobals(quiet, debug, json, noColor, insecure)
	return nil
}

func commandStringOverride(ctx *cli.Command, name string) string {
	// GlobalString stops at the nearest flag declaration, including an unset
	// default on a nested command. Walk explicit settings instead.
	for _, current := range ctx.Lineage() {
		if current.IsSet(name) {
			return current.String(name)
		}
	}
	return ""
}
