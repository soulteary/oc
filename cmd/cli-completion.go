// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package cmd

import (
	"fmt"

	"github.com/urfave/cli/v3"
)

const (
	commandCompletionKey      = "oc.cli.completion"
	commandCompletionNextKey  = "oc.cli.completion.error.next"
	commandCompletionTailsKey = "oc.cli.completion.flag.tails"
)

// The old hidden terminal marker is independent of the posener completion
// entrypoint. It prints visible command names and skips the current Before.
func commandShellCompletion(command *cli.Command, parseError bool) (bool, error) {
	if command.Root().Metadata[commandCompletionKey] != true {
		return false, nil
	}
	next := command.Args().First()
	if parseError {
		next, _ = command.Metadata[commandCompletionNextKey].(string)
	}
	if command.Command(next) != nil {
		return false, nil
	}
	for _, child := range command.Commands {
		if !child.Hidden {
			for _, name := range child.Names() {
				fmt.Fprintln(command.Root().Writer, name)
			}
		}
	}
	return true, cli.Exit("", 0)
}

// FlagSet left unconsumed arguments available even on an error. Record only
// the next raw token so completion can defer to a known child as it did before.
// Value validation and flag state still belong to urfave.
func commandCompletionFollowingFlag(command *cli.Command, name string) string {
	if command.Root().Metadata[commandCompletionKey] != true {
		return ""
	}
	tails, _ := command.Metadata[commandCompletionTailsKey].(map[string][]string)
	if len(tails[name]) == 0 {
		return ""
	}
	next := tails[name][0]
	tails[name] = tails[name][1:]
	return next
}
