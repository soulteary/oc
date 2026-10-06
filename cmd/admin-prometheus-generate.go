/*
 * MinIO Client (C) 2019 MinIO, Inc.
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
	"time"

	"github.com/fatih/color"
	"github.com/minio/cli"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/console"
	"github.com/soulteary/otterio/pkg/madmin"

	jwtgo "github.com/dgrijalva/jwt-go"
	json "github.com/soulteary/mc/pkg/colorjson"
	yaml "gopkg.in/yaml.v2"
)

const (
	defaultJobName     = "otterio-job"
	legacyMetricsPath  = "/otterio/prometheus/metrics"
	defaultMetricsPath = "/otterio/v2/metrics/cluster"
)

var adminPrometheusGenerateCmd = cli.Command{
	Name:         "generate",
	Usage:        "generates prometheus config",
	Action:       mainAdminPrometheusGenerate,
	OnUsageError: onUsageError,
	Before:       setGlobalsFromContext,
	Flags: append([]cli.Flag{
		cli.StringFlag{Name: "metrics-type", Value: "cluster", Usage: "metrics endpoint: cluster, node or legacy"},
		cli.BoolFlag{Name: "public", Usage: "omit bearer token for an explicitly public metrics deployment"},
		cli.StringFlag{Name: "metrics-ca", Usage: "metrics CA file path on the Prometheus host (HTTPS only)"},
	}, globalFlags...),
	HideHelpCommand: true,
	CustomHelpTemplate: `NAME:
  {{.HelpName}} - {{.Usage}}

USAGE:
  {{.HelpName}} TARGET

FLAGS:
  {{range .VisibleFlags}}{{.}}
  {{end}}
EXAMPLES:
  1. Generate a default prometheus config.
     {{.Prompt}} {{.HelpName}} myminio

`,
}

// PrometheusConfig - container to hold the top level scrape config.
type PrometheusConfig struct {
	ScrapeConfigs []ScrapeConfig `yaml:"scrape_configs,omitempty"`
}

// String colorized prometheus config yaml.
func (c PrometheusConfig) String() string {
	b, err := yaml.Marshal(c)
	if err != nil {
		return fmt.Sprintf("error creating config string: %s", err)
	}
	return console.Colorize("yaml", string(b))
}

// JSON jsonified prometheus config.
func (c PrometheusConfig) JSON() string {
	if len(c.ScrapeConfigs) == 0 {
		return "{}"
	}
	jsonMessageBytes, e := json.MarshalIndent(c.ScrapeConfigs[0], "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(jsonMessageBytes)
}

// StatConfig - container to hold the targets config.
type StatConfig struct {
	Targets []string `yaml:",flow" json:"targets"`
}

// String colorized stat config yaml.
func (t StatConfig) String() string {
	b, err := yaml.Marshal(t)
	if err != nil {
		return fmt.Sprintf("error creating config string: %s", err)
	}
	return console.Colorize("yaml", string(b))
}

// JSON jsonified stat config.
func (t StatConfig) JSON() string {
	jsonMessageBytes, e := json.MarshalIndent(t.Targets, "", " ")
	fatalIf(probe.NewError(e), "Unable to marshal into JSON.")
	return string(jsonMessageBytes)
}

// ScrapeConfig configures a scraping unit for Prometheus.
type ScrapeConfig struct {
	JobName       string            `yaml:"job_name" json:"jobName"`
	BearerToken   string            `yaml:"bearer_token,omitempty" json:"bearerToken,omitempty"`
	MetricsPath   string            `yaml:"metrics_path,omitempty" json:"metricsPath"`
	Scheme        string            `yaml:"scheme,omitempty" json:"scheme"`
	StaticConfigs []StatConfig      `yaml:"static_configs,omitempty" json:"staticConfigs"`
	TLSConfig     *MetricsTLSConfig `yaml:"tls_config,omitempty" json:"tlsConfig,omitempty"`
}

// MetricsTLSConfig references the S3 listener CA on the Prometheus host.
type MetricsTLSConfig struct {
	CAFile string `yaml:"ca_file" json:"caFile"`
}

func setMetricsCA(config *PrometheusConfig, caFile string) error {
	if caFile == "" {
		return nil
	}
	if len(config.ScrapeConfigs) == 0 || config.ScrapeConfigs[0].Scheme != "https" {
		return fmt.Errorf("metrics CA requires an HTTPS metrics target")
	}
	config.ScrapeConfigs[0].TLSConfig = &MetricsTLSConfig{CAFile: caFile}
	return nil
}

const (
	defaultPrometheusJWTExpiry = 100 * 365 * 24 * time.Hour
)

// checkAdminPrometheusSyntax - validate all the passed arguments
func checkAdminPrometheusSyntax(ctx *cli.Context) {
	if len(ctx.Args()) != 1 {
		cli.ShowCommandHelpAndExit(ctx, "generate", 1) // last argument is exit code
	}
}

// Build a fresh configuration on every call; endpoint selection is explicit,
// rather than comparing a fork's version string with a MinIO release date.
func buildPrometheusConfig(cfg *aliasConfigV10, info madmin.InfoMessage, metricsType string, public bool) (PrometheusConfig, error) {
	if len(info.Servers) == 0 {
		return PrometheusConfig{}, fmt.Errorf("server info contains no servers")
	}
	u, err := validateAdminEndpoint(cfg.URL)
	if err != nil {
		return PrometheusConfig{}, fmt.Errorf("metrics target: %w", err)
	}
	path := defaultMetricsPath
	switch metricsType {
	case "", "cluster":
	case "node":
		path = "/otterio/v2/metrics/node"
	case "legacy":
		path = legacyMetricsPath
	default:
		return PrometheusConfig{}, fmt.Errorf("metrics type must be cluster, node or legacy")
	}
	token := ""
	if !public {
		if cfg.AccessKey == "" || cfg.SecretKey == "" || cfg.SessionToken != "" {
			return PrometheusConfig{}, fmt.Errorf("JWT metrics require static credentials; use --public only for a public metrics deployment")
		}
		jwt := jwtgo.NewWithClaims(jwtgo.SigningMethodHS512, jwtgo.StandardClaims{
			ExpiresAt: UTCNow().Add(defaultPrometheusJWTExpiry).Unix(),
			Subject:   cfg.AccessKey, Issuer: "prometheus",
		})
		token, err = jwt.SignedString([]byte(cfg.SecretKey))
		if err != nil {
			return PrometheusConfig{}, err
		}
	}
	return PrometheusConfig{ScrapeConfigs: []ScrapeConfig{{JobName: defaultJobName, BearerToken: token,
		MetricsPath: path, Scheme: u.Scheme, StaticConfigs: []StatConfig{{Targets: []string{u.Host}}}}}}, nil
}

func generatePrometheusConfig(ctx *cli.Context) error {
	alias := cleanAlias(ctx.Args().Get(0))
	if !isValidAlias(alias) {
		return fmt.Errorf("invalid alias")
	}
	_, _, hostConfig, aliasErr := expandAlias(alias)
	if aliasErr != nil {
		return aliasErr.ToGoError()
	}
	if hostConfig == nil {
		return fmt.Errorf("alias %q is not configured", alias)
	}
	client, err := newAdminClient(alias)
	if err != nil {
		return fmt.Errorf("initialize admin connection: %w", err.ToGoError())
	}
	info, infoErr := client.ServerInfo(globalContext)
	if infoErr != nil {
		return fmt.Errorf("get server info: %w", infoErr)
	}
	config, configErr := buildPrometheusConfig(hostConfig, info, ctx.String("metrics-type"), ctx.Bool("public"))
	if configErr != nil {
		return configErr
	}
	if err := setMetricsCA(&config, ctx.String("metrics-ca")); err != nil {
		return err
	}
	printMsg(config)
	return nil
}

// mainAdminPrometheus is the handle for "mc admin prometheus generate" sub-command.
func mainAdminPrometheusGenerate(ctx *cli.Context) error {

	console.SetColor("yaml", color.New(color.FgGreen))

	checkAdminPrometheusSyntax(ctx)

	return generatePrometheusConfig(ctx)
}
