// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestFeatureBucketsUseSingleS3WriteAndPreserveNonemptyBucket(t *testing.T) {
	var puts, deletes atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Path != "/exact-bucket/" && r.URL.Path != "/exact-bucket" {
			t.Errorf("unexpected target %s", r.URL.Path)
		}
		switch r.Method {
		case http.MethodPut:
			puts.Add(1)
			w.WriteHeader(200)
		case http.MethodDelete:
			deletes.Add(1)
			if r.Header.Get("X-Minio-Force-Delete") != "" || r.URL.RawQuery != "" {
				t.Error("bucket deletion added force or query")
			}
			w.Header().Set("Content-Type", "application/xml")
			w.WriteHeader(409)
			fmt.Fprint(w, `<Error><Code>BucketNotEmpty</Code><Message>secret upstream marker</Message></Error>`)
		default:
			t.Errorf("unexpected S3 method %s", r.Method)
		}
	}))
	defer upstream.Close()
	c := testClient(t, Config{S3URL: upstream.URL})
	if err := c.CreateBucket(context.Background(), "exact-bucket"); err != nil {
		t.Fatal(err)
	}
	assertAPIError(t, c.DeleteBucket(context.Background(), "exact-bucket"), 409, "bucket_not_empty")
	if puts.Load() != 1 || deletes.Load() != 1 {
		t.Fatal("bucket write was replayed or precleared")
	}
	for _, invalid := range []string{"ab", "Bad-Bucket", "127.0.0.1", "a..b", "-bad-name", "bad-.name"} {
		assertAPIError(t, c.CreateBucket(context.Background(), invalid), 400, "InvalidRequest")
	}
}

func TestFeatureVersionsPreserveExactKeyOrderNullAndBothMarkers(t *testing.T) {
	key, version := "folder/a+中文.txt", "v+/?=%"
	marker := key + "[otterio_cache:v1,id:opaque]"
	var pages atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		q := r.URL.Query()
		w.Header().Set("X-Otterio-Version-Authorization", "v1")
		if !q.Has("versions") || q.Get("prefix") != key || q.Get("max-keys") != "2" || q.Get("encoding-type") != "url" {
			t.Error("version query altered exact prefix/limit")
		}
		switch pages.Add(1) {
		case 1:
			if q.Has("key-marker") || q.Has("version-id-marker") {
				t.Error("first request had markers")
			}
			fmt.Fprintf(w, `<ListVersionsResult><EncodingType>url</EncodingType><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextVersionIdMarker>v+/?=%%</NextVersionIdMarker><DeleteMarker><Key>%s</Key><VersionId>deleted</VersionId><IsLatest>true</IsLatest><LastModified>2026-10-08T00:00:00Z</LastModified></DeleteMarker><Version><Key>%s</Key><VersionId>null</VersionId><IsLatest>false</IsLatest><Size>7</Size><ETag>"etag"</ETag><LastModified>2026-10-07T00:00:00Z</LastModified></Version></ListVersionsResult>`, url.QueryEscape(marker), url.QueryEscape(key), url.QueryEscape(key))
		case 2:
			if q.Get("key-marker") != marker || q.Get("version-id-marker") != version {
				t.Error("opaque version markers were changed")
			}
			fmt.Fprintf(w, `<ListVersionsResult><EncodingType>url</EncodingType><IsTruncated>true</IsTruncated><NextKeyMarker>%s</NextKeyMarker><NextVersionIdMarker>neighbor</NextVersionIdMarker><Version><Key>%s</Key><VersionId>old</VersionId><Size>3</Size><ETag>"old-etag"</ETag></Version><Version><Key>%s</Key><VersionId>neighbor</VersionId><Size>4</Size></Version></ListVersionsResult>`, url.QueryEscape(key+".neighbor"), url.QueryEscape(key), url.QueryEscape(key+".neighbor"))
		default:
			t.Error("version page was prefetched")
		}
	}))
	defer upstream.Close()
	c := testClient(t, Config{S3URL: upstream.URL})
	first, err := c.ListVersions(context.Background(), "bucket", key, "", 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Entries) != 2 || !first.Entries[0].DeleteMarker || !first.Entries[0].Latest || first.Entries[1].VersionID != "null" || first.NextCursor == "" {
		t.Fatalf("version order/null/deletion changed: %#v", first)
	}
	_, err = c.ListVersions(context.Background(), "another-bucket", key, first.NextCursor, 2)
	assertAPIError(t, err, 400, "InvalidRequest")
	second, err := c.ListVersions(context.Background(), "bucket", key, first.NextCursor, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(second.Entries) != 1 || second.Entries[0].VersionID != "old" || second.NextCursor != "" || pages.Load() != 2 {
		t.Fatalf("neighbor escaped exact key page: %#v", second)
	}
}

