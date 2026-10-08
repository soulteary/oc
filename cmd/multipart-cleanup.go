package cmd

import (
	"bytes"
	"context"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sync"
	"time"

	minio "github.com/soulteary/otterio-sdk/v7"
)

type multipartCleanupError struct{ cause error }

func (e multipartCleanupError) Error() string {
	return fmt.Sprintf("abort multipart upload: %v", e.cause)
}
func (e multipartCleanupError) Unwrap() error { return e.cause }

// Remember only the upload initiated by this Put call. Never remove sessions
// belonging to another concurrent upload of the same object.
type uploadSessionKey struct{}
type uploadSession struct {
	sync.Mutex
	id string
}
type uploadSessionTransport struct{ base http.RoundTripper }

func (t uploadSessionTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := t.base.RoundTrip(req)
	session, _ := req.Context().Value(uploadSessionKey{}).(*uploadSession)
	if err != nil || session == nil || req.Method != http.MethodPost || !req.URL.Query().Has("uploads") || resp.StatusCode != http.StatusOK {
		return resp, err
	}
	data, readErr := io.ReadAll(resp.Body)
	resp.Body.Close()
	resp.Body = io.NopCloser(bytes.NewReader(data))
	if readErr != nil {
		return resp, readErr
	}
	var result struct {
		UploadID string `xml:"UploadId"`
	}
	if xml.Unmarshal(data, &result) == nil {
		session.Lock()
		session.id = result.UploadID
		session.Unlock()
	}
	return resp, nil
}

func putWithCleanup(ctx context.Context, api *minio.Client, bucket, object string, reader io.Reader, size int64, opts minio.PutObjectOptions) (minio.UploadInfo, error) {
	session := &uploadSession{}
	info, err := api.PutObject(context.WithValue(ctx, uploadSessionKey{}, session), bucket, object, reader, size, opts)
	if err != nil {
		session.Lock()
		id := session.id
		session.Unlock()
		if id != "" {
			// The SDK's abort inherits the canceled upload context. OC owns a bounded
			// cleanup context so cancellation also removes the server-side session.
			cleanup, cancel := context.WithTimeout(context.WithoutCancel(ctx), 2*time.Second)
			defer cancel()
			core := minio.Core{Client: api}
			if abortErr := core.AbortMultipartUpload(cleanup, bucket, object, id); abortErr != nil {
				response := minio.ToErrorResponse(abortErr)
				if response.Code != "NoSuchUpload" {
					return info, errors.Join(err, multipartCleanupError{cause: abortErr})
				}
			}
		}
	}
	return info, err
}
