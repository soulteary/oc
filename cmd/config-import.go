package cmd

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/urfave/cli/v3"
)

var configImportCmd = &cli.Command{
	OnUsageError: onUsageError,
	Name:         "import", Usage: "import a version 10 mc/oc config file with a backup; source stays unchanged",
	Flags: globalFlags, Before: commandBefore(setGlobalsFromContext),
	Action: commandAction(func(ctx *cli.Command) error {
		if ctx.Args().Len() != 1 {
			return fmt.Errorf("usage: oc config import PATH_TO_CONFIG_JSON")
		}
		count, err := importClientConfig(ctx.Args().Get(0), mustGetMcConfigPath())
		fatalIf(probe.NewError(err), "Unable to import configuration.")
		printMsg(clientConfigImportMessage{Status: "success", Aliases: count})
		return nil
	}),
}

type clientConfigImportMessage struct {
	Status  string `json:"status"`
	Aliases int    `json:"aliases"`
}

func (m clientConfigImportMessage) JSON() string { data, _ := json.Marshal(m); return string(data) }
func (m clientConfigImportMessage) String() string {
	return fmt.Sprintf("Imported %d aliases; previous configuration backed up.", m.Aliases)
}

func importClientConfig(source, destination string) (int, error) {
	source, err := filepath.Abs(source)
	if err != nil {
		return 0, err
	}
	destination, err = filepath.Abs(destination)
	if err != nil {
		return 0, err
	}
	if source == destination {
		return 0, fmt.Errorf("source and destination must differ")
	}
	sourceInfo, err := os.Stat(source)
	if err != nil {
		return 0, err
	}
	if targetInfo, statErr := os.Stat(destination); statErr == nil && os.SameFile(sourceInfo, targetInfo) {
		return 0, fmt.Errorf("source and destination must differ")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		return 0, err
	}
	var cfg configV10
	if err = json.Unmarshal(data, &cfg); err != nil {
		return 0, fmt.Errorf("invalid configuration JSON")
	}
	if cfg.Aliases == nil {
		return 0, fmt.Errorf("configuration must contain aliases")
	}
	if ok, _ := validateConfigFile(&cfg); !ok {
		return 0, fmt.Errorf("configuration validation failed; version 10 and valid endpoints are required")
	}
	for alias, host := range cfg.Aliases {
		if err := validateImportEndpoint(host.URL); err != nil {
			return 0, err
		}
		if !isValidAlias(alias) {
			return 0, fmt.Errorf("configuration contains an invalid alias name")
		}
		if host.Path != "" && host.Path != "auto" && host.Path != "on" && host.Path != "off" && host.Path != "dns" && host.Path != "path" {
			return 0, fmt.Errorf("configuration contains an invalid bucket lookup setting")
		}
		// Resolve relative CA references at their original configuration location.
		if host.AdminCAFile != "" && !filepath.IsAbs(host.AdminCAFile) {
			host.AdminCAFile = filepath.Join(filepath.Dir(source), host.AdminCAFile)
			cfg.Aliases[alias] = host
		}
	}
	encoded, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return 0, err
	}
	encoded = append(encoded, '\n')
	if err = os.MkdirAll(filepath.Dir(destination), 0700); err != nil {
		return 0, err
	}
	old, err := os.ReadFile(destination)
	if err != nil && !os.IsNotExist(err) {
		return 0, err
	}
	if err == nil {
		backup, createErr := os.OpenFile(destination+".backup-"+time.Now().UTC().Format("20060102T150405.000000000"), os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
		if createErr != nil {
			return 0, createErr
		}
		_, writeErr := backup.Write(old)
		if writeErr == nil {
			writeErr = backup.Sync()
		}
		closeErr := backup.Close()
		if writeErr != nil {
			return 0, writeErr
		}
		if closeErr != nil {
			return 0, closeErr
		}
	}
	temp, err := os.CreateTemp(filepath.Dir(destination), ".oc-import-*")
	if err != nil {
		return 0, err
	}
	defer os.Remove(temp.Name())
	if _, err = temp.Write(encoded); err == nil {
		err = temp.Sync()
	}
	closeErr := temp.Close()
	if err != nil {
		return 0, err
	}
	if closeErr != nil {
		return 0, closeErr
	}
	if err = os.Rename(temp.Name(), destination); err != nil {
		return 0, err
	}
	cfgMutex.Lock()
	cacheCfgV10 = nil
	cfgMutex.Unlock()
	return len(cfg.Aliases), nil
}

// Validate a configured S3 endpoint, rather than an object URL or environment alias.
func validateImportEndpoint(endpoint string) error {
	u, err := url.Parse(endpoint)
	if err != nil || u == nil || (u.Scheme != "http" && u.Scheme != "https") || u.Hostname() == "" || strings.ContainsAny(u.Host, " \t\r\n") {
		return fmt.Errorf("configuration contains an invalid S3 endpoint")
	}
	if u.User != nil || u.RawQuery != "" || u.ForceQuery || u.Fragment != "" || strings.Contains(endpoint, "#") || u.Opaque != "" || (u.Path != "" && u.Path != "/") {
		return fmt.Errorf("S3 endpoint must not contain credentials, query parameters, fragments or path prefixes")
	}
	return validateEndpointHost(u)
}
