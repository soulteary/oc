// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

// This fixture is overlaid into internal/storageclient only by
// buildscripts/test-conditional-object-mutations.py. It never accepts a running
// storage endpoint: both backends are fresh local processes with temporary data.
package storageclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/http/httputil"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
)

type liveConditionalFixture struct {
	endpoint, access, secret string
	sdk                      *minio.Client
}

func (f liveConditionalFixture) safeError(err error) string {
	if err == nil {
		return ""
	}
	return strings.NewReplacer(f.access, "[fixture-access]", f.secret, "[fixture-secret]").Replace(err.Error())
}

func startLiveConditionalFixture(t *testing.T, backend string) liveConditionalFixture {
	t.Helper()
	binary := os.Getenv("OC_CONDITIONAL_SERVER_BINARY")
	if binary == "" {
		t.Fatal("OC_CONDITIONAL_SERVER_BINARY must name the disposable integration server binary")
	}
	root := t.TempDir()
	if err := os.Mkdir(filepath.Join(root, "certs"), 0700); err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	var random [24]byte
	if _, err := rand.Read(random[:]); err != nil {
		t.Fatal(err)
	}
	access, secret := "cond-"+hex.EncodeToString(random[:6]), hex.EncodeToString(random[:])
	redact := strings.NewReplacer(access, "[fixture-access]", secret, "[fixture-secret]")
	args := []string{"server", "--address", address, "--certs-dir", filepath.Join(root, "certs")}
	if backend == "FS" {
		args = append(args, filepath.Join(root, "data"))
	} else {
		for i := 0; i < 4; i++ {
			args = append(args, filepath.Join(root, fmt.Sprintf("disk%d", i)))
		}
	}
	cmd := exec.Command(binary, args...)
	cmd.Dir = root
	for _, item := range os.Environ() {
		key, _, _ := strings.Cut(item, "=")
		if strings.HasPrefix(key, "OTTERIO_") || strings.HasPrefix(key, "MINIO_") || strings.HasPrefix(key, "OC_") || strings.HasPrefix(key, "MC_") || key == "GOMAXPROCS" || strings.EqualFold(key, "NO_PROXY") {
			continue
		}
		cmd.Env = append(cmd.Env, item)
	}
	cmd.Env = append(cmd.Env, "OTTERIO_ROOT_USER="+access, "OTTERIO_ROOT_PASSWORD="+secret, "OTTERIO_BROWSER=off", "NO_PROXY=*", "no_proxy=*", "GOMAXPROCS=2")
	logPath := filepath.Join(root, "server.log")
	log, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	cmd.Stdout, cmd.Stderr = log, log
	if err := cmd.Start(); err != nil {
		log.Close()
		t.Fatal(err)
	}
	done := make(chan error, 1)
	var exited atomic.Bool
	go func() {
		err := cmd.Wait()
		exited.Store(true)
		done <- err
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Signal(os.Interrupt)
		select {
		case <-done:
		case <-time.After(3 * time.Second):
			_ = cmd.Process.Kill()
			<-done
		}
		_ = log.Close()
		if t.Failed() {
			content, _ := os.ReadFile(logPath)
			redacted := redact.Replace(string(content))
			if len(redacted) > 12000 {
				redacted = redacted[len(redacted)-12000:]
			}
			t.Logf("%s server log:\n%s", backend, redacted)
		}
	})
	endpoint := "http://" + address
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	health := &http.Client{Transport: transport, Timeout: time.Second}
	deadline := time.Now().Add(90 * time.Second)
	for {
		resp, healthErr := health.Get(endpoint + "/otterio/health/ready")
		if healthErr == nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			_ = resp.Body.Close()
			if resp.StatusCode == http.StatusOK {
				break
			}
		}
		if exited.Load() {
			t.Fatal("disposable server exited before readiness")
		}
		if time.Now().After(deadline) {
			t.Fatal("disposable server readiness timed out")
		}
		time.Sleep(100 * time.Millisecond)
	}
	sdk, err := minio.New(address, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: false, BucketLookup: minio.BucketLookupPath, Region: "us-east-1", Transport: transport, MaxRetries: 1})
	if err != nil {
		t.Fatal(err)
	}
	// The HTTP router can answer its readiness endpoint while the erasure
	// backend is still formatting local disks. Wait for an authenticated,
	// read-only S3 operation before creating this fixture's bucket.
	for {
		probeCtx, cancelProbe := context.WithTimeout(context.Background(), time.Second)
		_, probeErr := sdk.ListBuckets(probeCtx)
		cancelProbe()
		if probeErr == nil {
			break
		}
		if exited.Load() || time.Now().After(deadline) {
			t.Fatalf("disposable S3 server did not become ready: %s", redact.Replace(probeErr.Error()))
		}
		time.Sleep(100 * time.Millisecond)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	if err := sdk.MakeBucket(ctx, "conditional-rename", minio.MakeBucketOptions{Region: "us-east-1"}); err != nil {
		t.Fatal(redact.Replace(err.Error()))
	}
	return liveConditionalFixture{endpoint: endpoint, access: access, secret: secret, sdk: sdk}
}