func TestFeatureVersionCancellationClosesOwnedRequest(t *testing.T) {
	started, ended := make(chan struct{}), make(chan struct{})
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		close(started)
		<-r.Context().Done()
		close(ended)
	}))
	defer upstream.Close()
	c := testClient(t, Config{S3URL: upstream.URL})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, err := c.ListVersions(ctx, "bucket", "key", "", 2); done <- err }()
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("version request did not start")
	}
	cancel()
	select {
	case err := <-done:
		assertAPIError(t, err, 499, "Canceled")
	case <-time.After(time.Second):
		t.Fatal("canceled version request did not return")
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("version request did not close")
	}
}

func TestFeatureReferenceCarriesVersionAndConditionalReadWithoutFallback(t *testing.T) {
	var heads, gets atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Query().Get("versionId") != "old+version" {
			t.Error("version was dropped")
		}
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("X-Otterio-Version-Authorization", "v1")
		w.Header().Set("X-Amz-Version-Id", "old+version")
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
		if r.Method == http.MethodHead {
			heads.Add(1)
			return
		}
		gets.Add(1)
		if r.Header.Get("If-Match") != `"etag"` {
			t.Error("version download lacks fixed ETag")
		}
		fmt.Fprint(w, "old")
	}))
	defer upstream.Close()
	c := testClient(t, Config{S3URL: upstream.URL})
	ref := consoleapi.ObjectRef{Bucket: "bucket", Key: "key", VersionID: "old+version"}
	info, err := c.StatReference(context.Background(), ref)
	if err != nil || info.VersionID != ref.VersionID || info.Size != 3 {
		t.Fatalf("stat failed: %#v %v", info, err)
	}
	object, err := c.OpenReference(context.Background(), ref, info.ETag)
	if err != nil {
		t.Fatal(err)
	}
	data, err := io.ReadAll(object.Body)
	_ = object.Body.Close()
	if err != nil || string(data) != "old" || heads.Load() != 1 || gets.Load() != 1 {
		t.Fatal("version open repeated HEAD or returned wrong body")
	}
}

func TestFeatureReferenceRejectsMissingOrChangedVersion(t *testing.T) {
	for _, scenario := range []string{"missing", "ignored", "changed"} {
		t.Run(scenario, func(t *testing.T) {
			var gets atomic.Int32
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				gets.Add(1)
				if r.Method != http.MethodGet || r.URL.Query().Get("versionId") != "old" {
					t.Error("version read fell back")
				}
				if scenario == "missing" {
					w.WriteHeader(404)
					fmt.Fprint(w, `<Error><Code>NoSuchVersion</Code></Error>`)
					return
				}
				etag := "etag"
				w.Header().Set("X-Otterio-Version-Authorization", "v1")
				if scenario == "changed" {
					etag = "new-etag"
					w.Header().Set("X-Amz-Version-Id", "old")
				}
				w.Header().Set("ETag", `"`+etag+`"`)
				w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
				fmt.Fprint(w, "new")
			}))
			defer upstream.Close()
			c := testClient(t, Config{S3URL: upstream.URL})
			_, err := c.OpenReference(context.Background(), consoleapi.ObjectRef{Bucket: "bucket", Key: "key", VersionID: "old"}, "etag")
			status, code := 409, "object_changed"
			if scenario == "missing" {
				status, code = 404, "version_unavailable"
			}
			assertAPIError(t, err, status, code)
			if gets.Load() != 1 {
				t.Fatal("unavailable version was retried")
			}
		})
	}
}

