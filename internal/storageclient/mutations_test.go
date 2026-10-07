// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/xml"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestUploadSingleRequiresPerRequestConditionalCapability(t *testing.T) {
	for _, test := range []struct {
		name       string
		headStatus int
		capability string
		overwrite  bool
		wantStatus int
		wantCode   string
	}{
		{"new-supported", 404, "v1", false, 0, ""},
		{"existing", 200, "v1", false, 409, "object_exists"},
		{"unadvertised", 404, "", false, 501, "conditional_writes_unsupported"},
		{"unknown-capability", 404, "v2", false, 501, "conditional_writes_unsupported"},
		{"denied", 403, "v1", false, 403, "AccessDenied"},
		{"explicit-overwrite", 403, "", true, 0, ""},
	} {
		t.Run(test.name, func(t *testing.T) {
			key := "folder//../中文 空格+?#%2F.txt"
			var heads, puts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				if r.URL.Path != "/bucket/"+key {
					t.Error("mutation changed the object key")
				}
				if r.Method == http.MethodHead {
					heads.Add(1)
					w.Header().Set("X-Otterio-Conditional-Writes", test.capability)
					w.Header().Set("Last-Modified", "Thu, 08 Oct 2026 00:00:00 GMT")
					w.Header().Set("ETag", `"existing"`)
					w.Header().Set("Content-Length", "7")
					w.WriteHeader(test.headStatus)
					return
				}
				if r.Method != http.MethodPut {
					t.Error("unexpected mutation method")
					return
				}
				puts.Add(1)
				if (!test.overwrite && r.Header.Get("If-None-Match") != "*") || (test.overwrite && r.Header.Get("If-None-Match") != "") {
					t.Error("overwrite condition changed")
				}
				data, err := io.ReadAll(r.Body)
				if err != nil || string(data) != "payload" || r.ContentLength != 7 {
					t.Error("single PUT payload changed")
				}
				checkPayloadHashes(t, r, data)
				w.Header().Set("ETag", `"uploaded"`)
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			var acknowledged int64
			result, err := client.Upload(context.Background(), "bucket", key, strings.NewReader("payload"), 7, consoleapi.UploadOptions{Overwrite: test.overwrite}, func(n int64) { acknowledged = n })
			if test.wantStatus != 0 {
				assertAPIError(t, err, test.wantStatus, test.wantCode)
				if puts.Load() != 0 || acknowledged != 0 {
					t.Fatal("rejected upload dispatched PUT or acknowledged bytes")
				}
			} else if err != nil || result.Size != 7 || result.ETag != "uploaded" || acknowledged != 7 || puts.Load() != 1 {
				t.Fatal("upload was not acknowledged exactly once")
			}
			if test.overwrite && heads.Load() != 0 {
				t.Fatal("explicit overwrite performed an existence HEAD")
			}
		})
	}
}

func checkPayloadHashes(t *testing.T, r *http.Request, data []byte) {
	t.Helper()
	md5Sum, shaSum := md5.Sum(data), sha256.Sum256(data)
	if r.Header.Get("Content-MD5") != base64.StdEncoding.EncodeToString(md5Sum[:]) || r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(shaSum[:]) {
		t.Error("payload was not signed with its real hashes")
	}
}

type generatedUploadReader struct {
	remaining int64
	read      int64
	maxRead   int
}

func (r *generatedUploadReader) Read(p []byte) (int, error) {
	if len(p) > r.maxRead {
		r.maxRead = len(p)
	}
	if r.remaining == 0 {
		return 0, io.EOF
	}
	n := len(p)
	if int64(n) > r.remaining {
		n = int(r.remaining)
	}
	for i := 0; i < n; i++ {
		p[i] = byte((r.read + int64(i)) % 251)
	}
	r.read += int64(n)
	r.remaining -= int64(n)
	return n, nil
}