type liveConditionalRequest struct {
	Method        string `json:"method"`
	Path          string `json:"path"`
	Operation     string `json:"operation"`
	IfMatch       string `json:"ifMatch,omitempty"`
	IfNoneMatch   string `json:"ifNoneMatch,omitempty"`
	SourceIfMatch string `json:"sourceIfMatch,omitempty"`
	SignedHeaders string `json:"signedHeaders"`
	Status        int    `json:"status"`
}

type liveConditionalTrace struct {
	mu       sync.Mutex
	requests []liveConditionalRequest
}

type liveConditionalTraceKey struct{}

func liveConditionalOperation(r *http.Request) string {
	q := r.URL.Query()
	switch {
	case q.Has("location"):
		return "location"
	case r.Method == http.MethodHead:
		return "head"
	case r.Header.Get("X-Amz-Copy-Source") != "":
		return "copy"
	case q.Has("uploads"):
		return "initiate"
	case q.Has("partNumber"):
		return "part"
	case r.Method == http.MethodPost && q.Has("uploadId"):
		return "complete"
	case r.Method == http.MethodDelete && q.Has("uploadId"):
		return "abort"
	case r.Method == http.MethodDelete:
		return "delete"
	case r.Method == http.MethodPut:
		return "put"
	default:
		return "other"
	}
}

func (f liveConditionalFixture) tracedClient(t *testing.T, before func(*http.Request) error) (*Client, *liveConditionalTrace) {
	t.Helper()
	trace := &liveConditionalTrace{}
	target, err := url.Parse(f.endpoint)
	if err != nil {
		t.Fatal(err)
	}
	proxy := httputil.NewSingleHostReverseProxy(target)
	transport := &http.Transport{Proxy: nil}
	proxy.Transport = transport
	t.Cleanup(transport.CloseIdleConnections)
	// Preserve the original Host at the real server: it is part of SigV4. Only
	// the network destination changes; no S3 response or condition is simulated.
	proxy.ModifyResponse = func(resp *http.Response) error {
		index := resp.Request.Context().Value(liveConditionalTraceKey{}).(int)
		trace.mu.Lock()
		trace.requests[index].Status = resp.StatusCode
		trace.mu.Unlock()
		return nil
	}
	proxy.ErrorHandler = func(w http.ResponseWriter, r *http.Request, err error) {
		t.Errorf("integration proxy failed: %s", f.safeError(err))
		w.WriteHeader(http.StatusBadGateway)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		signed := r.URL.Query().Get("X-Amz-SignedHeaders")
		if signed == "" {
			_, rest, _ := strings.Cut(r.Header.Get("Authorization"), "SignedHeaders=")
			signed, _, _ = strings.Cut(rest, ",")
		}
		record := liveConditionalRequest{Method: r.Method, Path: r.URL.Path, Operation: liveConditionalOperation(r), IfMatch: r.Header.Get("If-Match"), IfNoneMatch: r.Header.Get("If-None-Match"), SourceIfMatch: r.Header.Get("X-Amz-Copy-Source-If-Match"), SignedHeaders: signed}
		trace.mu.Lock()
		index := len(trace.requests)
		trace.requests = append(trace.requests, record)
		trace.mu.Unlock()
		if before != nil {
			if err := before(r); err != nil {
				t.Errorf("competing writer failed: %s", f.safeError(err))
				w.WriteHeader(http.StatusBadGateway)
				return
			}
		}
		proxy.ServeHTTP(w, r.WithContext(context.WithValue(r.Context(), liveConditionalTraceKey{}, index)))
	}))
	t.Cleanup(server.Close)
	client, err := New(Config{S3URL: server.URL, AccessKey: f.access, SecretKey: f.secret, Path: "on"})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(client.Close)
	return client, trace
}

func (trace *liveConditionalTrace) snapshot() []liveConditionalRequest {
	trace.mu.Lock()
	defer trace.mu.Unlock()
	return append([]liveConditionalRequest(nil), trace.requests...)
}