func TestFeaturePresignSignsPublicHostAndVersionWithoutPublicNetworkRequest(t *testing.T) {
	var heads atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.Method != http.MethodHead || r.URL.Query().Get("versionId") != "null" {
			t.Error("sharing did not authorize exact null version")
		}
		heads.Add(1)
		w.Header().Set("X-Amz-Version-Id", "null")
		w.Header().Set("X-Otterio-Version-Authorization", "v1")
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
	}))
	defer upstream.Close()
	c := testClient(t, Config{S3URL: upstream.URL, ShareURL: "https://public-storage.invalid:9443"})
	args := consoleapi.ShareRequest{ObjectRef: consoleapi.ObjectRef{Bucket: "bucket", Key: "a+ 中文.txt", VersionID: "null"}, ExpiresSeconds: 3600, DownloadName: "中文下载.txt"}
	share, err := c.Presign(context.Background(), args)
	if err != nil {
		t.Fatal(err)
	}
	u, err := url.Parse(share.URL)
	if err != nil || u.Host != "public-storage.invalid:9443" || u.Scheme != "https" || u.Path != "/bucket/a+ 中文.txt" {
		t.Fatal("share endpoint or exact key changed")
	}
	q := u.Query()
	if q.Get("versionId") != "null" || q.Get("X-Amz-Expires") != "3600" || q.Get("X-Amz-Signature") == "" || !strings.Contains(q.Get("response-content-disposition"), "attachment") || heads.Load() != 1 {
		t.Fatal("share signature/expiry/version missing")
	}
	signedAt, _ := time.Parse("20060102T150405Z", q.Get("X-Amz-Date"))
	if !share.ExpiresAt.Equal(signedAt.Add(time.Hour)) {
		t.Fatal("share expiry differs from signed URL")
	}
	args.ExpiresSeconds = 604801
	_, err = c.Presign(context.Background(), args)
	assertAPIError(t, err, 400, "InvalidRequest")
	if heads.Load() != 1 {
		t.Fatal("invalid share reached upstream")
	}
}

func TestFeatureShareEndpointRejectsCredentialsPrefixesAndFragments(t *testing.T) {
	for _, endpoint := range []string{"https://access:secret@example.com", "https://example.com/storage", "https://example.com?x=1", "https://example.com#part"} {
		_, err := New(Config{S3URL: "https://s3.example.com", ShareURL: endpoint})
		assertAPIError(t, err, 400, "InvalidConfiguration")
	}
}

func TestFeatureLegacyServerCannotExposeVersionBytesOrShares(t *testing.T) {
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Query().Has("versions") {
			fmt.Fprint(w, `<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`)
			return
		}
		w.Header().Set("X-Amz-Version-Id", "old")
		w.Header().Set("ETag", `"etag"`)
		w.Header().Set("Content-Length", "3")
		w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
		if r.Method == http.MethodGet {
			fmt.Fprint(w, "old")
		}
	}))
	defer upstream.Close()
	c := testClient(t, Config{S3URL: upstream.URL})
	ref := consoleapi.ObjectRef{Bucket: "bucket", Key: "key", VersionID: "old"}
	_, err := c.StatReference(context.Background(), ref)
	assertAPIError(t, err, 501, "versions_unsupported")
	object, err := c.OpenReference(context.Background(), ref, "etag")
	assertAPIError(t, err, 501, "versions_unsupported")
	if object.Body != nil {
		t.Fatal("legacy server leaked an historical stream")
	}
	_, err = c.Presign(context.Background(), consoleapi.ShareRequest{ObjectRef: ref, ExpiresSeconds: 60})
	assertAPIError(t, err, 501, "versions_unsupported")
	_, err = c.ListVersions(context.Background(), "bucket", "key", "", 10)
	assertAPIError(t, err, 501, "versions_unsupported")
	// Current-object readers retain current-object permissions and can still
	// use an ETag-protected stream on an unpatched server.
	object, err = c.OpenReference(context.Background(), consoleapi.ObjectRef{Bucket: "bucket", Key: "key"}, "etag")
	if err != nil {
		t.Fatal(err)
	}
	_ = object.Body.Close()
}

func TestFeatureBucketCreationRefusesSDKRegionWriteReplay(t *testing.T) {
	var puts atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		puts.Add(1)
		w.WriteHeader(400)
		fmt.Fprint(w, `<Error><Code>AuthorizationHeaderMalformed</Code><Region>different-region</Region></Error>`)
	}))
	defer upstream.Close()
	c := testClient(t, Config{S3URL: upstream.URL})
	if err := c.CreateBucket(context.Background(), "bucket"); err == nil {
		t.Fatal("regional rejection appeared successful")
	}
	if puts.Load() != 1 {
		t.Fatal("SDK replayed a bucket creation")
	}
}

