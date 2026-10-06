package cmd

import (
	"strings"
	"testing"
)

func TestDiagnosticEndpointAllowlist(t *testing.T) {
	for _, endpoint := range []string{
		"https://access:secret@private.example:9001/path?token=session&signature=signed#fragment",
		"http://access:secret@localhost:9000", "invalid://secret", "%secret",
	} {
		report := doctorReport{S3Scheme: diagnosticScheme(endpoint), AdminScheme: diagnosticScheme(endpoint)}
		for _, secret := range []string{"access", "secret", "private.example", "session", "signed", "fragment"} {
			if strings.Contains(report.JSON(), secret) {
				t.Fatalf("diagnostic exposed %q", secret)
			}
		}
	}
}
