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
	"fmt"
	"testing"

	"github.com/urfave/cli/v3"
)

func TestAutoCompletionCompletness(t *testing.T) {

	var checkCompletion func(cmd *cli.Command, cmdPath string) error

	checkCompletion = func(cmd *cli.Command, cmdPath string) error {
		if cmd.Commands != nil {
			for _, subCmd := range cmd.Commands {
				if cmd.Hidden {
					continue
				}
				err := checkCompletion(subCmd, cmdPath+"/"+subCmd.Name)
				if err != nil {
					return err
				}
			}
			return nil
		}
		_, ok := completeCmds[cmdPath]
		if !ok && !cmd.Hidden {
			return fmt.Errorf("Completion for `%s` not found", cmdPath)
		}
		return nil
	}

	for _, cmd := range appCmds {
		if cmd.Hidden {
			continue
		}
		err := checkCompletion(cmd, "/"+cmd.Name)
		if err != nil {
			t.Fatalf("Missing completion function: %v", err)
		}

	}
}

func TestAutoCompletionVisibilityAndAliases(t *testing.T) {
	command := cmdToCompleteCmd(&cli.Command{
		Name: "admin",
		Flags: []cli.Flag{
			&cli.BoolFlag{Name: "hidden-option", Aliases: []string{"x"}, Hidden: true},
		},
		Commands: []*cli.Command{
			{Name: "visible", Aliases: []string{"short"}},
			{Name: "hidden-command", Hidden: true},
		},
	}, "")
	if _, present := command.Flags["--hidden-option"]; !present {
		t.Fatal("legacy completion includes hidden flags")
	}
	if _, present := command.Flags["-x"]; !present {
		t.Fatal("flag alias missing")
	}
	if _, present := command.Sub["visible"]; !present {
		t.Fatal("visible command missing")
	}
	for _, excluded := range []string{"short", "hidden-command"} {
		if _, present := command.Sub[excluded]; present {
			t.Fatalf("legacy completion excludes %q", excluded)
		}
	}
}