func TestVersionSupportProbeUsesZeroEntryReadAndServerAcknowledgement(t *testing.T) {
	for _, tc := range []struct {
		name    string
		status  int
		header  string
		body    string
		want    bool
		wantErr bool
	}{
		{"supported-empty-unversioned", 200, "v1", `<ListVersionsResult><IsTruncated>false</IsTruncated></ListVersionsResult>`, true, false},
		{"filesystem-unsupported", 501, "", `<Error><Code>NotImplemented</Code></Error>`, false, false},
		{"old-server-without-authorization", 200, "", `<ListVersionsResult/>`, false, false},
		{"denied-is-not-support", 403, "", `<Error><Code>AccessDenied</Code></Error>`, false, true},
		{"malformed", 200, "v1", `<html/>`, false, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method != "GET" || !r.URL.Query().Has("versions") || r.URL.Query().Get("max-keys") != "0" {
					t.Errorf("non-probe request: %s %s", r.Method, r.URL)
				}
				w.Header().Set("X-Otterio-Version-Authorization", tc.header)
				w.WriteHeader(tc.status)
				fmt.Fprint(w, tc.body)
			}))
			defer upstream.Close()
			client := testClient(t, Config{S3URL: upstream.URL})
			supported, err := client.VersionSupported(context.Background(), "bucket")
			if supported != tc.want || (err != nil) != tc.wantErr {
				t.Fatalf("supported=%v err=%v", supported, err)
			}
		})
	}
}

func TestRenameCopiesConditionallyBeforeDeletingAndPreservesSourceOnFailure(t *testing.T) {
	for _, fail := range []bool{false, true} {
		t.Run(fmt.Sprint(fail), func(t *testing.T) {
			var operations []string
			upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				switch r.Method {
				case "GET":
					fmt.Fprint(w, `<ListBucketResult><Name>exact-bucket</Name><Contents><Key>old.txt</Key><Size>3</Size><ETag>"etag"</ETag></Contents></ListBucketResult>`)
				case "HEAD":
					if strings.HasSuffix(r.URL.Path, "/new.txt") {
						w.Header().Set("X-Otterio-Conditional-Writes", "v1")
						w.WriteHeader(404)
						return
					}
					w.Header().Set("ETag", `"etag"`)
					w.Header().Set("Content-Length", "3")
					w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
				case "PUT":
					operations = append(operations, "copy")
					if r.Header.Get("If-None-Match") != "*" || r.Header.Get("X-Amz-Copy-Source-If-Match") != `"etag"` {
						t.Error("missing copy preconditions")
					}
					if fail {
						w.WriteHeader(403)
						fmt.Fprint(w, `<Error><Code>AccessDenied</Code></Error>`)
						return
					}
					fmt.Fprint(w, `<CopyObjectResult><ETag>"etag"</ETag><LastModified>2026-10-08T00:00:00Z</LastModified></CopyObjectResult>`)
				case "DELETE":
					operations = append(operations, "delete")
					w.WriteHeader(204)
				default:
					t.Error(r.Method)
				}
			}))
			defer upstream.Close()
			c := testClient(t, Config{S3URL: upstream.URL})
			page, err := c.ListObjects(context.Background(), "exact-bucket", "", "", 10)
			if err != nil || len(page.Entries) != 1 {
				t.Fatal(page, err)
			}
			etag := page.Entries[0].ETag
			// A browser with an already loaded list can still send the old wire value.
			if fail {
				etag = `"` + etag + `"`
			}
			err = c.RenameObject(context.Background(), "exact-bucket", page.Entries[0].Key, "new.txt", etag)
			if fail {
				if err == nil || strings.Join(operations, ",") != "copy" {
					t.Fatal(err, operations)
				}
			} else if err != nil || strings.Join(operations, ",") != "copy,delete" {
				t.Fatal(err, operations)
			}
		})
	}
}

func TestRenameETagNormalizationRejectsMalformedHeaders(t *testing.T) {
	for _, etag := range []string{"etag", `"etag"`} {
		if got := canonicalETag(etag); got != "etag" || !validETag(got) {
			t.Fatal(etag, got)
		}
	}
	for _, etag := range []string{`""`, `"etag`, `etag"`, `""etag""`, "\"etag\r\nX-Test: injected\""} {
		if validETag(canonicalETag(etag)) {
			t.Fatal("accepted unsafe etag", etag)
		}
	}
}
