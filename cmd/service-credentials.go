package cmd

import "fmt"

func validateServiceCredentials(access, secret string) error {
	if access == "" && secret == "" {
		return nil
	}
	if len(access) < 3 || len(access) > 20 {
		return fmt.Errorf("service access key must contain 3 to 20 bytes")
	}
	if len(secret) < 8 || len(secret) > 40 {
		return fmt.Errorf("service secret key must contain 8 to 40 bytes")
	}
	return nil
}