func TestUploadMultipartUsesSequentialOwnedPartsAndOpaqueKeys(t *testing.T) {
	const size = 2*uploadPartSize + 3
	key, uploadID := "folder//../中文 空格+?#%2F", "owned+/?=%id"
	reader := &generatedUploadReader{remaining: size}
	var initiates, parts, completes, aborts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Path != "/bucket/"+key {
			t.Error("multipart key was normalized")
		}
		query := r.URL.Query()
		switch {
		case r.Method == http.MethodHead:
			w.Header().Set("X-Otterio-Conditional-Writes", "v1")
			w.WriteHeader(404)
		case r.Method == http.MethodPost && query.Has("uploads"):
			initiates.Add(1)
			fmt.Fprintf(w, `<InitiateMultipartUploadResult><UploadId>%s</UploadId></InitiateMultipartUploadResult>`, uploadID)
		case r.Method == http.MethodPut:
			if query.Get("uploadId") != uploadID {
				t.Error("part used another upload ID")
			}
			n := parts.Add(1)
			if query.Get("partNumber") != strconv.Itoa(int(n)) {
				t.Error("parts were reordered or retried")
			}
			wantSize := int64(uploadPartSize)
			if n == 3 {
				wantSize = 3
			}
			md5Hash, shaHash := md5.New(), sha256.New()
			copied, err := io.Copy(io.MultiWriter(md5Hash, shaHash), r.Body)
			if err != nil || copied != wantSize || r.ContentLength != wantSize {
				t.Error("part size changed")
			}
			if r.Header.Get("Content-MD5") != base64.StdEncoding.EncodeToString(md5Hash.Sum(nil)) || r.Header.Get("X-Amz-Content-Sha256") != hex.EncodeToString(shaHash.Sum(nil)) {
				t.Error("part digest does not match transmitted payload")
			}
			w.Header().Set("ETag", fmt.Sprintf(`"part-%d"`, n))
		case r.Method == http.MethodPost && query.Get("uploadId") == uploadID:
			completes.Add(1)
			if r.Header.Get("If-None-Match") != "*" {
				t.Error("completion lost the atomic create condition")
			}
			var document struct {
				Parts []struct {
					Number int    `xml:"PartNumber"`
					ETag   string `xml:"ETag"`
				} `xml:"Part"`
			}
			if xml.NewDecoder(r.Body).Decode(&document) != nil || len(document.Parts) != 3 {
				t.Error("completion did not contain all parts")
			}
			for i, part := range document.Parts {
				if part.Number != i+1 || part.ETag != fmt.Sprintf("part-%d", i+1) {
					t.Error("completion used the wrong acknowledged part")
				}
			}
			fmt.Fprintf(w, `<CompleteMultipartUploadResult><Bucket>bucket</Bucket><Key>%s</Key><ETag>complete-etag</ETag></CompleteMultipartUploadResult>`, key)
		case r.Method == http.MethodDelete:
			aborts.Add(1)
			w.WriteHeader(204)
		default:
			t.Error("unexpected multipart request")
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	var progress []int64
	result, err := client.Upload(context.Background(), "bucket", key, reader, size, consoleapi.UploadOptions{}, func(n int64) { progress = append(progress, n) })
	if err != nil || result.Size != size || result.ETag != "complete-etag" {
		t.Fatalf("multipart result failed: %v", err)
	}
	if initiates.Load() != 1 || parts.Load() != 3 || completes.Load() != 1 || aborts.Load() != 0 {
		t.Fatal("multipart requests were retried, skipped, or unnecessarily aborted")
	}
	if reader.maxRead > uploadPartSize || reader.read != size || len(progress) != 3 || progress[0] != uploadPartSize || progress[1] != 2*uploadPartSize || progress[2] != size {
		t.Fatal("upload read or acknowledged progress was not bounded and sequential")
	}
}

func TestUploadCancellationUsesIndependentOwnedCleanupAndReportsFailure(t *testing.T) {
	for _, cleanupStatus := range []int{204, 403} {
		t.Run(strconv.Itoa(cleanupStatus), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			var parts, aborts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				query := r.URL.Query()
				switch r.Method {
				case http.MethodPost:
					if !query.Has("uploads") {
						t.Error("canceled upload reached completion")
					}
					fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned-session</UploadId></InitiateMultipartUploadResult>`)
				case http.MethodPut:
					parts.Add(1)
					io.Copy(io.Discard, r.Body)
					cancel()
					<-r.Context().Done()
				case http.MethodDelete:
					aborts.Add(1)
					if query.Get("uploadId") != "owned-session" || r.Context().Err() != nil || !strings.HasPrefix(r.Header.Get("Authorization"), "AWS4-HMAC-SHA256 ") {
						t.Error("cleanup lost its independent context, owned ID or startup identity")
					}
					w.WriteHeader(cleanupStatus)
					if cleanupStatus != 204 {
						fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>secret-marker cleanup denial</Message></Error>`)
					}
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.Upload(ctx, "bucket", "object", &generatedUploadReader{remaining: uploadPartSize + 1}, uploadPartSize+1, consoleapi.UploadOptions{Overwrite: true}, nil)
			if cleanupStatus == 204 {
				assertAPIError(t, err, 499, "Canceled")
			} else {
				assertAPIError(t, err, 502, "cleanup_failed")
			}
			if parts.Load() != 1 || aborts.Load() != 1 {
				t.Fatal("cancellation did not abort exactly its owned session")
			}
		})
	}
}

