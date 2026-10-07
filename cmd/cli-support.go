// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.
// Legacy help formatting follows MinIO CLI's MIT-licensed conventions; see CREDITS.

package cmd

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/urfave/cli/v3"
)

// Command definitions are prototypes. A run owns a fresh tree and fresh flags,
// including independent declarations of the same option at different levels.
func cloneCommand(prototype *cli.Command) *cli.Command {
	command := *prototype
	command.Metadata = make(map[string]any)
	command.DisableSliceFlagSeparator = true
	if len(prototype.Commands) != 0 && command.StopOnNthArg == nil {
		stopAfterFirstArgument := 1
		command.StopOnNthArg = &stopAfterFirstArgument
	}
	command.Aliases = slices.Clone(prototype.Aliases)
	command.Commands = make([]*cli.Command, len(prototype.Commands))
	for i, child := range prototype.Commands {
		command.Commands[i] = cloneCommand(child)
	}
	if len(prototype.Commands) != 0 && !prototype.HideHelpCommand {
		command.Commands = append(command.Commands, cloneCommand(&cli.Command{
			Name: "help", Aliases: []string{"h"}, Usage: "Shows a list of commands or help for one command",
			ArgsUsage: "[command]",
			OnUsageError: func(_ context.Context, current *cli.Command, err error, _ bool) error {
				if aliasError := commandAliasUsageError(current); aliasError != nil {
					return aliasError
				}
				if completed, completionError := commandShellCompletion(current, true); completed {
					return completionError
				}
				err = legacyCommandUsageError(current, err)
				fmt.Fprintln(current.Root().Writer, "Incorrect Usage:", err)
				fmt.Fprintln(current.Root().Writer)
				return err
			},
			Action: func(ctx context.Context, help *cli.Command) error {
				parent := help.Lineage()[1]
				if topic := help.Args().First(); topic != "" {
					// Topics name a child, whereas the application help wrapper
					// also accepts the current command for syntax-error help.
					return cli.DefaultShowCommandHelp(ctx, parent, topic)
				}
				return cli.ShowSubcommandHelp(parent)
			},
		}))
	}
	if command.OnUsageError == nil && len(prototype.Commands) != 0 {
		command.OnUsageError = func(_ context.Context, current *cli.Command, err error, _ bool) error {
			if aliasError := commandAliasUsageError(current); aliasError != nil {
				return aliasError
			}
			if completed, completionError := commandShellCompletion(current, true); completed {
				return completionError
			}
			err = legacyCommandUsageError(current, err)
			fmt.Fprintf(current.Root().Writer, "Incorrect Usage. %s\n\n", err)
			return err
		}
	}
	command.Flags = make([]cli.Flag, len(prototype.Flags))
	for i, flag := range prototype.Flags {
		// Copy exported definition fields only, never a parser's private state.
		value := reflect.ValueOf(flag).Elem()
		copy := reflect.New(value.Type())
		for n := 0; n < value.NumField(); n++ {
			if value.Type().Field(n).PkgPath == "" {
				field := value.Field(n)
				if field.Kind() == reflect.Slice {
					cloned := reflect.MakeSlice(field.Type(), field.Len(), field.Len())
					reflect.Copy(cloned, field)
					field = cloned
				}
				copy.Elem().Field(n).Set(field)
			}
		}
		copy.Elem().FieldByName("Local").SetBool(true)
		if generic, ok := copy.Interface().(*cli.GenericFlag); ok {
			if health, ok := generic.Value.(*HealthDataTypeSlice); ok {
				cloned := HealthDataTypeSlice(slices.Clone(*health))
				generic.Value = &cloned
			}
		}
		if integer, ok := copy.Interface().(*cli.IntSliceFlag); ok {
			integer.Config.Base = 10
		}
		copiedFlag := copy.Interface().(cli.Flag)
		if setter, ok := copiedFlag.(cli.StringerSetter); ok {
			setter.SetStringer(func(flag cli.Flag) string {
				name, usage := commandFlagHelp(flag)
				return name + "\t" + usage
			})
		}
		// Keep list flags native so v3 can set its private separator configuration.
		switch copiedFlag.(type) {
		case *cli.StringSliceFlag, *cli.IntSliceFlag:
			command.Flags[i] = copiedFlag
		default:
			command.Flags[i] = &commandFlag{Flag: copiedFlag, owner: &command}
		}
	}
	if !prototype.HideHelp {
		hasHelp := false
		for _, flag := range command.Flags {
			hasHelp = hasHelp || slices.Contains(flag.Names(), "help")
		}
		if !hasHelp {
			help := &cli.BoolFlag{Name: "help", Aliases: []string{"h"}, Usage: "show help", Local: true}
			help.SetStringer(func(flag cli.Flag) string { name, usage := commandFlagHelp(flag); return name + "\t" + usage })
			command.Flags = append(command.Flags, &commandFlag{Flag: help, owner: &command})
		}
	}
	return &command
}