func (trace *liveConditionalTrace) verify(t *testing.T, counts map[string]int) []liveConditionalRequest {
	t.Helper()
	requests := trace.snapshot()
	actual := make(map[string]int)
	for _, request := range requests {
		if request.Operation != "location" && request.Operation != "head" {
			actual[request.Operation]++
		}
	}
	if !reflect.DeepEqual(actual, counts) {
		t.Errorf("mutation requests = %#v, want %#v", actual, counts)
	}
	data, _ := json.Marshal(requests)
	t.Logf("actual HTTP trace: %s", data)
	return requests
}

func livePut(ctx context.Context, sdk *minio.Client, key string, data []byte, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	opts.DisableMultipart = true
	return sdk.PutObject(ctx, "conditional-rename", key, bytes.NewReader(data), int64(len(data)), opts)
}

func livePutOrFatal(t *testing.T, fixture liveConditionalFixture, key string, data []byte, opts minio.PutObjectOptions) minio.UploadInfo {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	info, err := livePut(ctx, fixture.sdk, key, data, opts)
	if err != nil {
		t.Fatal(fixture.safeError(err))
	}
	return info
}

func liveAssertBytes(t *testing.T, fixture liveConditionalFixture, key string, expected []byte) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	object, err := fixture.sdk.GetObject(ctx, "conditional-rename", key, minio.GetObjectOptions{})
	if err != nil {
		t.Fatal(fixture.safeError(err))
	}
	defer object.Close()
	actual, err := io.ReadAll(object)
	if err != nil {
		t.Fatal(fixture.safeError(err))
	}
	if !bytes.Equal(actual, expected) {
		t.Fatalf("stored %q content changed: got %d bytes, want %d", key, len(actual), len(expected))
	}
}

func liveAssertMissing(t *testing.T, fixture liveConditionalFixture, key string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	_, err := fixture.sdk.StatObject(ctx, "conditional-rename", key, minio.StatObjectOptions{})
	response := minio.ToErrorResponse(err)
	if response.StatusCode != http.StatusNotFound {
		t.Fatalf("%q should be absent, got %s", key, fixture.safeError(err))
	}
}

func liveAssertAPIError(t *testing.T, err error, status int, code string) {
	t.Helper()
	var api *consoleapi.Error
	if !errors.As(err, &api) || api.Status != status || api.Code != code {
		t.Fatalf("API error = %#v, want %d/%s", err, status, code)
	}
}

