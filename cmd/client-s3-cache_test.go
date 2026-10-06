package cmd

import (
	"context"
	"crypto/x509"
	"encoding/xml"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/soulteary/mc/pkg/probe"
)

func TestS3CacheIsolation(t *testing.T) {
	base := Config{HostURL: "http://localhost:9000/bucket/first", AccessKey: "abc",
		SecretKey: "defghi", SessionToken: "token", Signature: "s3v4"}
	for _, test := range []struct {
		name   string
		change func(*Config)
	}{
		{"scheme", func(c *Config) { c.HostURL = "https://localhost:9000/bucket/first" }},
		{"host", func(c *Config) { c.HostURL = "http://localhost:9001/bucket/first" }},
		{"credential boundaries", func(c *Config) { c.AccessKey, c.SecretKey = "abcd", "efghi" }},
		{"secret token boundaries", func(c *Config) { c.SecretKey, c.SessionToken = "defgh", "itoken" }},
		{"access key", func(c *Config) { c.AccessKey = "other" }},
		{"secret key", func(c *Config) { c.SecretKey = "othersecret" }},
		{"session token", func(c *Config) { c.SessionToken = "othertoken" }},
		{"signature", func(c *Config) { c.Signature = "s3v2" }},
		{"insecure", func(c *Config) { c.Insecure = true }},
		{"debug", func(c *Config) { c.Debug = true }},
		{"lookup", func(c *Config) { c.Lookup = minio.BucketLookupPath }},
		{"app name", func(c *Config) { c.AppName = "other" }},
		{"app version", func(c *Config) { c.AppVersion = "other" }},
		{"transport", func(c *Config) { c.Transport = &http.Transport{} }},
	} {
		t.Run(test.name, func(t *testing.T) {
			factory := newFactory()
			first, err := factory(&base)
			if err != nil {
				t.Fatal(err)
			}
			changed := base
			test.change(&changed)
			second, err := factory(&changed)
			if err != nil {
				t.Fatal(err)
			}
			if first.(*S3Client).api == second.(*S3Client).api {
				t.Fatal("different identities or connection settings shared an SDK client")
			}
		})
	}
}

// Exercise the same creation path used by mirror and inspect real requests,
// including concurrent use of both cached identities.
func TestS3CacheUserAgentIsolation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, "<ListAllMyBucketsResult><Buckets><Bucket><Name>")
		_ = xml.EscapeText(w, []byte(r.UserAgent()))
		fmt.Fprint(w, "</Name></Bucket></Buckets></ListAllMyBucketsResult>")
	}))
	defer server.Close()
	previousFactory, previousLoader := S3New, loadMcConfig
	S3New = newFactory()
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	t.Cleanup(func() { S3New, loadMcConfig = previousFactory, previousLoader })
	t.Setenv("OC_HOST_cachetest", strings.Replace(server.URL, "://", "://access:secret123@", 1))
	t.Setenv("OC_REGION", "us-east-1")
	normal, err := newClient("cachetest")
	if err != nil {
		t.Fatal(err)
	}
	mirror, err := newClientWithAppInfo("cachetest", uaMirrorAppName, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	again, err := newClient("cachetest")
	if err != nil {
		t.Fatal(err)
	}
	if normal.(*S3Client).api != again.(*S3Client).api || normal.(*S3Client).api == mirror.(*S3Client).api {
		t.Fatal("application-specific lookup changed the default cached client")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	readAgent := func(client Client) (string, error) {
		buckets, err := client.(*S3Client).api.ListBuckets(ctx)
		if err != nil {
			return "", err
		}
		if len(buckets) != 1 {
			return "", fmt.Errorf("unexpected response bucket count: %d", len(buckets))
		}
		return buckets[0].Name, nil
	}
	normalAgent, requestErr := readAgent(normal)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	mirrorAgent, requestErr := readAgent(mirror)
	if requestErr != nil {
		t.Fatal(requestErr)
	}
	if !strings.Contains(mirrorAgent, uaMirrorAppName+"/test-version") || strings.Contains(normalAgent, uaMirrorAppName+"/test-version") {
		t.Fatal("application information leaked between real requests")
	}
	var wg sync.WaitGroup
	for _, test := range []struct {
		client Client
		agent  string
	}{{again, normalAgent}, {mirror, mirrorAgent}} {
		wg.Add(1)
		go func(client Client, expected string) {
			defer wg.Done()
			for range 10 {
				agent, err := readAgent(client)
				if err != nil {
					t.Error(err)
					return
				}
				if agent != expected {
					t.Error("concurrent request used another application's user agent")
					return
				}
			}
		}(test.client, test.agent)
	}
	wg.Wait()
}

func TestClientAppInfoFilesystem(t *testing.T) {
	previousLoader := loadMcConfig
	loadMcConfig = func() (*configV10, *probe.Error) { return newConfigV10(), nil }
	t.Cleanup(func() { loadMcConfig = previousLoader })
	client, err := newClientWithAppInfo(t.TempDir(), uaMirrorAppName, "test-version")
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := client.(*fsClient); !ok {
		t.Fatal("filesystem target became an S3 client")
	}
}

func TestS3CacheReuse(t *testing.T) {
	factory := newFactory()
	config := Config{HostURL: "http://localhost:9000/bucket/first", AccessKey: "abc", SecretKey: "defghi", Signature: "s3v4"}
	first, err := factory(&config)
	if err != nil {
		t.Fatal(err)
	}
	config.HostURL = "http://localhost:9000/other/second"
	config.Signature = "S3V4"
	second, err := factory(&config)
	if err != nil {
		t.Fatal(err)
	}
	if first.(*S3Client).api != second.(*S3Client).api {
		t.Fatal("equivalent connection settings did not reuse the SDK client")
	}
	if first.GetURL().String() == second.GetURL().String() {
		t.Fatal("sharing the SDK client lost the per-object target URL")
	}
}

func TestS3CacheRegionAndRoots(t *testing.T) {
	t.Setenv("OC_REGION", "us-east-1")
	factory := newFactory()
	config := Config{HostURL: "https://localhost:9000", AccessKey: "abc", SecretKey: "defghi", Signature: "s3v4"}
	first, err := factory(&config)
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv("OC_REGION", "eu-west-1")
	second, err := factory(&config)
	if err != nil {
		t.Fatal(err)
	}
	if first.(*S3Client).api == second.(*S3Client).api {
		t.Fatal("different regions shared an SDK client")
	}
	originalRoots := globalRootCAs
	t.Cleanup(func() { globalRootCAs = originalRoots })
	globalRootCAs = x509.NewCertPool()
	third, err := factory(&config)
	if err != nil {
		t.Fatal(err)
	}
	if second.(*S3Client).api == third.(*S3Client).api {
		t.Fatal("different CA pools shared an SDK client")
	}
}
