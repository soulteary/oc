// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func testClient(t *testing.T, cfg Config) *Client {
	t.Helper()
	if cfg.AccessKey == "" {
		cfg.AccessKey, cfg.SecretKey = "test-access", "secret-marker-test-key"
	}
	if cfg.Path == "" {
		cfg.Path = "on"
	}
	client, err := New(cfg)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client
}

func locationResponse(w http.ResponseWriter, r *http.Request) bool {
	if r.URL.Query().Has("location") {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<LocationConstraint xmlns="http://s3.amazonaws.com/doc/2006-03-01/">us-east-1</LocationConstraint>`)
		return true
	}
	return false
}

func TestListObjectsUsesOnePageAndPreservesOpaqueCursor(t *testing.T) {
	var requests atomic.Int32
	cursor := "opaque+/?=%cursor"
	prefix := "folder//../literal%2F+?#/"
	key := prefix + "中文 空格.txt"
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		n := requests.Add(1)
		query := r.URL.Query()
		if query.Get("list-type") != "2" || query.Get("prefix") != prefix || query.Get("max-keys") != "2" || query.Get("delimiter") != "/" {
			t.Error("pagination request changed prefix, limit, delimiter or protocol")
		}
		if (n == 1 && query.Get("continuation-token") != "") || (n == 2 && query.Get("continuation-token") != cursor) {
			t.Error("opaque cursor was changed")
		}
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprintf(w, `<ListBucketResult><Name>bucket</Name><EncodingType>url</EncodingType><IsTruncated>true</IsTruncated><NextContinuationToken>%s</NextContinuationToken><CommonPrefixes><Prefix>%s</Prefix></CommonPrefixes><Contents><Key>%s</Key><LastModified>2026-10-08T00:00:00Z</LastModified><Size>7</Size><ETag>"etag"</ETag></Contents></ListBucketResult>`, cursor, url.QueryEscape(prefix), url.QueryEscape(key))
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	for i := 0; i < 2; i++ {
		inputCursor := ""
		if i == 1 {
			inputCursor = cursor
		}
		page, err := client.ListObjects(context.Background(), "bucket", prefix, inputCursor, 2)
		if err != nil {
			t.Fatal(err)
		}
		if len(page.Entries) != 2 || page.Entries[0].Key != prefix || !page.Entries[0].IsPrefix || page.Entries[1].Key != key || page.NextCursor != cursor {
			t.Fatalf("page changed keys or cursor: %#v", page)
		}
		if requests.Load() != int32(i+1) {
			t.Fatal("pagination prefetched another page")
		}
	}
}

func TestListObjectsCancellationStopsBackgroundCoreRequest(t *testing.T) {
	started := make(chan struct{})
	ended := make(chan struct{})
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if requests.Add(1) == 1 {
			close(started)
		}
		<-r.Context().Done()
		close(ended)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() { _, err := client.ListObjects(ctx, "bucket", "", "", 10); result <- err }()
	select {
	case <-started:
	case <-time.After(3 * time.Second):
		t.Fatal("page request did not start")
	}
	cancel()
	select {
	case err := <-result:
		assertAPIError(t, err, 499, "Canceled")
	case <-time.After(2 * time.Second):
		t.Fatal("Core ignored cancellation")
	}
	select {
	case <-ended:
	case <-time.After(time.Second):
		t.Fatal("upstream request remained active")
	}
	if requests.Load() != 1 {
		t.Fatal("canceled page retried in the background")
	}
}

func TestConcurrentPaginationDoesNotShareRequestContext(t *testing.T) {
	firstStarted, secondStarted := make(chan struct{}), make(chan struct{})
	releaseSecond := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Query().Get("prefix") == "first/" {
			close(firstStarted)
			<-r.Context().Done()
			return
		}
		close(secondStarted)
		select {
		case <-releaseSecond:
			fmt.Fprint(w, `<ListBucketResult><IsTruncated>false</IsTruncated></ListBucketResult>`)
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	firstCtx, cancelFirst := context.WithCancel(context.Background())
	defer cancelFirst()
	secondCtx, cancelSecond := context.WithCancel(context.Background())
	defer cancelSecond()
	firstResult, secondResult := make(chan error, 1), make(chan error, 1)
	go func() { _, err := client.ListObjects(firstCtx, "bucket", "first/", "", 10); firstResult <- err }()
	go func() { _, err := client.ListObjects(secondCtx, "bucket", "second/", "", 10); secondResult <- err }()
	for _, started := range []chan struct{}{firstStarted, secondStarted} {
		select {
		case <-started:
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent page request did not start")
		}
	}
	cancelFirst()
	select {
	case err := <-firstResult:
		assertAPIError(t, err, 499, "Canceled")
	case <-time.After(time.Second):
		t.Fatal("first page did not cancel")
	}
	select {
	case <-secondResult:
		t.Fatal("canceling one page ended the other page")
	default:
	}
	close(releaseSecond)
	select {
	case err := <-secondResult:
		if err != nil {
			t.Fatal("independent page was canceled")
		}
	case <-time.After(time.Second):
		t.Fatal("independent page did not complete")
	}
}

func TestMetadataDeadlineStopsStalledXMLAfterHeaders(t *testing.T) {
	for _, operation := range []string{"buckets", "objects"} {
		t.Run(operation, func(t *testing.T) {
			ended := make(chan struct{})
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				w.Header().Set("Content-Type", "application/xml")
				fmt.Fprint(w, `<ListBucketResult>`)
				w.(http.Flusher).Flush()
				<-r.Context().Done()
				close(ended)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			if client.metadataTimeout != 15*time.Second {
				t.Fatal("metadata deadline is not bounded to 15 seconds by default")
			}
			client.metadataTimeout = 50 * time.Millisecond
			result := make(chan error, 1)
			go func() {
				var err error
				if operation == "buckets" {
					_, err = client.ListBuckets(context.Background())
				} else {
					_, err = client.ListObjects(context.Background(), "bucket", "", "", 10)
				}
				result <- err
			}()
			select {
			case err := <-result:
				assertAPIError(t, err, 504, "Timeout")
			case <-time.After(time.Second):
				t.Fatal("metadata XML body did not respect its deadline")
			}
			select {
			case <-ended:
			case <-time.After(time.Second):
				t.Fatal("stalled metadata request remained active")
			}
		})
	}
}

func TestOpenObjectMetadataDeadlineDoesNotExpireReturnedBody(t *testing.T) {
	releaseBody := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		w.Header().Set("Content-Length", "7")
		w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
		if r.Method == http.MethodHead {
			return
		}
		fmt.Fprint(w, "pay")
		w.(http.Flusher).Flush()
		select {
		case <-releaseBody:
			fmt.Fprint(w, "load")
		case <-r.Context().Done():
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	client.metadataTimeout = 50 * time.Millisecond
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	object, err := client.OpenObject(ctx, "bucket", "object")
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	<-time.After(2 * client.metadataTimeout)
	close(releaseBody)
	data, err := io.ReadAll(object.Body)
	if err != nil || string(data) != "payload" {
		t.Fatal("metadata deadline canceled the streaming download")
	}
}

func TestOpenObjectHeadHasOverallMetadataDeadline(t *testing.T) {
	var gets atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.Method == http.MethodGet {
			gets.Add(1)
			return
		}
		<-r.Context().Done()
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	client.metadataTimeout = 50 * time.Millisecond
	result := make(chan error, 1)
	go func() { _, err := client.OpenObject(context.Background(), "bucket", "object"); result <- err }()
	select {
	case err := <-result:
		assertAPIError(t, err, 504, "Timeout")
	case <-time.After(time.Second):
		t.Fatal("HEAD did not respect metadata deadline")
	}
	if gets.Load() != 0 {
		t.Fatal("download opened after HEAD failed")
	}
}

func TestOpenObjectPreservesKeyAndOpensBeforeReturning(t *testing.T) {
	key := "folder//../中文 空格+?#%2F.txt"
	var head, get atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Path != "/bucket/"+key {
			t.Error("object key was cleaned or decoded twice")
		}
		w.Header().Set("Content-Length", "7")
		w.Header().Set("Content-Type", "text/plain")
		w.Header().Set("ETag", `"example-etag"`)
		w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
		if r.Method == http.MethodHead {
			head.Add(1)
			return
		}
		if r.Method != http.MethodGet || head.Load() != 1 {
			t.Error("download did not stat first")
		}
		get.Add(1)
		fmt.Fprint(w, "payload")
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	object, err := client.OpenObject(context.Background(), "bucket", key)
	if err != nil {
		t.Fatal(err)
	}
	defer object.Body.Close()
	if get.Load() != 1 || object.Size != 7 || object.ContentType != "text/plain" || object.ETag != "example-etag" {
		t.Fatal("object was not opened with safe metadata")
	}
	data, err := io.ReadAll(object.Body)
	if err != nil || string(data) != "payload" {
		t.Fatal("download data mismatch")
	}
}

func TestOpenObjectReportsHeadAndGetFailuresBeforeBody(t *testing.T) {
	for _, test := range []struct {
		name                              string
		headStatus, getStatus, wantStatus int
		code                              string
		wantGet                           int32
	}{
		{"missing", 404, 200, 404, "NotFound", 0},
		{"head-denied", 403, 200, 403, "AccessDenied", 0},
		{"get-denied", 200, 403, 403, "AccessDenied", 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			var gets atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.Method == http.MethodHead {
					w.Header().Set("Content-Length", "7")
					w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
					w.Header().Set("ETag", `"example-etag"`)
					w.WriteHeader(test.headStatus)
					return
				}
				gets.Add(1)
				w.Header().Set("Content-Type", "application/xml")
				w.WriteHeader(test.getStatus)
				fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>secret-marker upstream credential</Message></Error>`)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			object, err := client.OpenObject(context.Background(), "bucket", "object")
			assertAPIError(t, err, test.wantStatus, test.code)
			if object.Body != nil || gets.Load() != test.wantGet {
				t.Fatal("failed download returned a body or skipped HEAD")
			}
		})
	}
}

