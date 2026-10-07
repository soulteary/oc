// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"context"
	"fmt"
	"strings"

	"github.com/urfave/cli/v3"
)

// runCLICommand keeps the executable's argument spelling and stopping points. It only
// classifies tokens using declared flag metadata; urfave owns value parsing,
// flag state, help, errors, and command execution.
func runCLICommand(ctx context.Context, command *cli.Command, args []string) error {
	if len(args) == 0 {
		return command.Run(ctx, args)
	}
	completion := len(args) > 1 && args[len(args)-1] == "--generate-bash-completion"
	if completion {
		args = append([]string(nil), args[:len(args)-1]...)
		if command.Metadata == nil {
			command.Metadata = make(map[string]any)
		}
		command.Metadata[commandCompletionKey] = true
		defer delete(command.Metadata, commandCompletionKey)
	}
	lex := commandArgumentLexer{original: args, errors: make(map[*cli.Command]map[string]string), parents: make(map[*cli.Command]*cli.Command), completion: completion}
	normalized := append([]string{args[0]}, lex.scope(command, args[1:])...)
	for scope, messages := range lex.errors {
		original := scope.OnUsageError
		scope.OnUsageError = func(ctx context.Context, cmd *cli.Command, err error, subcommand bool) error {
			if message, ok := messages[err.Error()]; ok {
				err = fmt.Errorf("%s", message)
			}
			if original != nil {
				return original(ctx, cmd, err, subcommand)
			}
			return err
		}
		defer func() { scope.OnUsageError = original }()
	}
	return command.Run(ctx, normalized)
}

type commandArgumentLexer struct {
	original   []string
	errors     map[*cli.Command]map[string]string
	parents    map[*cli.Command]*cli.Command
	next       int
	completion bool
}

func (lex *commandArgumentLexer) invalid(command *cli.Command, message string) string {
	for {
		name := fmt.Sprintf("__oc_lexical_%d", lex.next)
		lex.next++
		collision := lex.flag(command, name) != nil
		for _, arg := range lex.original {
			collision = collision || strings.Contains(arg, name)
		}
		if collision {
			continue
		}
		if lex.errors[command] == nil {
			lex.errors[command] = make(map[string]string)
		}
		lex.errors[command]["flag provided but not defined: -"+name] = message
		return "--" + name
	}
}

func (lex *commandArgumentLexer) scope(command *cli.Command, args []string) []string {
	if command.SkipFlagParsing {
		return append([]string(nil), args...)
	}
	if command.StopOnNthArg != nil && *command.StopOnNthArg == 0 {
		return append([]string{"--"}, args...)
	}
	var flags, positional []string
	for i := 0; i < len(args); i++ {
		arg := args[i]
		if arg == "--" {
			positional = append(positional, args[i+1:]...)
			break
		}
		if len(arg) < 2 || arg[0] != '-' {
			positional = append(positional, arg)
			if command.StopOnNthArg != nil && len(positional) >= *command.StopOnNthArg {
				positional = append(positional, args[i+1:]...)
				break
			}
			continue
		}
		name := strings.TrimPrefix(arg, "-")
		name = strings.TrimPrefix(name, "-")
		if name == "" || name[0] == '-' || name[0] == '=' {
			lex.completionNext(command, arg)
			flags = append(flags, lex.invalid(command, "bad flag syntax: "+arg))
			break
		}
		name, value, inline := strings.Cut(name, "=")
		flag := lex.flag(command, name)
		if flag == nil {
			lex.completionNext(command, argumentAt(args, i+1))
			flags = append(flags, lex.invalid(command, "flag provided but not defined: -"+name))
			break
		}
		isBool, takesValue := commandFlagShape(flag)
		if inline && value == "" && isBool {
			lex.completionNext(command, argumentAt(args, i+1))
			flags = append(flags, lex.invalid(command, fmt.Sprintf("invalid boolean value %q for -%s: parse error", value, name)))
			break
		}
		// A declared non-letter name also needs the long form to bypass v3's
		// treatment of a short option beginning with a non-letter as positional.
		flags = append(flags, "--"+strings.TrimPrefix(strings.TrimPrefix(arg, "-"), "-"))
		if !inline && takesValue && i+1 < len(args) {
			i++
			flags = append(flags, args[i])
		} else if !inline && takesValue && len(positional) > 0 {
			// The legacy leaf reorder put earlier positionals after flags. A
			// final value-taking flag therefore consumed the first positional.
			flags = append(flags, positional[0])
			positional = positional[1:]
		}
		if lex.completion && len(command.Commands) != 0 {
			if command.Metadata == nil {
				command.Metadata = make(map[string]any)
			}
			tails, _ := command.Metadata[commandCompletionTailsKey].(map[string][]string)
			if tails == nil {
				tails = make(map[string][]string)
				command.Metadata[commandCompletionTailsKey] = tails
			}
			tails[name] = append(tails[name], argumentAt(args, i+1))
		}
	}
	if len(positional) > 0 {
		if child := command.Command(positional[0]); child != nil {
			lex.parents[child] = command
			positional = append([]string{positional[0]}, lex.scope(child, positional[1:])...)
		}
		// Protect the original positional bytes from v3's TrimSpace-based flag
		// classification, including the empty string and quoted leading spaces.
		flags = append(flags, "--")
	}
	return append(flags, positional...)
}

func commandFlagShape(flag cli.Flag) (isBool, takesValue bool) {
	if boolean, ok := flag.(interface{ IsBoolFlag() bool }); ok && boolean.IsBoolFlag() {
		return true, false
	}
	if meta, ok := flag.(cli.DocGenerationFlag); ok {
		return meta.TypeName() == "bool", meta.TakesValue()
	}
	return false, true
}

func (lex *commandArgumentLexer) completionNext(command *cli.Command, next string) {
	if lex.completion {
		if command.Metadata == nil {
			command.Metadata = make(map[string]any)
		}
		command.Metadata[commandCompletionNextKey] = next
	}
}

func (lex *commandArgumentLexer) flag(command *cli.Command, name string) cli.Flag {
	for scope := command; scope != nil; scope = lex.parents[scope] {
		for _, flag := range scope.Flags {
			if scope != command {
				if local, ok := flag.(cli.LocalFlag); !ok || local.IsLocal() {
					continue
				}
			}
			for _, alias := range flag.Names() {
				if alias == name {
					return flag
				}
			}
		}
	}
	return nil
}
