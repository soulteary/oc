/*
 * MinIO Client (C) 2014-2019 MinIO, Inc.
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
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"syscall"

	"github.com/cheggaaa/pb"
	"github.com/pkg/profile"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/trie"
	"github.com/soulteary/otterio/pkg/words"
	"github.com/urfave/cli/v3"

	completeinstall "github.com/posener/complete/cmd/install"
)

var (
	// global flags for mc.
	mcFlags = []cli.Flag{
		&cli.BoolFlag{
			Name:  "autocompletion",
			Usage: "install auto-completion for your shell",
		},
	}
)

// Help template for mc
var mcHelpTemplate = `NAME:
  {{.Name}} - {{.Usage}}

USAGE:
  {{.Name}} {{if .VisibleFlags}}[FLAGS] {{end}}COMMAND{{if .VisibleFlags}} [COMMAND FLAGS | -h]{{end}} [ARGUMENTS...]

COMMANDS:
  {{range .VisibleCommands}}{{join .Names ", "}}{{ "\t" }}{{.Usage}}
  {{end}}{{if .VisibleFlags}}
GLOBAL FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
TIP:
  Use '{{.Name}} --autocompletion' to enable shell autocompletion

VERSION:
  ` + ReleaseTag +
	`{{ "\n"}}{{range $key, $value := ExtraInfo}}
{{$key}}:
  {{$value}}
{{end}}`

// Main starts mc application
func Main(args []string) {
	if code := runMain(args); code != 0 {
		os.Exit(code)
	}
}

func runMain(args []string) (exitCode int) {

	if len(args) > 1 {
		switch args[1] {
		case "mc", "oc", filepath.Base(args[0]):
			mainComplete()
			return
		}
	}

	// Enable profiling supported modes are [cpu, mem, block].
	// ``MC_PROFILER`` supported options are [cpu, mem, block].
	switch clientEnv("MC_PROFILER") {
	case "cpu":
		defer profile.Start(profile.CPUProfile, profile.ProfilePath(mustGetProfileDir())).Stop()
	case "mem":
		defer profile.Start(profile.MemProfile, profile.ProfilePath(mustGetProfileDir())).Stop()
	case "block":
		defer profile.Start(profile.BlockProfile, profile.ProfilePath(mustGetProfileDir())).Stop()
	}

	probe.Init() // Set project's root source path.
	probe.SetAppInfo("Release-Tag", ReleaseTag)
	probe.SetAppInfo("Commit", ShortCommitID)

	// Fetch terminal size, if not available, automatically
	// set globalQuiet to true.
	if w, e := pb.GetTerminalWidth(); e != nil {
		globalQuiet = true
	} else {
		globalTermWidth = w
	}

	// Set the mc app name.
	appName := "oc"
	if runtime.GOOS == "windows" && strings.HasSuffix(strings.ToLower(appName), ".exe") {
		// Trim ".exe" from Windows executable.
		appName = appName[:strings.LastIndex(appName, ".")]
	}

	// Monitor OS exit signals and cancel the global context in such case
	done := make(chan struct{})
	defer close(done)
	go trapSignals(done, os.Interrupt, syscall.SIGTERM)

	// Run the app - exit on error.
	runArgs := append([]string(nil), args...)
	runArgs[0] = appName
	err := runCLICommand(globalContext, registerApp(appName), runArgs)
	if code := signalExitCode.Load(); code != 0 {
		return int(code)
	}
	if err != nil {
		if code, ok := err.(cli.ExitCoder); ok {
			return code.ExitCode()
		}
		return 1
	}
	return 0
}

// Function invoked when invalid flag is passed
func onUsageError(_ context.Context, ctx *cli.Command, err error, subcommand bool) error {
	if aliasError := commandAliasUsageError(ctx); aliasError != nil {
		return aliasError
	}
	if completed, completionError := commandShellCompletion(ctx, true); completed {
		return completionError
	}
	err = legacyCommandUsageError(ctx, err)
	type subCommandHelp struct {
		flagName string
		usage    string
	}

	// Calculate the maximum width of the flag name field
	// for a good looking printing
	flags := ctx.Flags
	// An application-level usage error previously had an empty Command value.
	if ctx == ctx.Root() {
		flags = nil
	}
	var help = make([]subCommandHelp, len(flags))
	maxWidth := 0
	for i, f := range flags {
		flagName, usage := commandFlagHelp(f)
		if len(flagName) > maxWidth {
			maxWidth = len(flagName)
		}

		help[i] = subCommandHelp{flagName: flagName, usage: usage}
	}
	maxWidth += 2

	var errMsg strings.Builder

	// Do the good-looking printing now
	fmt.Fprintln(&errMsg, "Invalid command usage,", err.Error())
	fmt.Fprintln(&errMsg, "")
	fmt.Fprintln(&errMsg, "SUPPORTED FLAGS:")
	for _, h := range help {
		spaces := string(bytes.Repeat([]byte{' '}, maxWidth-len(h.flagName)))
		fmt.Fprintf(&errMsg, "   %s%s%s\n", h.flagName, spaces, h.usage)
	}
	console.Fatal(errMsg.String())
	return err
}

// Function invoked when invalid command is passed.
func commandNotFound(ctx *cli.Command, cmds []*cli.Command) {
	command := ctx.Args().First()
	if command == "" {
		cli.ShowCommandHelp(context.Background(), ctx, command)
		return
	}
	msg := fmt.Sprintf("`%s` is not a recognized command. Get help using `--help` flag.", command)
	var commandsTree = trie.NewTrie()
	for _, cmd := range cmds {
		commandsTree.Insert(cmd.Name)
	}
	closestCommands := findClosestCommands(commandsTree, command)
	if len(closestCommands) > 0 {
		msg += "\n\nDid you mean one of these?\n"
		if len(closestCommands) == 1 {
			cmd := closestCommands[0]
			msg += fmt.Sprintf("        `%s`", cmd)
		} else {
			for _, cmd := range closestCommands {
				msg += fmt.Sprintf("        `%s`\n", cmd)
			}
		}
	}
	fatalIf(errDummy().Trace(), msg)
}

// Check for sane config environment early on and gracefully report.
func checkConfig() {
	// Refresh the config once.
	loadMcConfig = loadMcConfigFactory()
	// Ensures config file is sane.
	config, err := loadMcConfig()
	// Verify if the path is accesible before validating the config
	fatalIf(err.Trace(mustGetMcConfigPath()), "Unable to access configuration file.")

	// Validate and print error messges
	ok, errMsgs := validateConfigFile(config)
	if !ok {
		var errorMsg bytes.Buffer
		for index, errMsg := range errMsgs {
			// Print atmost 10 errors
			if index > 10 {
				break
			}
			errorMsg.WriteString(errMsg + "\n")
		}
		console.Fatal(errorMsg.String())
	}
}

func migrate() {
	// Fix broken config files if any.
	fixConfig()

	// Migrate config files if any.
	migrateConfig()

	// Migrate session files if any.
	migrateSession()

	// Migrate shared urls if any.
	migrateShare()
}

// Get os/arch/platform specific information.
// Returns a map of current os/arch/platform/memstats.
func getSystemData() map[string]string {
	host, e := os.Hostname()
	fatalIf(probe.NewError(e), "Unable to determine the hostname.")

	memstats := &runtime.MemStats{}
	runtime.ReadMemStats(memstats)
	mem := fmt.Sprintf("Used: %s | Allocated: %s | UsedHeap: %s | AllocatedHeap: %s",
		pb.Format(int64(memstats.Alloc)).To(pb.U_BYTES),
		pb.Format(int64(memstats.TotalAlloc)).To(pb.U_BYTES),
		pb.Format(int64(memstats.HeapAlloc)).To(pb.U_BYTES),
		pb.Format(int64(memstats.HeapSys)).To(pb.U_BYTES))
	platform := fmt.Sprintf("Host: %s | OS: %s | Arch: %s", host, runtime.GOOS, runtime.GOARCH)
	goruntime := fmt.Sprintf("Version: %s | CPUs: %s", runtime.Version(), strconv.Itoa(runtime.NumCPU()))
	return map[string]string{
		"PLATFORM": platform,
		"RUNTIME":  goruntime,
		"MEM":      mem,
	}
}

// initMC - initialize 'mc'.
func initMC() {
	// Check if mc config exists.
	if !isMcConfigExists() {
		err := saveMcConfig(newMcConfig())
		fatalIf(err.Trace(), "Unable to save new mc config.")

		if !globalQuiet && !globalJSON {
			console.Infoln("Configuration written to `" + mustGetMcConfigPath() + "`. Please update your access credentials.")
		}
	}

	// Check if mc session directory exists.
	if !isSessionDirExists() {
		fatalIf(createSessionDir().Trace(), "Unable to create session config directory.")
	}

	// Check if mc share directory exists.
	if !isShareDirExists() {
		initShareConfig()
	}

	// Check if certs dir exists
	if !isCertsDirExists() {
		fatalIf(createCertsDir().Trace(), "Unable to create `CAs` directory.")
	}

	// Check if CAs dir exists
	if !isCAsDirExists() {
		fatalIf(createCAsDir().Trace(), "Unable to create `CAs` directory.")
	}

	// Load all authority certificates present in CAs dir
	loadRootCAs()

}

func installAutoCompletion() {
	if runtime.GOOS == "windows" {
		console.Infoln("autocompletion feature is not available for this operating system")
		return
	}

	shellName := os.Getenv("SHELL")
	if shellName == "" {
		ppid := os.Getppid()
		cmd := exec.Command("ps", "-p", strconv.Itoa(ppid), "-o", "comm=")
		ppName, err := cmd.Output()
		if err != nil {
			fatalIf(probe.NewError(err), "Failed to enable autocompletion. Cannot determine shell type and"+
				"no SHELL environment variable found")
		}
		shellName = strings.TrimSpace(string(ppName))
		console.Infoln("No 'SHELL' env var. Your shell is auto determined as '" + shellName + "'.")
	} else {
		console.Infoln("Your shell is set to '" + shellName + "', by env var 'SHELL'.")
	}
	shellName = strings.ToLower(filepath.Base(shellName))

	supportedShells := map[string]bool{
		"bash": true,
		"zsh":  true,
		"fish": true,
	}

	if !supportedShells[shellName] {
		fatalIf(probe.NewError(errors.New("")),
			"'"+shellName+"' is not a supported shell. "+
				"Supported shells are: bash, zsh, fish")
	}

	err := completeinstall.Install(filepath.Base(os.Args[0]))
	var printMsg string
	if err != nil && strings.Contains(err.Error(), "* already installed") {
		errStr := err.Error()[strings.Index(err.Error(), "\n")+1:]
		re := regexp.MustCompile(`[::space::]*\*.*` + shellName + `.*`)
		relatedMsg := re.FindStringSubmatch(errStr)
		if len(relatedMsg) > 0 {
			printMsg = "\n" + relatedMsg[0]
		} else {
			printMsg = ""
		}
	}
	if printMsg != "" {
		if completeinstall.IsInstalled(filepath.Base(os.Args[0])) || completeinstall.IsInstalled("mc") {
			console.Infoln("autocompletion is enabled.", printMsg)
		} else {
			fatalIf(probe.NewError(err), "Unable to install auto-completion.")
		}
	} else {
		console.Infoln("enabled autocompletion in your '" + shellName + "' rc file. Please restart your shell.")
	}
}

func registerBefore(ctx *cli.Command) error {
	if ctx.IsSet("config-dir") {
		// Set the config directory.
		setMcConfigDir(ctx.String("config-dir"))
	} else if commandGlobalIsSet(ctx, "config-dir") {
		// Set the config directory.
		setMcConfigDir(commandGlobalString(ctx, "config-dir"))
	}

	// Set global flags.
	setGlobalsFromContext(ctx)

	// Migrate any old version of config / state files to newer format.
	migrate()

	// Initialize default config files.
	initMC()

	// Check if config can be read.
	checkConfig()

	return nil
}

// findClosestCommands to match a given string with commands trie tree.
func findClosestCommands(commandsTree *trie.Trie, command string) []string {
	closestCommands := commandsTree.PrefixMatch(command)
	sort.Strings(closestCommands)
	// Suggest other close commands - allow missed, wrongly added and even transposed characters
	for _, value := range commandsTree.Walk(commandsTree.Root()) {
		if sort.SearchStrings(closestCommands, value) < len(closestCommands) {
			continue
		}
		// 2 is arbitrary and represents the max allowed number of typed errors
		if words.DamerauLevenshteinDistance(command, value) < 2 {
			closestCommands = append(closestCommands, value)
		}
	}
	return closestCommands
}

var appCmds = []*cli.Command{
	aliasCmd,
	lsCmd,
	mbCmd,
	rbCmd,
	cpCmd,
	mirrorCmd,
	catCmd,
	headCmd,
	pipeCmd,
	shareCmd,
	findCmd,
	sqlCmd,
	statCmd,
	mvCmd,
	treeCmd,
	duCmd,
	retentionCmd,
	legalHoldCmd,
	diffCmd,
	rmCmd,
	versionCmd,
	ilmCmd,
	encryptCmd,
	eventCmd,
	watchCmd,
	undoCmd,
	policyCmd,
	tagCmd,
	replicateCmd,
	adminCmd,
	configCmd,
	doctorCmd,
	updateCmd,
}

func registerApp(name string) *cli.Command {
	app := &cli.Command{}
	app.Name = name
	app.Action = commandAction(func(ctx *cli.Command) error {
		if ctx.Bool("autocompletion") || commandGlobalBool(ctx, "autocompletion") {
			// Install shell completions
			installAutoCompletion()
			return nil
		}

		if ctx.Args().First() != "" {
			commandNotFound(ctx, app.Commands)
		} else {
			cli.ShowAppHelp(ctx)
		}

		return exitStatus(globalErrorExitStatus)
	})

	app.Before = commandBefore(registerBefore)
	app.ExtraInfo = func() map[string]string {
		if globalDebug {
			return getSystemData()
		}
		return make(map[string]string)
	}

	app.HideHelpCommand = true
	app.Usage = "OC client for cloud storage and filesystems."
	app.Commands = appCmds
	app.Authors = []any{"OtterIO contributors"}
	app.Version = ReleaseTag
	app.Flags = append(append([]cli.Flag{}, mcFlags...), globalFlags...)
	app.Flags = append(app.Flags, &cli.BoolFlag{Name: "help", Aliases: []string{"h"}, Usage: "show help"},
		&cli.BoolFlag{Name: "version", Aliases: []string{"v"}, Usage: "print the version"})
	// Our sourced flag lifecycle preserves Bool semantics for --version=false.
	app.HideVersion = true
	app.CustomRootCommandHelpTemplate = mcHelpTemplate
	app.OnUsageError = onUsageError
	// Keep exit handling at Main so command defers and signal cleanup can finish.
	app.ExitErrHandler = func(_ context.Context, _ *cli.Command, err error) {
		if _, coded := err.(cli.ExitCoder); coded && err.Error() != "" {
			fmt.Fprintln(os.Stderr, err)
		}
	}
	app.DisableSliceFlagSeparator = true
	installCommandHelpPrinter()

	return cloneCommand(app)
}

// mustGetProfilePath must get location that the profile will be written to.
func mustGetProfileDir() string {
	return filepath.Join(mustGetMcConfigDir(), globalProfileDir)
}