func TestMutationLostAcknowledgementIsUnknownWithoutRetryOrRedirect(t *testing.T) {
	for _, method := range []string{http.MethodPut, http.MethodDelete, "complete"} {
		t.Run(method, func(t *testing.T) {
			var writes, aborts, forwarded atomic.Int32
			redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
			defer redirect.Close()
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				q := r.URL.Query()
				if q.Has("uploads") {
					fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned</UploadId></InitiateMultipartUploadResult>`)
					return
				}
				if q.Has("partNumber") {
					io.Copy(io.Discard, r.Body)
					w.Header().Set("ETag", `"part"`)
					return
				}
				if r.Method == http.MethodDelete && q.Get("uploadId") == "owned" {
					aborts.Add(1)
					w.WriteHeader(204)
					return
				}
				writes.Add(1)
				io.Copy(io.Discard, r.Body)
				if method == http.MethodDelete {
					http.Redirect(w, r, redirect.URL, http.StatusTemporaryRedirect)
					return
				}
				connection, _, err := w.(http.Hijacker).Hijack()
				if err != nil {
					t.Error("could not simulate lost acknowledgement")
					return
				}
				connection.Close()
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			var err error
			switch method {
			case http.MethodDelete:
				err = client.DeleteObject(context.Background(), "bucket", "object")
			case http.MethodPut:
				_, err = client.Upload(context.Background(), "bucket", "object", strings.NewReader("payload"), 7, consoleapi.UploadOptions{Overwrite: true}, nil)
			default:
				_, err = client.Upload(context.Background(), "bucket", "object", &generatedUploadReader{remaining: uploadPartSize + 1}, uploadPartSize+1, consoleapi.UploadOptions{Overwrite: true}, nil)
			}
			if method == http.MethodDelete {
				assertAPIError(t, err, 502, "UpstreamError")
			} else {
				assertAPIError(t, err, 502, "outcome_unknown")
			}
			if writes.Load() != 1 || forwarded.Load() != 0 {
				t.Fatal("write retried or forwarded credentials to redirect target")
			}
			if method == "complete" && aborts.Load() != 1 {
				t.Fatal("uncertain completion did not attempt owned cleanup")
			}
		})
	}
}

func TestUploadValidationAndLengthMismatchNeverCommits(t *testing.T) {
	var requests atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { requests.Add(1) }))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	for _, test := range []struct {
		key    string
		body   io.Reader
		size   int64
		status int
		code   string
	}{
		{"", strings.NewReader("x"), 1, 400, "InvalidRequest"},
		{"nul\x00key", strings.NewReader("x"), 1, 400, "InvalidRequest"},
		{string([]byte{0xff}), strings.NewReader("x"), 1, 400, "InvalidRequest"},
		{strings.Repeat("x", 1025), strings.NewReader("x"), 1, 400, "InvalidRequest"},
		{"object", strings.NewReader("x"), -1, 400, "InvalidRequest"},
		{"object", nil, 1, 400, "InvalidRequest"},
		{"object", strings.NewReader("x"), maxUploadSize + 1, 413, "upload_too_large"},
		{"object", strings.NewReader("x"), 2, 400, "upload_size_mismatch"},
		{"object", strings.NewReader("xx"), 1, 400, "upload_size_mismatch"},
	} {
		_, err := client.Upload(context.Background(), "bucket", test.key, test.body, test.size, consoleapi.UploadOptions{Overwrite: true}, nil)
		assertAPIError(t, err, test.status, test.code)
	}
	if requests.Load() != 0 {
		t.Fatal("invalid upload dispatched an upstream request")
	}
}

func TestDeletePreservesKeyAndNeverDeletesVersions(t *testing.T) {
	key := "folder//../中文 空格+?#%2F"
	var deletes atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		deletes.Add(1)
		if r.Method != http.MethodDelete || r.URL.Path != "/bucket/"+key || len(r.URL.Query()) != 0 || r.Header.Get("x-amz-bypass-governance-retention") != "" || r.Header.Get("x-minio-force-delete") != "" {
			t.Error("delete changed key, selected a version or bypassed retention")
		}
		w.Header().Set("x-amz-delete-marker", "true")
		w.Header().Set("x-amz-version-id", "new-marker")
		w.WriteHeader(204)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	if err := client.DeleteObject(context.Background(), "bucket", key); err != nil {
		t.Fatal(err)
	}
	if deletes.Load() != 1 {
		t.Fatal("delete was not acknowledged once")
	}
}

func TestScanObjectsUsesFlatBoundedPageAndCancellation(t *testing.T) {
	var lists atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		lists.Add(1)
		q := r.URL.Query()
		if q.Get("delimiter") != "" || q.Get("prefix") != "prefix/" || q.Get("continuation-token") != "opaque+cursor" || q.Get("max-keys") != "2" {
			t.Error("recursive scan changed the flat page contract")
		}
		fmt.Fprint(w, `<ListBucketResult><IsTruncated>true</IsTruncated><NextContinuationToken>next</NextContinuationToken><Contents><Key>prefix/child/deep</Key><LastModified>2026-10-08T00:00:00Z</LastModified><Size>1</Size></Contents></ListBucketResult>`)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	page, err := client.ScanObjects(context.Background(), "bucket", "prefix/", "opaque+cursor", 2)
	if err != nil || len(page.Entries) != 1 || page.Entries[0].Key != "prefix/child/deep" || page.NextCursor != "next" || lists.Load() != 1 {
		t.Fatal("flat scan prefetched, lost cursor or lost descendant")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.ScanObjects(ctx, "bucket", "prefix/", "", 2)
	assertAPIError(t, err, 499, "Canceled")
	if lists.Load() != 1 {
		t.Fatal("canceled scan sent another page")
	}
}

func TestConditionalCapabilityDoesNotCrossConcurrentUploads(t *testing.T) {
	var puts atomic.Int32
	gate := make(chan struct{})
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.Method == http.MethodHead {
			if strings.HasSuffix(r.URL.Path, "/supported") {
				w.Header().Set("X-Otterio-Conditional-Writes", "v1")
				close(gate)
			} else {
				<-gate
			}
			w.WriteHeader(404)
			return
		}
		puts.Add(1)
		io.Copy(io.Discard, r.Body)
		w.Header().Set("ETag", `"uploaded"`)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	results := make(chan error, 2)
	go func() {
		_, err := client.Upload(context.Background(), "bucket", "supported", bytes.NewReader([]byte{1}), 1, consoleapi.UploadOptions{}, nil)
		results <- err
	}()
	go func() {
		_, err := client.Upload(context.Background(), "bucket", "unsupported", bytes.NewReader([]byte{1}), 1, consoleapi.UploadOptions{}, nil)
		results <- err
	}()
	var succeeded, unsupported int
	for i := 0; i < 2; i++ {
		select {
		case err := <-results:
			if err == nil {
				succeeded++
			} else {
				assertAPIError(t, err, 501, "conditional_writes_unsupported")
				unsupported++
			}
		case <-time.After(3 * time.Second):
			t.Fatal("concurrent conditional HEAD did not finish")
		}
	}
	if succeeded != 1 || unsupported != 1 || puts.Load() != 1 {
		t.Fatal("one request's capability authorized another upload")
	}
}

func TestUploadAtomicConditionRejectsConcurrentObjectCreation(t *testing.T) {
	for _, test := range []struct {
		size   int64
		status int
	}{{7, 412}, {uploadPartSize + 1, 412}, {uploadPartSize + 1, 200}} {
		size := test.size
		t.Run(fmt.Sprintf("%d/status-%d", size, test.status), func(t *testing.T) {
			var commits, aborts atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				q := r.URL.Query()
				switch {
				case r.Method == http.MethodHead:
					w.Header().Set("X-Otterio-Conditional-Writes", "v1")
					w.WriteHeader(404)
				case q.Has("uploads"):
					fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned</UploadId></InitiateMultipartUploadResult>`)
				case q.Has("partNumber"):
					io.Copy(io.Discard, r.Body)
					w.Header().Set("ETag", `"part"`)
				case r.Method == http.MethodDelete:
					aborts.Add(1)
					if q.Get("uploadId") != "owned" {
						t.Error("condition failure cleaned another upload")
					}
					w.WriteHeader(204)
				default:
					commits.Add(1)
					if r.Header.Get("If-None-Match") != "*" {
						t.Error("concurrent creation was not protected atomically")
					}
					io.Copy(io.Discard, r.Body)
					w.WriteHeader(test.status)
					fmt.Fprint(w, `<Error><Code>PreconditionFailed</Code><Message>secret-marker concurrent object</Message></Error>`)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			_, err := client.Upload(context.Background(), "bucket", "object", &generatedUploadReader{remaining: size}, size, consoleapi.UploadOptions{}, nil)
			assertAPIError(t, err, 409, "object_exists")
			if commits.Load() != 1 || (size > uploadPartSize && aborts.Load() != 1) {
				t.Fatal("conditional failure retried or skipped owned cleanup")
			}
		})
	}
}

