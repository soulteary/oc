package cmd

import (
	"context"
	"encoding/json"
	"fmt"
	"net/url"
	"runtime"
	"runtime/debug"
	"time"

	"github.com/soulteary/mc/pkg/probe"
	"github.com/urfave/cli/v3"
)

var doctorCmd = &cli.Command{
	Name: "doctor", Usage: "show credential-free compatibility diagnostics",
	Action: commandAction(mainDoctor), Before: commandBefore(setGlobalsFromContext),
	OnUsageError: onUsageError,
	Flags:        append([]cli.Flag{&cli.BoolFlag{Name: "online", Usage: "also query server information (read-only, requires an alias)"}}, globalFlags...),
}

// Use an allowlist instead of serializing Config: even malformed or environment
// supplied endpoints must never expose credentials, queries or private paths.
type doctorReport struct {
	Status                  string   `json:"status"`
	ClientVersion           string   `json:"clientVersion"`
	GoVersion               string   `json:"goVersion"`
	Platform                string   `json:"platform"`
	AdminSDK                string   `json:"adminSDK,omitempty"`
	S3Scheme                string   `json:"s3Scheme,omitempty"`
	AdminScheme             string   `json:"adminScheme,omitempty"`
	SeparateAdmin           bool     `json:"separateAdmin"`
	CustomAdminCA           bool     `json:"customAdminCA"`
	CertificateVerification bool     `json:"certificateVerification"`
	Online                  bool     `json:"online"`
	ServerCount             int      `json:"serverCount,omitempty"`
	ServerVersions          []string `json:"serverVersions,omitempty"`
	ErrorCode               string   `json:"errorCode,omitempty"`
	ErrorCategory           string   `json:"errorCategory,omitempty"`
}

func (r doctorReport) JSON() string   { data, _ := json.Marshal(r); return string(data) }
func (r doctorReport) String() string { return r.JSON() }

func diagnosticScheme(endpoint string) string {
	u, err := url.Parse(endpoint)
	if err == nil && (u.Scheme == "http" || u.Scheme == "https") {
		return u.Scheme
	}
	return "invalid"
}

func mainDoctor(ctx *cli.Command) error {
	if ctx.NArg() > 1 || ctx.Bool("online") && ctx.NArg() != 1 {
		return doctorError("doctor accepts one optional alias; --online requires an alias")
	}
	report := doctorReport{Status: "success", ClientVersion: ReleaseTag, GoVersion: runtime.Version(),
		Platform: runtime.GOOS + "/" + runtime.GOARCH, CertificateVerification: !globalInsecure,
		Online: ctx.Bool("online")}
	if build, ok := debug.ReadBuildInfo(); ok {
		for _, dep := range build.Deps {
			if dep.Path == "github.com/soulteary/otterio" {
				report.AdminSDK = dep.Version
			}
		}
	}
	if ctx.NArg() == 1 {
		alias := ctx.Args().Get(0)
		if !isValidAlias(alias) {
			return doctorError("doctor requires a configured alias")
		}
		_, _, config, configErr := expandAlias(alias)
		if configErr != nil {
			// Environment aliases can contain credentials; do not print parse errors.
			return doctorError("unable to resolve diagnostic alias configuration")
		}
		if config == nil {
			return doctorError("doctor alias is not configured")
		}
		admin, ca := resolveAdminSettings(alias, config)
		report.S3Scheme, report.AdminScheme = diagnosticScheme(config.URL), diagnosticScheme(admin)
		report.SeparateAdmin = diagnosticSeparateAdmin(config.URL, admin)
		report.CustomAdminCA = ca != ""
		if report.Online {
			// Use the configuration already described by the report, rather than
			// resolving the alias and management overrides a second time.
			clientConfig := NewS3Config(admin, config)
			clientConfig.AdminCAFile = ca
			client, err := s3AdminNew(clientConfig)
			if err != nil {
				return doctorError("unable to initialize diagnostic connection")
			}
			requestCtx, cancel := context.WithTimeout(globalContext, 15*time.Second)
			defer cancel()
			info, serverErr := client.ServerInfo(requestCtx)
			if serverErr != nil {
				report.Status = "error"
				report.ErrorCode, report.ErrorCategory = classifyClientError(serverErr)
				printMsg(report)
				// The SDK's raw error and endpoint are deliberately excluded.
				return fmt.Errorf("online diagnostic failed (%s)", report.ErrorCategory)
			}
			report.ServerCount = len(info.Servers)
			for _, server := range info.Servers {
				report.ServerVersions = append(report.ServerVersions, server.Version)
			}
		}
	}
	printMsg(report)
	return nil
}

func diagnosticSeparateAdmin(s3Endpoint, adminEndpoint string) bool {
	s3, s3Err := validateAdminEndpoint(s3Endpoint)
	admin, adminErr := validateAdminEndpoint(adminEndpoint)
	if s3Err != nil || adminErr != nil {
		return s3Endpoint != adminEndpoint
	}
	// The admin client normalizes an optional root slash before connecting.
	return s3.String() != admin.String()
}

func doctorError(message string) error {
	err := fmt.Errorf("%s", message)
	errorIf(probe.NewError(err), "Unable to complete diagnostics.")
	return err
}
