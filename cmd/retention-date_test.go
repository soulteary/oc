package cmd

import (
	"math"
	"testing"
	"time"

	minio "github.com/soulteary/otterio-sdk/v7"
)

func TestGetRetainUntilDateFromDateBoundaries(t *testing.T) {
	base := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		now      time.Time
		validity uint64
		unit     minio.ValidityUnit
		want     string
	}{
		{"last year", base, 7973, minio.Years, "9999-10-07T12:00:00Z"},
		{"last day", base, 2912163, minio.Days, "9999-12-31T12:00:00Z"},
		{"year ends at last day", time.Date(9998, time.December, 31, 23, 59, 59, 0, time.UTC), 1, minio.Years, "9999-12-31T23:59:59Z"},
		{"day ends at last day", time.Date(9999, time.December, 30, 23, 59, 59, 123456789, time.UTC), 1, minio.Days, "9999-12-31T23:59:59Z"},
		{"leap day", time.Date(2024, time.February, 28, 12, 0, 0, 0, time.UTC), 1, minio.Days, "2024-02-29T12:00:00Z"},
		{"after leap day", time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC), 1, minio.Days, "2024-03-01T12:00:00Z"},
		{"leap year normalization", time.Date(2024, time.February, 29, 12, 0, 0, 0, time.UTC), 1, minio.Years, "2025-03-01T12:00:00Z"},
		{"UTC date boundary", time.Date(2026, time.October, 7, 1, 0, 0, 0, time.FixedZone("UTC+8", 8*60*60)), 2912164, minio.Days, "9999-12-31T17:00:00Z"},
		{"year zero", time.Date(0, time.January, 1, 12, 0, 0, 0, time.UTC), 9999, minio.Years, "9999-01-01T12:00:00Z"},
	} {
		t.Run(tc.name, func(t *testing.T) {
			date, err := getRetainUntilDateFrom(tc.now, tc.validity, tc.unit)
			if err != nil {
				t.Fatal(err)
			}
			if date != tc.want {
				t.Fatalf("date = %q; want %q", date, tc.want)
			}
			got, parseErr := time.Parse(time.RFC3339, date)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if !got.After(tc.now) {
				t.Fatalf("retain-until date %s is not after %s", got, tc.now)
			}
		})
	}
}

func TestGetRetainUntilDateFromRejectsInvalidDates(t *testing.T) {
	base := time.Date(2026, time.October, 7, 12, 0, 0, 0, time.UTC)
	for _, tc := range []struct {
		name     string
		now      time.Time
		validity uint64
		unit     minio.ValidityUnit
	}{
		{"year 10000", base, 7974, minio.Years},
		{"day in year 10000", base, 2912164, minio.Days},
		{"no year remaining", time.Date(9999, time.January, 1, 0, 0, 0, 0, time.UTC), 1, minio.Years},
		{"no day remaining", time.Date(9999, time.December, 31, 0, 0, 0, 0, time.UTC), 1, minio.Days},
		{"empty unit", base, 1, ""},
		{"unknown unit", base, 1, "Weeks"},
		{"negative base year", time.Date(-1, time.January, 1, 0, 0, 0, 0, time.UTC), 1, minio.Days},
		{"base year 10000", time.Date(10000, time.January, 1, 0, 0, 0, 0, time.UTC), 1, minio.Years},
	} {
		t.Run(tc.name, func(t *testing.T) {
			date, err := getRetainUntilDateFrom(tc.now, tc.validity, tc.unit)
			if err == nil || date != "" {
				t.Fatalf("date = %q, error = %v; want empty date and error", date, err)
			}
		})
	}

	for _, unit := range []minio.ValidityUnit{minio.Days, minio.Years} {
		for _, validity := range []uint64{0, math.MaxInt32, math.MaxInt32 + 1, math.MaxUint64} {
			date, err := getRetainUntilDateFrom(base, validity, unit)
			if err == nil || date != "" {
				t.Fatalf("getRetainUntilDateFrom(%d, %s) = %q, %v; want empty date and error", validity, unit, date, err)
			}
		}
	}
}
