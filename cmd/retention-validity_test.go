package cmd

import (
	"strings"
	"testing"

	"github.com/minio/minio-go/v7"
)

func TestParseRetentionValidity(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		validity uint64
		unit     minio.ValidityUnit
		wantErr  bool
	}{
		{name: "empty", input: "", wantErr: true},
		{name: "single digit", input: "1", wantErr: true},
		{name: "days without number", input: "d", wantErr: true},
		{name: "years without number", input: "Y", wantErr: true},
		{name: "negative", input: "-1d", wantErr: true},
		{name: "negative zero", input: "-0y", wantErr: true},
		{name: "positive sign", input: "+1d", wantErr: true},
		{name: "zero days", input: "0d", wantErr: true},
		{name: "zero years", input: "0Y", wantErr: true},
		{name: "lowercase days", input: "30d", validity: 30, unit: minio.Days},
		{name: "uppercase days", input: "30D", validity: 30, unit: minio.Days},
		{name: "lowercase years", input: "3y", validity: 3, unit: minio.Years},
		{name: "uppercase years", input: "3Y", validity: 3, unit: minio.Years},
		{name: "leading zeroes", input: "0001d", validity: 1, unit: minio.Days},
		{name: "unknown unit", input: "1m", wantErr: true},
		{name: "missing unit", input: "30", wantErr: true},
		{name: "extra unit", input: "1dd", wantErr: true},
		{name: "leading whitespace", input: " 1d", wantErr: true},
		{name: "inner whitespace", input: "1 d", wantErr: true},
		{name: "trailing whitespace", input: "1d ", wantErr: true},
		{name: "trailing newline", input: "1d\n", wantErr: true},
		{name: "decimal fraction", input: "1.5d", wantErr: true},
		{name: "hexadecimal", input: "0x10d", wantErr: true},
		{name: "non-ASCII unit", input: "1日", wantErr: true},
		{name: "binary input", input: "\x00d", wantErr: true},
		{name: "MaxInt32 days", input: "2147483647d", validity: 2147483647, unit: minio.Days},
		{name: "MaxInt32 years", input: "2147483647Y", validity: 2147483647, unit: minio.Years},
		{name: "MaxInt32 plus one", input: "2147483648d", wantErr: true},
		{name: "MaxUint64", input: "18446744073709551615y", wantErr: true},
		{name: "huge number", input: strings.Repeat("9", 4096) + "d", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			validity, unit, err := parseRetentionValidity(tt.input)
			if tt.wantErr {
				if err == nil || validity != 0 || unit != "" {
					t.Fatalf("parseRetentionValidity(%q) = (%d, %q, %v); want (0, empty unit, error)", tt.input, validity, unit, err)
				}
				return
			}
			if err != nil || validity != tt.validity || unit != tt.unit {
				t.Fatalf("parseRetentionValidity(%q) = (%d, %q, %v); want (%d, %q, nil)", tt.input, validity, unit, err, tt.validity, tt.unit)
			}
		})
	}
}

func FuzzParseRetentionValidity(f *testing.F) {
	for _, input := range []string{"", "d", "1d", "3Y", "-1d", "+1d", "0d", "1m", "2147483647d", "2147483648y", "18446744073709551615d", "\x00\xff"} {
		f.Add(input)
	}
	f.Fuzz(func(t *testing.T, input string) {
		validity, unit, err := parseRetentionValidity(input)
		if err != nil {
			if validity != 0 || unit != "" {
				t.Fatalf("invalid input %q returned validity %d and unit %q", input, validity, unit)
			}
			return
		}
		if validity == 0 || validity > 2147483647 || (unit != minio.Days && unit != minio.Years) {
			t.Fatalf("input %q returned invalid validity %d or unit %q", input, validity, unit)
		}
	})
}
