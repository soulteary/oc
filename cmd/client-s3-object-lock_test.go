package cmd

import (
	"context"
	"encoding/xml"
	"io"
	"math"
	"net/http"
	"strings"
	"testing"

	minio "github.com/soulteary/otterio-sdk/v7"
)

type ObjectLockTestRoundTripper func(*http.Request) (*http.Response, error)

func (f ObjectLockTestRoundTripper) RoundTrip(r *http.Request) (*http.Response, error) {
	return f(r)
}

type ObjectLockTestConfig struct {
	XMLName           xml.Name `xml:"ObjectLockConfiguration"`
	ObjectLockEnabled string   `xml:"ObjectLockEnabled"`
	Rule              *struct {
		DefaultRetention struct {
			Mode  minio.RetentionMode `xml:"Mode"`
			Days  *uint64             `xml:"Days"`
			Years *uint64             `xml:"Years"`
		} `xml:"DefaultRetention"`
	} `xml:"Rule"`
}

func ObjectLockTestClient(t *testing.T) (*S3Client, *[]ObjectLockTestConfig) {
	t.Helper()
	var requests []ObjectLockTestConfig
	transport := ObjectLockTestRoundTripper(func(r *http.Request) (*http.Response, error) {
		if r.Method != http.MethodPut || r.URL.Path != "/bucket/" {
			t.Fatalf("unexpected object lock request: %s %s", r.Method, r.URL)
		}
		if _, ok := r.URL.Query()["object-lock"]; !ok {
			t.Fatalf("request missing object-lock query: %s", r.URL)
		}
		var config ObjectLockTestConfig
		if err := xml.NewDecoder(r.Body).Decode(&config); err != nil {
			t.Fatal(err)
		}
		requests = append(requests, config)
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader("")),
			Request:    r,
		}, nil
	})
	sdk, err := minio.New("localhost", &minio.Options{
		Secure:       false,
		Region:       "us-east-1",
		Transport:    transport,
		BucketLookup: minio.BucketLookupPath,
	})
	if err != nil {
		t.Fatal(err)
	}
	client := &S3Client{
		api:       sdk,
		targetURL: &ClientURL{Scheme: "http", Host: "localhost", Path: "/bucket", Separator: '/'},
	}
	return client, &requests
}

func TestObjectLockConfigRequest(t *testing.T) {
	for _, test := range []struct {
		name     string
		mode     minio.RetentionMode
		validity uint64
		unit     minio.ValidityUnit
	}{
		{"days", minio.Governance, 30, minio.Days},
		{"years", minio.Compliance, 1, minio.Years},
		{"max days", minio.Compliance, math.MaxInt32, minio.Days},
		{"max years", minio.Governance, math.MaxInt32, minio.Years},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := ObjectLockTestClient(t)
			if err := client.SetObjectLockConfig(context.Background(), test.mode, test.validity, test.unit); err != nil {
				t.Fatal(err)
			}
			if len(*requests) != 1 {
				t.Fatalf("sent %d requests; want 1", len(*requests))
			}
			config := (*requests)[0]
			if config.ObjectLockEnabled != "Enabled" || config.Rule == nil {
				t.Fatalf("unexpected object lock configuration: %+v", config)
			}
			retention := config.Rule.DefaultRetention
			if retention.Mode != test.mode {
				t.Fatalf("retention mode = %s; want %s", retention.Mode, test.mode)
			}
			value, other := retention.Days, retention.Years
			if test.unit == minio.Years {
				value, other = retention.Years, retention.Days
			}
			if value == nil || *value != test.validity || other != nil {
				t.Fatalf("retention = %+v; want only %s=%d", retention, test.unit, test.validity)
			}
		})
	}
}

func TestObjectLockConfigClear(t *testing.T) {
	client, requests := ObjectLockTestClient(t)
	if err := client.SetObjectLockConfig(context.Background(), "", 0, ""); err != nil {
		t.Fatal(err)
	}
	if len(*requests) != 1 {
		t.Fatalf("sent %d requests; want 1", len(*requests))
	}
	config := (*requests)[0]
	if config.ObjectLockEnabled != "Enabled" || config.Rule != nil {
		t.Fatalf("clear configuration = %+v; want Enabled with no Rule", config)
	}
}

func TestObjectLockConfigRejectsInvalidArguments(t *testing.T) {
	for _, test := range []struct {
		name     string
		mode     minio.RetentionMode
		validity uint64
		unit     minio.ValidityUnit
	}{
		{"zero days", minio.Governance, 0, minio.Days},
		{"zero years", minio.Compliance, 0, minio.Years},
		{"overflow days", minio.Governance, math.MaxInt32 + 1, minio.Days},
		{"overflow years", minio.Compliance, math.MaxInt32 + 1, minio.Years},
		{"missing mode", "", 1, minio.Days},
		{"missing unit", minio.Governance, 1, ""},
		{"incomplete clear unit", "", 0, minio.Days},
		{"incomplete clear mode", minio.Governance, 0, ""},
		{"incomplete clear validity", "", 1, ""},
		{"invalid mode", "INVALID", 1, minio.Days},
		{"invalid unit", minio.Governance, 1, "MONTHS"},
	} {
		t.Run(test.name, func(t *testing.T) {
			client, requests := ObjectLockTestClient(t)
			if err := client.SetObjectLockConfig(context.Background(), test.mode, test.validity, test.unit); err == nil {
				t.Fatal("accepted invalid object lock configuration")
			}
			if len(*requests) != 0 {
				t.Fatalf("sent %d requests for invalid arguments; want 0", len(*requests))
			}
		})
	}
}
