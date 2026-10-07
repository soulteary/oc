package cmd

import (
	"bytes"
	"context"
	"io"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/urfave/cli/v3"
)

func ignoreCLIExit(context.Context, *cli.Command, error) {}

type cliParserBooleanFlag struct{ cli.Flag }

func (*cliParserBooleanFlag) IsBoolFlag() bool { return true }

func runCLIForTest(prototype *cli.Command, arguments ...string) error {
	installCommandHelpPrinter()
	command := cloneCommand(prototype)
	command.ExitErrHandler = ignoreCLIExit
	command.Writer, command.ErrWriter = io.Discard, io.Discard
	err := runCLICommand(context.Background(), command, append([]string{command.Name}, arguments...))
	if exit, ok := err.(cli.ExitCoder); ok && exit.ExitCode() == 0 {
		return nil
	}
	return err
}

func TestCLIPresenceBooleanScope(t *testing.T) {
	for _, test := range []struct {
		arguments            []string
		local, global, value bool
	}{
		{[]string{"leaf"}, false, false, false},
		{[]string{"--quiet=false", "leaf"}, false, true, false},
		{[]string{"leaf", "--quiet=false"}, true, false, false},
		{[]string{"--quiet", "leaf", "--quiet=false"}, true, true, false},
	} {
		leaf := &cli.Command{Name: "leaf", Flags: []cli.Flag{&cli.BoolFlag{Name: "quiet", Aliases: []string{"q"}}},
			Action: commandAction(func(command *cli.Command) error {
				if command.IsSet("quiet") != test.local || commandGlobalIsSet(command, "quiet") != test.global || command.Bool("quiet") != test.value {
					t.Fatalf("args=%v: local=%v global=%v value=%v", test.arguments, command.IsSet("quiet"), commandGlobalIsSet(command, "quiet"), command.Bool("quiet"))
				}
				return nil
			})}
		root := &cli.Command{Name: "oc", Flags: []cli.Flag{&cli.BoolFlag{Name: "quiet", Aliases: []string{"q"}}}, Commands: []*cli.Command{leaf}, HideHelpCommand: true}
		if err := runCLIForTest(root, test.arguments...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCLIListParsingAndRunIsolation(t *testing.T) {
	var stringsSeen [][]string
	var integersSeen [][]int
	prototype := &cli.Command{Name: "trace", Flags: []cli.Flag{
		&cli.StringSliceFlag{Name: "path", Value: []string{"initial"}},
		&cli.IntSliceFlag{Name: "status-code"},
	}, Action: commandAction(func(command *cli.Command) error {
		stringsSeen = append(stringsSeen, append([]string(nil), command.StringSlice("path")...))
		integersSeen = append(integersSeen, append([]int(nil), command.IntSlice("status-code")...))
		return nil
	})}
	for _, arguments := range [][]string{{"--path", "a,b", "--path", "c", "--status-code", "404", "--status-code", "503"}, nil} {
		if err := runCLIForTest(prototype, arguments...); err != nil {
			t.Fatal(err)
		}
	}
	if !reflect.DeepEqual(stringsSeen, [][]string{{"a,b", "c"}, {"initial"}}) || !reflect.DeepEqual(integersSeen, [][]int{{404, 503}, nil}) {
		t.Fatalf("string values=%v integer values=%v", stringsSeen, integersSeen)
	}
	if err := runCLIForTest(prototype, "--status-code", "404,503"); err == nil {
		t.Fatal("comma-separated integer input accepted")
	}
	if err := runCLIForTest(prototype, "--status-code", "0x194"); err == nil {
		t.Fatal("nondecimal integer list accepted")
	}
	if !reflect.DeepEqual(prototype.Flags[0].(*cli.StringSliceFlag).Value, []string{"initial"}) {
		t.Fatal("prototype default mutated")
	}
}

func TestCLIEnvironmentEmptyPrecedence(t *testing.T) {
	t.Setenv("OC_TEST_CONFIG", "")
	t.Setenv("MC_TEST_CONFIG", "legacy")
	for _, test := range []struct {
		arguments []string
		expected  string
	}{{nil, ""}, {[]string{"--config-dir", "cli"}, "cli"}} {
		prototype := &cli.Command{Name: "oc", Flags: []cli.Flag{&cli.StringFlag{Name: "config-dir", Value: "default", Sources: cli.EnvVars("OC_TEST_CONFIG", "MC_TEST_CONFIG")}},
			Action: commandAction(func(command *cli.Command) error {
				if !command.IsSet("config-dir") || command.String("config-dir") != test.expected {
					t.Fatalf("expected explicitly-set %q, got %q set=%v", test.expected, command.String("config-dir"), command.IsSet("config-dir"))
				}
				return nil
			})}
		if err := runCLIForTest(prototype, test.arguments...); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCLIHealthSourcesAndRepetition(t *testing.T) {
	t.Setenv("OC_TEST_HEALTH", "syscpu,sysmem")
	t.Setenv("MC_TEST_HEALTH", "sysnet")
	var actual []string
	prototype := &cli.Command{Name: "health", Flags: []cli.Flag{&cli.GenericFlag{Name: "test", Value: &HealthDataTypeSlice{}, Sources: cli.EnvVars("OC_TEST_HEALTH", "MC_TEST_HEALTH"), Hidden: true}},
		Action: commandAction(func(command *cli.Command) error {
			selection := GetHealthDataTypeSlice(command, "test")
			if selection == nil {
				t.Fatal("health selection absent")
			}
			actual = nil
			for _, entry := range selection.Value() {
				actual = append(actual, string(entry))
			}
			return nil
		})}
	if err := runCLIForTest(prototype, "--test", "sysnet", "--test", "sysosinfo,syscpu"); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, []string{"syscpu", "sysmem", "sysnet", "sysosinfo", "syscpu"}) {
		t.Fatalf("health values %v", actual)
	}
	if err := runCLIForTest(prototype); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(actual, []string{"syscpu", "sysmem"}) {
		t.Fatalf("next run leaked values: %v", actual)
	}
	t.Setenv("OC_TEST_HEALTH", "")
	if err := runCLIForTest(prototype); err == nil || !strings.Contains(err.Error(), "health datatype") {
		t.Fatalf("empty primary source must fail, got %v", err)
	}
}

func TestCLIBeforeHelpAndParseErrorOrdering(t *testing.T) {
	installCommandHelpPrinter()
	for _, test := range []struct {
		arguments []string
		order     string
		fails     bool
	}{
		{[]string{"--help"}, "", false},
		{[]string{"parent", "leaf", "--help"}, "root,parent", false},
		{[]string{"parent", "leaf", "--unknown"}, "root,parent", true},
		{[]string{"--help", "--unknown"}, "", true},
		{[]string{"parent", "--help", "--unknown"}, "root", true},
		{[]string{"parent", "leaf", "--help", "--unknown"}, "root,parent", true},
		{[]string{"parent", "leaf", "--help=false"}, "root,parent,leaf,action", false},
		{[]string{"parent", "leaf"}, "root,parent,leaf,action", false},
	} {
		var calls []string
		before := func(name string) cli.BeforeFunc {
			return commandBefore(func(*cli.Command) error { calls = append(calls, name); return nil })
		}
		leaf := &cli.Command{Name: "leaf", Flags: []cli.Flag{&cli.BoolFlag{Name: "quiet"}}, Before: before("leaf"), Action: commandAction(func(*cli.Command) error { calls = append(calls, "action"); return nil }), OnUsageError: func(_ context.Context, _ *cli.Command, err error, _ bool) error { return err }}
		parent := &cli.Command{Name: "parent", Flags: []cli.Flag{&cli.BoolFlag{Name: "quiet"}}, Before: before("parent"), Commands: []*cli.Command{leaf}, HideHelpCommand: true}
		root := &cli.Command{Name: "oc", Flags: []cli.Flag{&cli.BoolFlag{Name: "quiet"}}, Before: before("root"), Commands: []*cli.Command{parent}, HideHelpCommand: true}
		err := runCLIForTest(root, test.arguments...)
		if (err != nil) != test.fails || strings.Join(calls, ",") != test.order {
			t.Fatalf("args=%v order=%v error=%v", test.arguments, calls, err)
		}
	}
}

func TestCLIVersionBooleanAndEarlyExit(t *testing.T) {
	for _, test := range []struct {
		arguments []string
		prints    bool
		runs      bool
		fails     bool
	}{
		{[]string{"--version"}, true, false, false}, {[]string{"--version=true", "leaf", "--unknown"}, true, false, false},
		{[]string{"--version=false", "leaf"}, false, true, false},
		{[]string{"--version", "--unknown"}, false, false, true},
		{[]string{"--version", "--config-dir"}, false, false, true},
	} {
		var output bytes.Buffer
		called := false
		root := cloneCommand(&cli.Command{Name: "oc", Version: "test", HideVersion: true, HideHelpCommand: true,
			Flags:    []cli.Flag{&cli.StringFlag{Name: "config-dir", Sources: cli.EnvVars("OC_TEST_UNUSED")}, &cli.BoolFlag{Name: "version", Aliases: []string{"v"}}},
			Commands: []*cli.Command{{Name: "leaf", Action: commandAction(func(*cli.Command) error { called = true; return nil })}}, ExitErrHandler: ignoreCLIExit, Writer: &output})
		err := runCLICommand(context.Background(), root, append([]string{"oc"}, test.arguments...))
		if test.prints {
			code, ok := err.(cli.ExitCoder)
			if !ok || code.ExitCode() != 0 {
				t.Fatalf("version error %v", err)
			}
		} else if (err != nil) != test.fails {
			t.Fatalf("args=%v error=%v", test.arguments, err)
		}
		if strings.Contains(output.String(), "oc version test") != test.prints || called != test.runs {
			t.Fatalf("args=%v output=%q action=%v", test.arguments, output.String(), called)
		}
	}
}

func TestCLIHealthErrorMetadata(t *testing.T) {
	flag := &cli.GenericFlag{Name: "test", Value: &HealthDataTypeSlice{}, Usage: "choose health tests", Hidden: true}
	name, usage := commandFlagHelp(flag)
	if !strings.Contains(name, "--test") || usage != "choose health tests" {
		t.Fatalf("name=%q usage=%q", name, usage)
	}
	if actual := legacyCommandUsageError(&cli.Command{Flags: []cli.Flag{&cli.DurationFlag{Name: "deadline", Value: time.Hour}}}, cli.Exit("invalid value \"wrong\" for flag -deadline: time: invalid duration \"wrong\"", 1)).Error(); actual != "invalid value \"wrong\" for flag -deadline: parse error" {
		t.Fatal(actual)
	}
}

func TestCLIAliasForms(t *testing.T) {
	for _, test := range []struct {
		arguments []string
		fails     bool
		value     string
	}{
		{[]string{"--config-dir", "first", "-C", "second"}, true, ""},
		{[]string{"--config-dir", "same", "-C", "same"}, true, ""},
		{[]string{"--config-dir", "first", "--config-dir", "second"}, false, "second"},
		{[]string{"--help", "-h"}, true, ""},
	} {
		var output bytes.Buffer
		command := cloneCommand(&cli.Command{Name: "oc", Flags: []cli.Flag{&cli.StringFlag{Name: "config-dir", Aliases: []string{"C"}}}, Writer: &output, ExitErrHandler: ignoreCLIExit,
			Action: commandAction(func(command *cli.Command) error {
				if command.String("config-dir") != test.value {
					t.Fatal(command.String("config-dir"))
				}
				return nil
			})})
		err := runCLICommand(context.Background(), command, append([]string{"oc"}, test.arguments...))
		if (err != nil) != test.fails {
			t.Fatalf("args=%v error=%v output=%q", test.arguments, err, output.String())
		}
		if test.fails && !strings.HasPrefix(output.String(), "Cannot use two forms of the same flag:") {
			t.Fatalf("args=%v output=%q", test.arguments, output.String())
		}
	}
}

func TestCLIAliasConflictAndParseErrorPriority(t *testing.T) {
	for _, test := range []struct {
		arguments []string
		alias     bool
	}{
		{[]string{"--config-dir", "a", "-C", "b", "--unknown"}, true},
		{[]string{"--config-dir", "a", "-C", "b", "--quiet=invalid"}, true},
		{[]string{"--help", "-h", "--quiet=invalid"}, true},
		{[]string{"--quiet", "-q=invalid"}, false},
		{[]string{"-q=invalid", "--quiet"}, false},
		{[]string{"--help", "-h=invalid"}, false},
		{[]string{"-h=invalid", "--help"}, false},
		{[]string{"--config-dir", "a", "-C"}, false},
		{[]string{"-C", "a", "--config-dir"}, false},
		{[]string{"--unknown", "--config-dir", "a", "-C", "b"}, false},
	} {
		for _, group := range []bool{false, true} {
			var output bytes.Buffer
			prototype := &cli.Command{Name: "oc", Flags: []cli.Flag{
				&cli.StringFlag{Name: "config-dir", Aliases: []string{"C"}},
				&cli.BoolFlag{Name: "quiet", Aliases: []string{"q"}},
			}, Writer: &output, ExitErrHandler: ignoreCLIExit}
			if group {
				prototype.Commands = []*cli.Command{{Name: "child"}}
			} else {
				prototype.OnUsageError = func(_ context.Context, command *cli.Command, err error, _ bool) error {
					if aliasError := commandAliasUsageError(command); aliasError != nil {
						return aliasError
					}
					return err
				}
			}
			err := runCLICommand(context.Background(), cloneCommand(prototype), append([]string{"oc"}, test.arguments...))
			if err == nil || strings.HasPrefix(output.String(), "Cannot use two forms of the same flag:") != test.alias || strings.HasPrefix(err.Error(), "Cannot use two forms of the same flag:") != test.alias {
				t.Fatalf("group=%v args=%v output=%q error=%v", group, test.arguments, output.String(), err)
			}
		}
	}
}

func TestCLIArgumentSpellingAndStopping(t *testing.T) {
	for _, test := range []struct {
		arguments []string
		scope     string
		args      []string
		config    string
		quiet     bool
		limit     int
		error     string
	}{
		{arguments: []string{"unknown", "--version"}, scope: "oc", args: []string{"unknown", "--version"}},
		{arguments: []string{"unknown", "--help"}, scope: "oc", args: []string{"unknown", "--help"}},
		{arguments: []string{"--help", "unknown", "--unknown"}},
		{arguments: []string{" --help"}, scope: "oc", args: []string{" --help"}},
		{arguments: []string{" unknown"}, scope: "oc", args: []string{" unknown"}},
		{arguments: []string{"--help "}, error: "flag provided but not defined: -help "},
		{arguments: []string{"leaf", "--help "}, error: "flag provided but not defined: -help "},
		{arguments: []string{"leaf", "-1"}, error: "flag provided but not defined: -1"},
		{arguments: []string{"leaf", "-1", "--help"}, error: "flag provided but not defined: -1"},
		{arguments: []string{"leaf", "--limit", "-1"}, scope: "oc leaf", args: []string{}, limit: -1},
		{arguments: []string{"leaf", "--", "-1", "--help"}, scope: "oc leaf", args: []string{"-1", "--help"}},
		{arguments: []string{"--", "leaf", "-1"}, error: "flag provided but not defined: -1"},
		{arguments: []string{"leaf", " --help", "--quiet=false"}, scope: "oc leaf", args: []string{" --help"}, quiet: true},
		{arguments: []string{"leaf", "--config-dir=value  "}, scope: "oc leaf", args: []string{}, config: "value  "},
		{arguments: []string{"leaf", "--config-dir=", "literal"}, scope: "oc leaf", args: []string{"literal"}},
		{arguments: []string{"leaf", "earlier", "--config-dir"}, scope: "oc leaf", args: []string{}, config: "earlier"},
		{arguments: []string{"--quiet="}, error: "invalid boolean value \"\" for -quiet: parse error"},
		{arguments: []string{"leaf", "--help="}, error: "invalid boolean value \"\" for -help: parse error"},
		{arguments: []string{"---x"}, error: "bad flag syntax: ---x"},
		{arguments: []string{"-=x"}, error: "bad flag syntax: -=x"},
		{arguments: []string{"leaf", "--=x"}, error: "bad flag syntax: --=x"},
	} {
		called := ""
		action := commandAction(func(command *cli.Command) error {
			called = command.FullName()
			if !reflect.DeepEqual(command.Args().Slice(), test.args) || command.String("config-dir") != test.config || command.IsSet("quiet") != test.quiet || command.Int("limit") != test.limit {
				t.Fatalf("args=%q action=%s positional=%q config=%q quiet=%v limit=%v", test.arguments, called, command.Args().Slice(), command.String("config-dir"), command.IsSet("quiet"), command.Int("limit"))
			}
			return nil
		})
		flags := func() []cli.Flag {
			return []cli.Flag{&cli.StringFlag{Name: "config-dir"}, &cli.BoolFlag{Name: "quiet"}, &cli.IntFlag{Name: "limit"}}
		}
		usageError := func(_ context.Context, command *cli.Command, err error, _ bool) error {
			if aliasError := commandAliasUsageError(command); aliasError != nil {
				return aliasError
			}
			return legacyCommandUsageError(command, err)
		}
		prototype := &cli.Command{Name: "oc", Version: "test", HideVersion: true, HideHelpCommand: true, Flags: append(flags(), &cli.BoolFlag{Name: "version"}), Action: action, OnUsageError: usageError,
			Commands: []*cli.Command{{Name: "leaf", Flags: flags(), Action: action, OnUsageError: usageError}}}
		err := runCLIForTest(prototype, test.arguments...)
		actualError := ""
		if err != nil {
			actualError = err.Error()
		}
		if actualError != test.error || called != test.scope {
			t.Fatalf("args=%q scope=%q error=%q", test.arguments, called, actualError)
		}
	}
}

func TestCLIArgumentsWithoutFlagParsing(t *testing.T) {
	arguments := []string{" --help", "-1", "--quiet=", "--"}
	for _, skipParsing := range []bool{false, true} {
		stopBeforeAnyArgument := 0
		prototype := &cli.Command{Name: "oc", SkipFlagParsing: skipParsing, Flags: []cli.Flag{&cli.BoolFlag{Name: "quiet"}},
			Action: commandAction(func(command *cli.Command) error {
				if !reflect.DeepEqual(command.Args().Slice(), arguments) || command.IsSet("quiet") {
					t.Fatalf("skipParsing=%v args=%q quiet=%v", skipParsing, command.Args().Slice(), command.IsSet("quiet"))
				}
				return nil
			})}
		if !skipParsing {
			prototype.StopOnNthArg = &stopBeforeAnyArgument
		}
		if err := runCLIForTest(prototype, arguments...); err != nil {
			t.Fatalf("skipParsing=%v: %v", skipParsing, err)
		}
	}
}

func TestCLIFlagShapeWithoutDocumentationMetadata(t *testing.T) {
	for _, normalize := range []bool{false, true} {
		command := &cli.Command{Name: "oc", Flags: []cli.Flag{&cliParserBooleanFlag{Flag: &cli.BoolFlag{Name: "custom"}}},
			Action: func(_ context.Context, command *cli.Command) error {
				if !command.Bool("custom") || !reflect.DeepEqual(command.Args().Slice(), []string{"literal"}) {
					t.Fatalf("normalize=%v bool=%v args=%q", normalize, command.Bool("custom"), command.Args().Slice())
				}
				return nil
			}}
		var err error
		if normalize {
			err = runCLICommand(context.Background(), command, []string{"oc", "--custom", "literal"})
		} else {
			err = command.Run(context.Background(), []string{"oc", "--custom", "literal"})
		}
		if err != nil {
			t.Fatal(err)
		}
	}
}

func TestCLILegacyCompletionOrdering(t *testing.T) {
	for _, test := range []struct {
		arguments []string
		output    string
		fails     bool
		before    string
	}{
		{arguments: []string{"--generate-bash-completion"}, output: "leaf\nparent\n"},
		{arguments: []string{"--unknown", "--generate-bash-completion"}, output: "leaf\nparent\n"},
		{arguments: []string{"--unknown", "leaf", "--generate-bash-completion"}, fails: true},
		{arguments: []string{"--quiet=invalid", "leaf", "--generate-bash-completion"}, fails: true},
		{arguments: []string{"--quiet=invalid", "--help", "leaf", "--generate-bash-completion"}, output: "leaf\nparent\n"},
		{arguments: []string{"--config-dir", "--generate-bash-completion"}, output: "leaf\nparent\n"},
		{arguments: []string{"leaf", "--unknown", "--generate-bash-completion"}, before: "root"},
		{arguments: []string{"leaf", "--help", "--generate-bash-completion"}, before: "root"},
		{arguments: []string{"parent", "--generate-bash-completion"}, output: "child\n", before: "root"},
		{arguments: []string{"parent", "child", "--generate-bash-completion"}, before: "root,parent"},
		{arguments: []string{"--help", "-h", "--generate-bash-completion"}, output: "Cannot use two forms of the same flag: h help\n", fails: true},
		{arguments: []string{"--generate-bash-completion", "literal"}, fails: true},
		{arguments: []string{"--generate-shell-completion"}, fails: true},
	} {
		var output bytes.Buffer
		var before []string
		hook := func(name string) cli.BeforeFunc {
			return commandBefore(func(*cli.Command) error { before = append(before, name); return nil })
		}
		flags := func() []cli.Flag {
			return []cli.Flag{&cli.BoolFlag{Name: "quiet"}, &cli.StringFlag{Name: "config-dir"}}
		}
		usage := func(_ context.Context, command *cli.Command, err error, _ bool) error {
			if aliasError := commandAliasUsageError(command); aliasError != nil {
				return aliasError
			}
			if completed, completionError := commandShellCompletion(command, true); completed {
				return completionError
			}
			return err
		}
		leaf := &cli.Command{Name: "leaf", Flags: flags(), Before: hook("leaf"), OnUsageError: usage}
		parent := &cli.Command{Name: "parent", Flags: flags(), Before: hook("parent"), OnUsageError: usage, HideHelpCommand: true,
			Commands: []*cli.Command{{Name: "child", Flags: flags(), OnUsageError: usage}}}
		command := cloneCommand(&cli.Command{Name: "oc", Flags: flags(), Before: hook("root"), OnUsageError: usage, HideHelpCommand: true,
			Commands: []*cli.Command{leaf, parent}, Writer: &output, ExitErrHandler: ignoreCLIExit})
		err := runCLICommand(context.Background(), command, append([]string{"oc"}, test.arguments...))
		failed := err != nil
		if exit, ok := err.(cli.ExitCoder); ok && exit.ExitCode() == 0 {
			failed = false
		}
		if failed != test.fails || output.String() != test.output || strings.Join(before, ",") != test.before {
			t.Fatalf("args=%q stdout=%q before=%v error=%v", test.arguments, output.String(), before, err)
		}
	}
}

func TestCLISynthesizedHelpLeaf(t *testing.T) {
	for _, test := range []struct {
		arguments  []string
		output     string
		fails      bool
		topicError string
	}{
		{arguments: []string{"group", "help", "--generate-bash-completion"}},
		{arguments: []string{"group", "h", "--unknown", "--generate-bash-completion"}},
		{arguments: []string{"group", "help", "--unknown"}, output: "Incorrect Usage: flag provided but not defined: -unknown\n\n", fails: true},
		{arguments: []string{"group", "help", "--help", "-h", "--unknown"}, output: "Cannot use two forms of the same flag: h help\n\n", fails: true},
		{arguments: []string{"group", "help", "--help"}, output: "NAME:\n  oc group help - Shows a list of commands or help for one command\n\nUSAGE:\n  oc group help [command options] [command]\n\nFLAGS:\n  --help, -h  show help\n  \n"},
		{arguments: []string{"group", "help", "group"}, fails: true, topicError: "No help topic for 'group'"},
		{arguments: []string{"group", "h", "group", "--help=false"}, fails: true, topicError: "No help topic for 'group'"},
	} {
		installCommandHelpPrinter()
		var output bytes.Buffer
		var before []string
		hook := func(name string) cli.BeforeFunc {
			return commandBefore(func(*cli.Command) error { before = append(before, name); return nil })
		}
		command := cloneCommand(&cli.Command{Name: "oc", Before: hook("root"), HideHelpCommand: true, Writer: &output, ExitErrHandler: ignoreCLIExit,
			Commands: []*cli.Command{{Name: "group", Before: hook("group"), Commands: []*cli.Command{{Name: "leaf"}}}}})
		err := runCLICommand(context.Background(), command, append([]string{"oc"}, test.arguments...))
		failed := err != nil
		if exit, ok := err.(cli.ExitCoder); ok && exit.ExitCode() == 0 {
			failed = false
		}
		if failed != test.fails || output.String() != test.output || strings.Join(before, ",") != "root,group" {
			t.Fatalf("args=%q stdout=%q before=%v error=%v", test.arguments, output.String(), before, err)
		}
		if test.topicError != "" {
			exit, ok := err.(cli.ExitCoder)
			if !ok || exit.ExitCode() != 3 || err.Error() != test.topicError {
				t.Fatalf("args=%q: expected exact help-topic error and exit 3, got %v", test.arguments, err)
			}
		}
	}
}