func TestMultipartLengthMismatchAbortsWithoutCompletion(t *testing.T) {
	for _, delta := range []int64{-1, 1} {
		t.Run(strconv.FormatInt(delta, 10), func(t *testing.T) {
			var parts, aborts, completes atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if locationResponse(w, r) {
					return
				}
				q := r.URL.Query()
				switch {
				case q.Has("uploads"):
					fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned</UploadId></InitiateMultipartUploadResult>`)
				case q.Has("partNumber"):
					parts.Add(1)
					io.Copy(io.Discard, r.Body)
					w.Header().Set("ETag", `"part"`)
				case r.Method == http.MethodDelete:
					aborts.Add(1)
					if q.Get("uploadId") != "owned" {
						t.Error("length mismatch cleaned another session")
					}
					w.WriteHeader(204)
				default:
					completes.Add(1)
				}
			}))
			defer server.Close()
			client := testClient(t, Config{S3URL: server.URL})
			const size = uploadPartSize + 2
			_, err := client.Upload(context.Background(), "bucket", "object", &generatedUploadReader{remaining: size + delta}, size, consoleapi.UploadOptions{Overwrite: true}, nil)
			assertAPIError(t, err, 400, "upload_size_mismatch")
			// Prefetch can detect the final length mismatch before the first PUT
			// reaches the server; it must never dispatch the invalid final part.
			if parts.Load() > 1 || aborts.Load() != 1 || completes.Load() != 0 {
				t.Fatal("mismatched multipart body committed or skipped owned cleanup")
			}
		})
	}
}

func TestMultipartCompletionAcknowledgementHasWholeMetadataDeadline(t *testing.T) {
	var completions, aborts atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		q := r.URL.Query()
		switch {
		case q.Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned</UploadId></InitiateMultipartUploadResult>`)
		case q.Has("partNumber"):
			io.Copy(io.Discard, r.Body)
			w.Header().Set("ETag", `"part"`)
		case r.Method == http.MethodDelete:
			aborts.Add(1)
			w.WriteHeader(204)
		default:
			completions.Add(1)
			io.Copy(io.Discard, r.Body)
			fmt.Fprint(w, `<CompleteMultipartUploadResult>`)
			w.(http.Flusher).Flush()
			<-r.Context().Done()
		}
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	client.metadataTimeout = 50 * time.Millisecond
	result := make(chan error, 1)
	go func() {
		_, err := client.Upload(context.Background(), "bucket", "object", &generatedUploadReader{remaining: uploadPartSize + 1}, uploadPartSize+1, consoleapi.UploadOptions{Overwrite: true}, nil)
		result <- err
	}()
	select {
	case err := <-result:
		assertAPIError(t, err, 502, "outcome_unknown")
	case <-time.After(3 * time.Second):
		t.Fatal("completion XML body ignored its metadata deadline")
	}
	if completions.Load() != 1 || aborts.Load() != 1 {
		t.Fatal("stalled completion retried or skipped owned cleanup")
	}
}

func TestMultipartCleanupNeverFollowsCredentialRedirect(t *testing.T) {
	var aborts, forwarded atomic.Int32
	redirect := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { forwarded.Add(1) }))
	defer redirect.Close()
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if locationResponse(w, r) {
			return
		}
		if r.URL.Query().Has("uploads") {
			fmt.Fprint(w, `<InitiateMultipartUploadResult><UploadId>owned</UploadId></InitiateMultipartUploadResult>`)
			return
		}
		if r.Method == http.MethodDelete {
			aborts.Add(1)
			if r.URL.Query().Get("uploadId") != "owned" {
				t.Error("cleanup used another upload session")
			}
			http.Redirect(w, r, redirect.URL, http.StatusTemporaryRedirect)
			return
		}
		io.Copy(io.Discard, r.Body)
		w.WriteHeader(403)
		fmt.Fprint(w, `<Error><Code>AccessDenied</Code><Message>secret-marker part denial</Message></Error>`)
	}))
	defer server.Close()
	client := testClient(t, Config{S3URL: server.URL})
	_, err := client.Upload(context.Background(), "bucket", "object", &generatedUploadReader{remaining: uploadPartSize + 1}, uploadPartSize+1, consoleapi.UploadOptions{Overwrite: true}, nil)
	assertAPIError(t, err, 502, "cleanup_failed")
	if aborts.Load() != 1 || forwarded.Load() != 0 {
		t.Fatal("cleanup retried or forwarded startup credentials")
	}
}