func TestLiveConditionalUploadAndRename(t *testing.T) {
	for _, backend := range []string{"FS", "Erasure"} {
		t.Run(backend, func(t *testing.T) {
			fixture := startLiveConditionalFixture(t, backend)
			for _, conflict := range []bool{false, true} {
				name := "large-create"
				if conflict {
					name = "large-create-target-race"
				}
				t.Run(name, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					data := bytes.Repeat([]byte{0x4b}, uploadPartSize+1)
					competitor := []byte("independent target writer")
					var injected atomic.Bool
					client, trace := fixture.tracedClient(t, func(r *http.Request) error {
						if conflict && liveConditionalOperation(r) == "complete" && injected.CompareAndSwap(false, true) {
							_, err := livePut(ctx, fixture.sdk, name, competitor, minio.PutObjectOptions{})
							return err
						}
						return nil
					})
					result, err := client.Upload(ctx, "conditional-rename", name, bytes.NewReader(data), int64(len(data)), consoleapi.UploadOptions{}, nil)
					counts := map[string]int{"initiate": 1, "part": 2, "complete": 1}
					if conflict {
						liveAssertAPIError(t, err, http.StatusConflict, "object_exists")
						if !injected.Load() {
							t.Fatal("target race was not injected")
						}
						liveAssertBytes(t, fixture, name, competitor)
						counts["abort"] = 1
					} else {
						if err != nil || result.Size != int64(len(data)) || !strings.HasSuffix(result.ETag, "-2") {
							t.Fatalf("large upload = %#v, error %v", result, err)
						}
						liveAssertBytes(t, fixture, name, data)
					}
					for _, request := range trace.verify(t, counts) {
						if request.Operation == "complete" {
							if request.IfNoneMatch != "*" || !strings.Contains(request.SignedHeaders, "if-none-match") {
								t.Error("completion condition missing or unsigned")
							}
						} else if request.IfMatch != "" || request.IfNoneMatch != "" {
							t.Errorf("condition on non-committing operation: %#v", request)
						}
					}
				})
			}
			for _, scenario := range []string{"rename-metadata-tags", "rename-multipart-source", "rename-target-race", "rename-source-before-copy", "rename-source-before-delete"} {
				t.Run(scenario, func(t *testing.T) {
					ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
					defer cancel()
					key, target := scenario+"/source +中文%#?.txt", scenario+"/target +中文%#?.txt"
					original, replacement := []byte("original source body"), []byte("new body from concurrent source writer")
					targetReplacement := []byte("new body from concurrent target writer")
					metadata := minio.PutObjectOptions{ContentType: "text/plain", UserMetadata: map[string]string{"Owner": "live-integration", "Purpose": "conditional-rename"}, UserTags: map[string]string{"team": "integration", "preserve": "yes"}}
					var etag string
					if scenario == "rename-multipart-source" {
						original = bytes.Repeat([]byte{0x59}, uploadPartSize+1)
						uploadClient, _ := fixture.tracedClient(t, nil)
						info, err := uploadClient.Upload(ctx, "conditional-rename", key, bytes.NewReader(original), int64(len(original)), consoleapi.UploadOptions{}, nil)
						if err != nil {
							t.Fatal(err)
						}
						etag = info.ETag
						if !strings.HasSuffix(etag, "-2") {
							t.Fatalf("expected multipart source, got ETag %q", etag)
						}
					} else {
						etag = livePutOrFatal(t, fixture, key, original, metadata).ETag
					}
					var injected atomic.Bool
					client, trace := fixture.tracedClient(t, func(r *http.Request) error {
						op := liveConditionalOperation(r)
						if scenario == "rename-target-race" && op == "copy" && injected.CompareAndSwap(false, true) {
							_, err := livePut(ctx, fixture.sdk, target, targetReplacement, minio.PutObjectOptions{})
							return err
						}
						if ((scenario == "rename-source-before-copy" && op == "copy") || (scenario == "rename-source-before-delete" && op == "delete")) && injected.CompareAndSwap(false, true) {
							_, err := livePut(ctx, fixture.sdk, key, replacement, minio.PutObjectOptions{})
							return err
						}
						return nil
					})
					err := client.RenameObject(ctx, "conditional-rename", key, target, etag)
					counts := map[string]int{"copy": 1}
					switch scenario {
					case "rename-target-race":
						liveAssertAPIError(t, err, http.StatusConflict, "rename_conflict")
						liveAssertBytes(t, fixture, key, original)
						liveAssertBytes(t, fixture, target, targetReplacement)
					case "rename-source-before-copy":
						liveAssertAPIError(t, err, http.StatusConflict, "rename_conflict")
						liveAssertBytes(t, fixture, key, replacement)
						liveAssertMissing(t, fixture, target)
					case "rename-source-before-delete":
						liveAssertAPIError(t, err, http.StatusConflict, "rename_partial")
						liveAssertBytes(t, fixture, key, replacement)
						liveAssertBytes(t, fixture, target, original)
						counts["delete"] = 1
					default:
						if err != nil {
							t.Fatal(err)
						}
						liveAssertMissing(t, fixture, key)
						liveAssertBytes(t, fixture, target, original)
						counts["delete"] = 1
					}
					if strings.Contains(scenario, "race") || strings.Contains(scenario, "before-") {
						if !injected.Load() {
							t.Fatal("requested concurrency window was not exercised")
						}
					}
					if scenario == "rename-metadata-tags" {
						stat, err := fixture.sdk.StatObject(ctx, "conditional-rename", target, minio.StatObjectOptions{})
						if err != nil {
							t.Fatal(fixture.safeError(err))
						}
						if stat.ContentType != metadata.ContentType || stat.Metadata.Get("X-Amz-Meta-Owner") != "live-integration" || stat.Metadata.Get("X-Amz-Meta-Purpose") != "conditional-rename" {
							t.Fatalf("copied metadata changed: %#v", stat)
						}
						tags, err := fixture.sdk.GetObjectTagging(ctx, "conditional-rename", target, minio.GetObjectTaggingOptions{})
						if err != nil || !reflect.DeepEqual(tags.ToMap(), metadata.UserTags) {
							t.Fatalf("copied tags changed: %#v, error %s", tags, fixture.safeError(err))
						}
					}
					for _, request := range trace.verify(t, counts) {
						switch request.Operation {
						case "copy":
							if request.IfNoneMatch != "*" || request.SourceIfMatch != "\""+etag+"\"" || !strings.Contains(request.SignedHeaders, "if-none-match") || !strings.Contains(request.SignedHeaders, "x-amz-copy-source-if-match") {
								t.Errorf("copy conditions missing or unsigned: %#v", request)
							}
						case "delete":
							if request.IfMatch != "\""+etag+"\"" || !strings.Contains(request.SignedHeaders, "if-match") {
								t.Errorf("source delete condition missing or unsigned: %#v", request)
							}
							want := http.StatusNoContent
							if scenario == "rename-source-before-delete" {
								want = http.StatusPreconditionFailed
							}
							if request.Status != want {
								t.Errorf("source DELETE HTTP status = %d, want %d", request.Status, want)
							}
						}
					}
				})
			}
		})
	}
}
