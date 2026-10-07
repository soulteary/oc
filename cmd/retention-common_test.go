package cmd

import (
	"context"
	"strconv"
	"testing"
	"time"

	"github.com/minio/minio-go/v7"
)

func TestGetRetainUntilDateRejectsOverflow(t *testing.T) {
	maxInt := uint64(^uint(0) >> 1)
	for _, unit := range []minio.ValidityUnit{minio.Days, minio.Years} {
		for _, validity := range []uint64{0, maxInt + 1, ^uint64(0)} {
			t.Run(string(unit)+"/"+strconv.FormatUint(validity, 10), func(t *testing.T) {
				date, err := getRetainUntilDate(validity, unit)
				if err == nil || date != "" {
					t.Fatalf("getRetainUntilDate(%d, %s) = %q, %v; want empty date and error", validity, unit, date, err)
				}
			})
		}
	}
}

func TestGetRetainUntilDate(t *testing.T) {
	for _, unit := range []minio.ValidityUnit{minio.Days, minio.Years} {
		t.Run(string(unit), func(t *testing.T) {
			before := UTCNow().Truncate(time.Second)
			date, err := getRetainUntilDate(1, unit)
			if err != nil {
				t.Fatal(err)
			}
			after := UTCNow().Truncate(time.Second)
			got, parseErr := time.Parse(time.RFC3339, date)
			if parseErr != nil {
				t.Fatal(parseErr)
			}
			if unit == minio.Years {
				before, after = before.AddDate(1, 0, 0), after.AddDate(1, 0, 0)
			} else {
				before, after = before.AddDate(0, 0, 1), after.AddDate(0, 0, 1)
			}
			if got.Before(before) || got.After(after) {
				t.Fatalf("retain-until date %s outside [%s, %s]", got, before, after)
			}
		})
	}
}

func TestSetObjectLockConfigRejectsOverflow(t *testing.T) {
	client := &S3Client{targetURL: &ClientURL{Scheme: "http", Host: "localhost", Path: "/bucket"}}
	for _, unit := range []minio.ValidityUnit{minio.Days, minio.Years} {
		for _, validity := range []uint64{uint64(1) << 31, uint64(^uint32(0)) + 1, ^uint64(0)} {
			t.Run(string(unit)+"/"+strconv.FormatUint(validity, 10), func(t *testing.T) {
				// The nil SDK client ensures rejection happens before any API call.
				if err := client.SetObjectLockConfig(context.Background(), minio.Governance, validity, unit); err == nil {
					t.Fatalf("SetObjectLockConfig accepted overflowing validity %d", validity)
				}
			})
		}
	}
}