func commandAction(action func(*cli.Command) error) cli.ActionFunc {
	return func(_ context.Context, command *cli.Command) error { return action(command) }
}

func commandBefore(before func(*cli.Command) error) cli.BeforeFunc {
	return func(ctx context.Context, command *cli.Command) (context.Context, error) {
		if command.Metadata == nil {
			command.Metadata = make(map[string]any)
		}
		if command.Metadata["oc.before.done"] == true {
			return ctx, nil
		}
		command.Metadata["oc.before.done"] = true
		return ctx, before(command)
	}
}

// The original framework ran each parent's Before before parsing its child.
// v3 defers Before until reaching the action. Perform those ancestor hooks at
// the child's flag lifecycle boundary, preserving help and parse-error paths.
func beforeCommandAncestors(command *cli.Command) error {
	lineage := command.Lineage()
	for n := len(lineage) - 1; n > 0; n-- {
		parent := lineage[n]
		if parent.Before != nil {
			if _, err := parent.Before(context.Background(), parent); err != nil {
				return err
			}
		}
	}
	return nil
}

type commandFlag struct {
	cli.Flag
	owner     *cli.Command
	spellings map[string]bool
}

func (flag *commandFlag) PreParse() error {
	flag.spellings = make(map[string]bool)
	if err := beforeCommandAncestors(flag.owner); err != nil {
		return err
	}
	if err := flag.Flag.PreParse(); err != nil {
		return err
	}
	// GenericFlag skips empty source strings, but the health selector has
	// always treated a present empty primary variable as an invalid test name.
	if generic, ok := flag.Flag.(*cli.GenericFlag); ok {
		if _, health := generic.Value.(*HealthDataTypeSlice); health {
			for _, key := range generic.GetEnvVars() {
				if value, present := os.LookupEnv(key); present {
					parsed := &HealthDataTypeSlice{}
					if err := parsed.Set(value); err != nil {
						return fmt.Errorf("could not parse %s as health datatype value for flag %s: %s", value, generic.Name, err)
					}
					break
				}
			}
		}
	}
	if duration, ok := flag.Flag.(*cli.DurationFlag); ok {
		for _, key := range duration.GetEnvVars() {
			if value, present := os.LookupEnv(key); present {
				if _, err := time.ParseDuration(value); err != nil {
					return fmt.Errorf("could not parse %s as duration for flag %s: %s", value, duration.Name, err)
				}
				break
			}
		}
	}
	// Environment sources were applied before help and command-line parsing in
	// the original CLI. CLI input still replaces ordinary source defaults in v3.
	return flag.Flag.PostParse()
}

func (flag *commandFlag) Set(name, value string) error {
	next := commandCompletionFollowingFlag(flag.owner, name)
	if err := flag.Flag.Set(name, value); err != nil {
		if flag.owner.Root().Metadata[commandCompletionKey] == true {
			flag.owner.Metadata[commandCompletionNextKey] = next
		}
		return err
	}
	if flag.spellings == nil {
		flag.spellings = make(map[string]bool)
	}
	flag.spellings[name] = true
	return nil
}

func (flag *commandFlag) Get() any {
	return flag.Flag.Get()
}

