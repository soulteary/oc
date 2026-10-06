package cmd

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	minio "github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

func TestCanceledMultipartAbortsOwnedSession(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	released := make(chan struct{})
	aborted := make(chan string, 1)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPost && r.URL.Query().Has("uploads"):
			fmt.Fprint(w, `<InitiateMultipartUploadResult><Bucket>bucket</Bucket><Key>object</Key><UploadId>owned-session</UploadId></InitiateMultipartUploadResult>`)
		case r.Method == http.MethodPut:
			cancel()
			io.Copy(io.Discard, r.Body)
			<-r.Context().Done()
			close(released)
		case r.Method == http.MethodDelete:
			aborted <- r.URL.Query().Get("uploadId")
			w.WriteHeader(http.StatusNoContent)
		default:
			t.Errorf("unexpected request %s %s", r.Method, r.URL)
			w.WriteHeader(500)
		}
	}))
	defer server.Close()
	api, err := minio.New(strings.TrimPrefix(server.URL, "http://"), &minio.Options{Creds: credentials.NewStaticV4("access", "secret", ""), Region: "us-east-1", Transport: uploadSessionTransport{base: http.DefaultTransport}})
	if err != nil {
		t.Fatal(err)
	}
	_, err = putWithCleanup(ctx, api, "bucket", "object", strings.NewReader(strings.Repeat("x", 6*1024*1024)), 6*1024*1024, minio.PutObjectOptions{PartSize: 5 * 1024 * 1024})
	if err == nil {
		t.Fatal("canceled upload succeeded")
	}
	select {
	case id := <-aborted:
		if id != "owned-session" {
			t.Fatal(id)
		}
	case <-time.After(time.Second):
		t.Fatal("session was not aborted")
	}
	select {
	case <-released:
	case <-time.After(time.Second):
		t.Fatal("upload connection remains open")
	}
}