func TestAccountMapsOnlyVisibleBucketFields(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/otterio/admin/v3/accountinfo" || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
			t.Error("account request did not use signed admin API")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"AccountName":"secret-marker-account","Policy":{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::secret-marker-policy/*"]}]},"Buckets":[{"name":"visible","size":17,"access":{"read":true,"write":false}},{"name":"hidden","size":29,"access":{"read":false,"write":false}}]}`)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	account, err := client.AccountInfo(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	data, err := json.Marshal(account)
	if err != nil || len(account.Buckets) != 1 || account.Buckets[0].Name != "visible" || account.Buckets[0].Size != 17 || !account.Buckets[0].Read || account.Buckets[0].Write || strings.Contains(string(data), "secret-marker") {
		t.Fatal("account output included unauthorized buckets or identity/policy fields")
	}
}

func TestAccountRejectsRedirectAndSanitizesAdminErrors(t *testing.T) {
	var forwarded atomic.Int32
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer target.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL+"/secret-marker", http.StatusTemporaryRedirect)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	_, err := client.AccountInfo(context.Background())
	assertAPIError(t, err, 502, "AdminRedirectDisabled")
	if forwarded.Load() != 0 {
		t.Fatal("management request reached redirect target")
	}
	for _, payload := range []string{`{"Code":"AccessDenied","Message":"secret-marker"}`, `{"Code":"secret-marker","Message":"secret-marker"}`, `{"malformed"`} {
		fixture := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusForbidden)
			fmt.Fprint(w, payload)
		}))
		client := testClient(t, Config{S3URL: fixture.URL})
		_, err := client.AccountInfo(context.Background())
		if err == nil || strings.Contains(err.Error(), "secret-marker") {
			t.Fatal("raw management error leaked")
		}
		fixture.Close()
	}
}

func newTLSFixture(t *testing.T, handler http.Handler) (*httptest.Server, *x509.CertPool) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{SerialNumber: big.NewInt(time.Now().UnixNano()), Subject: pkix.Name{CommonName: "localhost"}, NotBefore: time.Now().Add(-time.Minute), NotAfter: time.Now().Add(time.Hour), DNSNames: []string{"localhost"}, IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")}, KeyUsage: x509.KeyUsageDigitalSignature, ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth}, BasicConstraintsValid: true}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	certificate, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	server := httptest.NewUnstartedServer(handler)
	server.TLS = &tls.Config{Certificates: []tls.Certificate{{Certificate: [][]byte{der}, PrivateKey: key}}, MinVersion: tls.VersionTLS12}
	server.StartTLS()
	t.Cleanup(server.Close)
	roots := x509.NewCertPool()
	roots.AddCert(certificate)
	return server, roots
}

func TestS3AndAdminTrustPoolsAreIndependent(t *testing.T) {
	s3, s3Roots := newTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListAllMyBucketsResult><Buckets><Bucket><Name>bucket</Name><CreationDate>2026-10-08T00:00:00Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`)
	}))
	admin, adminRoots := newTLSFixture(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"Buckets":[{"name":"bucket","size":1,"access":{"read":true}}]}`)
	}))
	good := testClient(t, Config{S3URL: s3.URL, AdminURL: admin.URL, RootCAs: s3Roots, AdminRootCAs: adminRoots})
	if _, err := good.ListBuckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := good.AccountInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	wrongAdmin := testClient(t, Config{S3URL: s3.URL, AdminURL: admin.URL, RootCAs: s3Roots, AdminRootCAs: s3Roots})
	if _, err := wrongAdmin.ListBuckets(context.Background()); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if _, err := wrongAdmin.AccountInfo(ctx); err == nil {
		t.Fatal("S3 CA trusted independent admin certificate")
	}
	wrongS3 := testClient(t, Config{S3URL: s3.URL, AdminURL: admin.URL, RootCAs: adminRoots, AdminRootCAs: adminRoots})
	if _, err := wrongS3.AccountInfo(context.Background()); err != nil {
		t.Fatal(err)
	}
	if _, err := wrongS3.ListBuckets(context.Background()); err == nil {
		t.Fatal("admin CA trusted independent S3 certificate")
	}
}

func TestInvalidConfigurationAndRequestsDoNotLeakTargets(t *testing.T) {
	for _, cfg := range []Config{{S3URL: "http://secret-marker@localhost"}, {S3URL: "http://localhost/prefix"}, {S3URL: "http://localhost", AdminURL: "http://secret-marker@localhost"}, {S3URL: "http://localhost", API: "secret-marker"}, {S3URL: "http://localhost", Path: "secret-marker"}} {
		_, err := New(cfg)
		assertAPIError(t, err, 400, "InvalidConfiguration")
	}
	client := testClient(t, Config{S3URL: "http://127.0.0.1:1"})
	_, err := client.ListObjects(context.Background(), "bucket", "", "", 1001)
	assertAPIError(t, err, 400, "InvalidRequest")
	client.Close()
	_, err = client.ListBuckets(context.Background())
	assertAPIError(t, err, 503, "ClientClosed")
}

func assertAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var apiError *consoleapi.Error
	if !errors.As(err, &apiError) || apiError.Status != status || apiError.Code != code || strings.Contains(apiError.Message, "secret-marker") {
		t.Fatalf("expected safe API error %d/%s, got %v", status, code, err)
	}
}
