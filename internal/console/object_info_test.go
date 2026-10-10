package console

import (
	"context"
	"net/http"
	"testing"

	"github.com/soulteary/mc/internal/consoleapi"
)

func TestObjectInfoUsesExactAuthenticatedScope(t *testing.T) {
	reads := 0
	backend := &archiveBackend{stat: func(ctx context.Context, ref consoleapi.ObjectRef) (consoleapi.ObjectInfo, error) {
		reads++
		if ref.Bucket != "known-bucket" || ref.Key != "a +%.png" || ref.VersionID != "" {
			t.Fatalf("wrong scope: %#v", ref)
		}
		return consoleapi.ObjectInfo{Size: 12, ETag: "etag", ContentType: "image/png"}, nil
	}}
	s, _ := archiveServer(t, backend, 1024)
	cookie, _ := signIn(t, s)
	target := "/api/object-info?bucket=known-bucket&key=a+%2B%25.png"
	if w := jobRequest(s, http.MethodGet, target, "", nil, ""); w.Code != 401 {
		t.Fatalf("anonymous: %d", w.Code)
	}
	if reads != 0 {
		t.Fatal("anonymous metadata read")
	}
	if w := jobRequest(s, http.MethodGet, target, "", cookie, ""); w.Code != 200 {
		t.Fatalf("metadata: %d %s", w.Code, w.Body.String())
	}
	for _, suffix := range []string{"&bucket=other", "&versionId=unexpected", "&key=other"} {
		if w := jobRequest(s, http.MethodGet, target+suffix, "", cookie, ""); w.Code != 400 {
			t.Fatalf("invalid query: %d", w.Code)
		}
	}
	if reads != 1 {
		t.Fatalf("reads: %d", reads)
	}
}