func commandAliasError(command *cli.Command) error {
	for _, definition := range command.Flags {
		if candidate, ok := definition.(*commandFlag); ok {
			previous := ""
			for _, name := range candidate.Names() {
				if candidate.spellings[name] {
					if previous != "" {
						return fmt.Errorf("Cannot use two forms of the same flag: %s %s", name, previous) //nolint:staticcheck // ST1005: preserve the exact legacy CLI diagnostic.
					}
					previous = name
				}
			}
		}
	}
	return nil
}

func commandAliasUsageError(command *cli.Command) error {
	if err := commandAliasError(command); err != nil {
		fmt.Fprintln(command.Root().Writer, err)
		if command != command.Root() {
			fmt.Fprintln(command.Root().Writer)
		}
		return err
	}
	return nil
}

func (flag *commandFlag) PostParse() error {
	// v1 rejected two spellings of one option within one scope, even when the
	// values agree. Repeating the same spelling or using a different scope is OK.
	if err := commandAliasUsageError(flag.owner); err != nil {
		return err
	}
	if err := flag.Flag.PostParse(); err != nil {
		return err
	}
	if completed, completionError := commandShellCompletion(flag.owner, false); completed {
		return completionError
	}
	// Help follows successful parsing, so it cannot mask an invalid option.
	if flag.owner.Bool("help") {
		if flag.owner == flag.owner.Root() {
			if err := cli.ShowRootCommandHelp(flag.owner); err != nil {
				return err
			}
		} else if err := cli.ShowSubcommandHelp(flag.owner); err != nil {
			return err
		}
		return cli.Exit("", 0)
	}
	if flag.owner == flag.owner.Root() && flag.owner.Bool("version") {
		fmt.Fprintf(flag.owner.Writer, "%s version %s\n", flag.owner.Name, flag.owner.Version)
		return cli.Exit("", 0)
	}
	return nil
}

func (flag *commandFlag) IsBoolFlag() bool { return !flag.TakesValue() }
func (flag *commandFlag) IsLocal() bool    { return true }
func (flag *commandFlag) IsVisible() bool  { return flag.Flag.(cli.VisibleFlag).IsVisible() }
func (flag *commandFlag) TakesValue() bool { return flag.Flag.(cli.DocGenerationFlag).TakesValue() }
func (flag *commandFlag) GetUsage() string { return flag.Flag.(cli.DocGenerationFlag).GetUsage() }
func (flag *commandFlag) GetValue() string { return flag.Flag.(cli.DocGenerationFlag).GetValue() }
func (flag *commandFlag) GetDefaultText() string {
	return flag.Flag.(cli.DocGenerationFlag).GetDefaultText()
}
func (flag *commandFlag) GetEnvVars() []string { return flag.Flag.(cli.DocGenerationFlag).GetEnvVars() }
func (flag *commandFlag) IsDefaultVisible() bool {
	return flag.Flag.(cli.DocGenerationFlag).IsDefaultVisible()
}
func (flag *commandFlag) TypeName() string    { return flag.Flag.(cli.DocGenerationFlag).TypeName() }
func (flag *commandFlag) GetCategory() string { return flag.Flag.(cli.CategorizableFlag).GetCategory() }
func (flag *commandFlag) SetCategory(category string) {
	flag.Flag.(cli.CategorizableFlag).SetCategory(category)
}
func (flag *commandFlag) IsRequired() bool { return flag.Flag.(cli.RequiredFlag).IsRequired() }
func (flag *commandFlag) RunAction(ctx context.Context, command *cli.Command) error {
	return flag.Flag.(cli.ActionableFlag).RunAction(ctx, command)
}
func (flag *commandFlag) String() string {
	name, usage := commandFlagHelp(flag.Flag)
	return name + "\t" + usage
}

func commandGlobalIsSet(command *cli.Command, name string) bool {
	lineage := command.Lineage()
	if len(lineage) > 1 {
		lineage = lineage[1:]
	}
	for _, parent := range lineage {
		if parent.IsSet(name) {
			return true
		}
	}
	return false
}

func commandGlobalValue(command *cli.Command, name string) any {
	lineage := command.Lineage()
	if len(lineage) > 1 {
		lineage = lineage[1:]
	}
	for _, parent := range lineage {
		for _, flag := range parent.Flags {
			if slices.Contains(flag.Names(), name) {
				return flag.Get()
			}
		}
	}
	return nil
}

