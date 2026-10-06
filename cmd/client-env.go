package cmd

import (
	"os"
	"strings"
)

// OC values win even when explicitly empty; legacy MC values remain supported.
func lookupClientEnv(legacy string) (string, bool) {
	if value, ok := os.LookupEnv("OC_" + strings.TrimPrefix(legacy, "MC_")); ok {
		return value, true
	}
	return os.LookupEnv(legacy)
}

func clientEnv(legacy string) string {
	value, _ := lookupClientEnv(legacy)
	return value
}
