package cmd

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"testing"

	"github.com/soulteary/mc/pkg/probe"
)

func TestFatalJSONMessageHelper(t *testing.T) {
	if os.Getenv("OC_TEST_FATAL_JSON_MESSAGE") != "1" {
		return
	}
	globalJSON = true
	fatal(probe.NewError(errors.New("request failed")), "Unable to remove %s", "alice")
}

func TestFatalJSONFormatsMessage(t *testing.T) {
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	command := exec.Command(executable, "-test.run=^TestFatalJSONMessageHelper$")
	command.Env = append(os.Environ(), "OC_TEST_FATAL_JSON_MESSAGE=1")
	output, err := command.Output()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != 1 {
		t.Fatalf("expected exit 1, got %v: %s", err, output)
	}
	var report struct {
		Status string `json:"status"`
		Error  struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(output, &report); err != nil {
		t.Fatalf("invalid JSON: %v: %s", err, output)
	}
	if report.Status != "error" || report.Error.Message != "Unable to remove alice" {
		t.Fatalf("unexpected report: %s", output)
	}
}