func commandGlobalString(command *cli.Command, name string) string {
	value, _ := commandGlobalValue(command, name).(string)
	return value
}
func commandGlobalBool(command *cli.Command, name string) bool {
	value, _ := commandGlobalValue(command, name).(bool)
	return value
}
func commandGlobalGeneric(command *cli.Command, name string) any {
	return commandGlobalValue(command, name)
}

func argumentAt(arguments []string, index int) string {
	if index >= 0 && index < len(arguments) {
		return arguments[index]
	}
	return ""
}
func argumentTail(arguments []string) []string {
	if len(arguments) > 1 {
		return arguments[1:]
	}
	return nil
}

func legacyCommandUsageError(command *cli.Command, err error) error {
	message := strings.Replace(err.Error(), "flag needs an argument: --", "flag needs an argument: -", 1)
	for _, flag := range command.Flags {
		if wrapped, ok := flag.(*commandFlag); ok {
			flag = wrapped.Flag
		}
		for _, name := range flag.Names() {
			marker := " for flag -" + name + ": "
			if at := strings.Index(message, marker); at >= 0 && strings.HasPrefix(message, "invalid value ") {
				switch flag.(type) {
				case *cli.IntSliceFlag:
					message = strings.Replace(message, "strconv.ParseInt:", "strconv.Atoi:", 1)
				case *cli.BoolFlag:
					message = "invalid boolean value " + strings.TrimPrefix(message[:at], "invalid value ") + " for -" + name + ": parse error"
				case *cli.IntFlag, *cli.UintFlag, *cli.DurationFlag:
					detail := "parse error"
					if strings.HasSuffix(message, "value out of range") {
						detail = "value out of range"
					}
					message = message[:at+len(marker)] + detail
				}
			}
		}
	}
	return errors.New(message)
}

func commandFlagHelp(flag cli.Flag) (string, string) {
	if wrapped, ok := flag.(*commandFlag); ok {
		flag = wrapped.Flag
	}
	doc := flag.(cli.DocGenerationFlag)
	usage := doc.GetUsage()
	placeholder := ""
	if start := strings.IndexByte(usage, '`'); start >= 0 {
		if end := strings.IndexByte(usage[start+1:], '`'); end >= 0 {
			end += start + 1
			placeholder = usage[start+1 : end]
			usage = usage[:start] + placeholder + usage[end+1:]
		}
	}
	if doc.TakesValue() && placeholder == "" {
		placeholder = "value"
	}
	names := make([]string, 0, len(flag.Names()))
	for _, name := range flag.Names() {
		prefix := "--"
		if len(name) == 1 {
			prefix = "-"
		}
		if placeholder != "" {
			name += " " + placeholder
		}
		names = append(names, prefix+name)
	}
	// Defaults describe definitions, not values changed by a previous parse.
	value := reflect.ValueOf(flag).Elem().FieldByName("Value")
	defaultText := ""
	switch flag.(type) {
	case *cli.BoolFlag:
	case *cli.StringSliceFlag, *cli.IntSliceFlag:
		if value.Len() > 0 {
			parts := make([]string, value.Len())
			for n := range parts {
				if value.Index(n).Kind() == reflect.String {
					parts[n] = strconv.Quote(value.Index(n).String())
				} else {
					parts[n] = fmt.Sprint(value.Index(n).Interface())
				}
			}
			defaultText = strings.Join(parts, ", ")
		}
	case *cli.GenericFlag:
		// The health selector is deliberately hidden and had no default label.
	default:
		if value.Kind() == reflect.String {
			if value.String() != "" {
				defaultText = strconv.Quote(value.String())
			}
		} else {
			defaultText = fmt.Sprint(value.Interface())
		}
	}
	if defaultText != "" {
		usage += " (default: " + defaultText + ")"
	}
	if env := doc.GetEnvVars(); len(env) > 0 {
		prefix, separator, suffix := "$", ", $", ""
		if runtime.GOOS == "windows" {
			prefix, separator, suffix = "%", "%, %", "%"
		}
		usage += " [" + prefix + strings.Join(env, separator) + suffix + "]"
	}
	return strings.Join(names, ", "), strings.TrimSpace(usage)
}

