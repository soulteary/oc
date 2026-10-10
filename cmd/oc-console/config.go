// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package main

import (
	"encoding/json"
	"errors"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/mc/internal/storageclient"
	"github.com/soulteary/otterio/pkg/certs"
)

type options struct {
	authMode, s3URL, tlsCert, tlsKey                                         string
	dataDir                                                                  string
	containerListen                                                          bool
	publicURL                                                                string
	configDir, alias, address, s3CA, adminURL, adminCA, shareURL, archiveDir string
	allowWrites                                                              bool
	allowSharing                                                             bool
	maxUploadSize                                                            int64
	maxArchiveSize                                                           int64
}

type aliasConfig struct {
	URL          string `json:"url"`
	AdminURL     string `json:"adminURL"`
	AdminCAFile  string `json:"adminCAFile"`
	AccessKey    string `json:"accessKey"`
	SecretKey    string `json:"secretKey"`
	SessionToken string `json:"sessionToken"`
	API          string `json:"api"`
	Path         string `json:"path"`
}

var aliasName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_-]{0,63}$`)

func defaultConfigDir(getenv func(string) string) (string, error) {
	for _, key := range []string{"OC_CONFIG_DIR", "MC_CONFIG_DIR"} {
		if dir := getenv(key); dir != "" {
			return dir, nil
		}
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", errors.New("cannot determine the OC configuration directory; use --config-dir")
	}
	name := ".oc"
	if runtime.GOOS == "windows" {
		name = "oc"
	}
	return filepath.Join(homeDir, name), nil
}

func adminSetting(flagValue, key, alias, stored, fallback string, getenv func(string) string) string {
	for _, value := range []string{flagValue, getenv(key + "_" + alias), getenv(key), stored, fallback} {
		if value != "" {
			return value
		}
	}
	return ""
}

// loadClientConfig is deliberately read-only. It never creates, upgrades, or
// rewrites the CLI's config, and never includes its contents in error messages.
func loadClientConfig(opts options, getenv func(string) string) (storageclient.Config, error) {
	var result storageclient.Config
	if !aliasName.MatchString(opts.alias) {
		return result, errors.New("--alias must name one configured S3 alias (letters, numbers, underscore or hyphen, up to 64 characters)")
	}
	file, err := os.Open(filepath.Join(opts.configDir, "config.json"))
	if err != nil {
		return result, errors.New("cannot read OC config.json; configure an alias with oc or select --config-dir")
	}
	defer file.Close()
	const maxConfigSize = 4 << 20
	data, err := io.ReadAll(io.LimitReader(file, maxConfigSize+1))
	if err != nil || len(data) > maxConfigSize {
		return result, errors.New("OC configuration cannot be read or exceeds 4 MiB")
	}
	var cfg struct {
		Version string                 `json:"version"`
		Aliases map[string]aliasConfig `json:"aliases"`
	}
	if json.Unmarshal(data, &cfg) != nil || cfg.Version != "10" {
		return result, errors.New("oc-console requires a valid OC version 10 configuration")
	}
	selected, ok := cfg.Aliases[opts.alias]
	if !ok {
		return result, errors.New("selected alias is absent from OC config.json")
	}
	if getenv("OC_HOST_"+opts.alias) != "" || getenv("MC_HOST_"+opts.alias) != "" {
		return result, errors.New("host environment overrides are not supported by oc-console; configure the selected alias in config.json")
	}
	result = storageclient.Config{
		S3URL:     selected.URL,
		AdminURL:  adminSetting(opts.adminURL, "OC_ADMIN_URL", opts.alias, selected.AdminURL, selected.URL, getenv),
		AccessKey: selected.AccessKey, SecretKey: selected.SecretKey, SessionToken: selected.SessionToken,
		API: selected.API, Path: selected.Path, AppName: "oc-console", AppVersion: version,
	}
	result.ShareURL = adminSetting(opts.shareURL, "OC_SHARE_URL", opts.alias, "", "", getenv)
	result.RootCAs, err = certs.GetRootCAs(filepath.Join(opts.configDir, "certs", "CAs"))
	if err != nil {
		return storageclient.Config{}, errors.New("cannot load OC certificates from certs/CAs")
	}
	if opts.s3CA != "" {
		result.RootCAs, _, err = clienttransport.LoadCAFile(opts.s3CA)
		if err != nil {
			return storageclient.Config{}, errors.New("cannot load --s3-ca PEM certificates")
		}
	}
	// Match the CLI fallback when no independent management CA is selected.
	result.AdminRootCAs = result.RootCAs
	adminCA := adminSetting(opts.adminCA, "OC_ADMIN_CA", opts.alias, selected.AdminCAFile, "", getenv)
	if adminCA != "" {
		result.AdminRootCAs, _, err = clienttransport.LoadCAFile(adminCA)
		if err != nil {
			return storageclient.Config{}, errors.New("cannot load management CA PEM certificates")
		}
	}
	return result, nil
}

func validateListenAddress(address string) error {
	host, port, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("--address must contain a loopback IP and port, for example 127.0.0.1:9090")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsLoopback() || ip.Zone() != "" {
		return errors.New("oc-console currently accepts only literal loopback listen addresses")
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 0 || number > 65535 {
		return errors.New("--address port must be between 0 and 65535")
	}
	return nil
}

// Container mode separates the bind address from the browser origin.
func validateContainerListenAddress(address string) error {
	host, _, err := net.SplitHostPort(address)
	if err != nil {
		return errors.New("container address must be 0.0.0.0:PORT or [::]:PORT")
	}
	ip, err := netip.ParseAddr(host)
	if err != nil || !ip.IsUnspecified() || ip.Zone() != "" {
		return errors.New("container address must use an unspecified IPv4 or IPv6 host (0.0.0.0 or ::)")
	}
	_, port, _ := net.SplitHostPort(address)
	return validateListenAddress(net.JoinHostPort("127.0.0.1", port))
}