var commandHelpPrinterOnce sync.Once

const commandHelpProgramKey = "oc.cli.help.program.name"

type commandHelpData struct{ *cli.Command }

// Legacy top-level leaves inherited NewApp's executable HelpName. A group
// created a sub-app whose help names used the logical application name instead.
func (data commandHelpData) FullName() string {
	command := data.Command
	if len(command.Lineage()) == 2 && len(command.Commands) == 0 {
		if program, ok := command.Root().Metadata[commandHelpProgramKey].(string); ok && program != "" {
			return program + " " + command.Name
		}
	}
	return command.FullName()
}

// Keep the original explicit help command visible in these group templates.
func (data commandHelpData) VisibleCommands() []*cli.Command {
	var commands []*cli.Command
	for _, command := range data.Commands {
		if !command.Hidden {
			commands = append(commands, command)
		}
	}
	return commands
}

func installCommandHelpPrinter() {
	commandHelpPrinterOnce.Do(func() {
		// v3's automatic help path bypasses usage errors when --help occurs
		// before an invalid option. Explicit per-command flags handle it instead.
		cli.HelpFlag = nil
		cli.ShowSubcommandHelp = func(command *cli.Command) error {
			template := command.CustomHelpTemplate
			if len(command.Commands) != 0 {
				template = cli.SubcommandHelpTemplate
			} else if template == "" {
				template = cli.CommandHelpTemplate
			}
			cli.HelpPrinter(command.Root().Writer, template, command)
			return nil
		}
		cli.ShowCommandHelp = func(ctx context.Context, command *cli.Command, name string) error {
			if command.Bool("help") {
				if command == command.Root() {
					return cli.ShowRootCommandHelp(command)
				}
				return cli.ShowSubcommandHelp(command)
			}
			if command.HasName(name) || name == "" {
				return cli.ShowSubcommandHelp(command)
			}
			return cli.DefaultShowCommandHelp(ctx, command, name)
		}
		cli.CommandHelpTemplate = `NAME:
  {{.FullName}} - {{.Usage}}

USAGE:
  {{.FullName}}{{if .VisibleFlags}} [command options]{{end}} {{if .ArgsUsage}}{{.ArgsUsage}}{{else}}[arguments...]{{end}}{{if .Category}}

CATEGORY:
  {{.Category}}{{end}}{{if .Description}}

DESCRIPTION:
  {{.Description}}{{end}}{{if .VisibleFlags}}

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
`
		cli.SubcommandHelpTemplate = `NAME:
  {{.FullName}} - {{if .Description}}{{.Description}}{{else}}{{.Usage}}{{end}}

USAGE:
  {{.FullName}} COMMAND{{if .VisibleFlags}} [COMMAND FLAGS | -h]{{end}} [ARGUMENTS...]

COMMANDS:
  {{range .VisibleCommands}}{{join .Names ", "}}{{"\t"}}{{.Usage}}
  {{end}}{{if .VisibleFlags}}
FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}{{end}}
`
		cli.HelpPrinterCustom = func(output io.Writer, template string, data any, functions map[string]any) {
			if command, ok := data.(*cli.Command); ok {
				data = commandHelpData{command}
			}
			if functions == nil {
				functions = make(map[string]any)
			}
			prompt, env, disable, enable := "$", "export", "$ set +o history", "$ set -o history"
			if runtime.GOOS == "windows" {
				prompt, env = "C:\\>", "set"
				disable = "For security reasons, disable Windows history activity momentarily.\n     Go to \"Settings/Privacy/Activity history\" and click on check boxes,\n     \"Store my activity on this device\" and \"Send my activity history to\n     Microsoft\" to deselect and disable the history activity."
				enable = "Click and select \"Store my activity on this device\" check box to enable history activity."
			}
			functions["Prompt"] = func() string { return prompt }
			functions["EnvVarSetCommand"] = func() string { return env }
			functions["AssignmentOperator"] = func() string { return "=" }
			functions["DisableHistory"] = func() string { return disable }
			functions["EnableHistory"] = func() string { return enable }
			cli.DefaultPrintHelpCustom(output, template, data, functions)
		}
	})
}
