package oidc

import (
	"bytes"
	"context"
	"crypto/sha256"
	"crypto/x509"
	"encoding/pem"
	"encoding/xml"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	minio "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
)

// This opt-in test starts a disposable real OtterIO and a controlled TLS IdP.
// It is protocol acceptance, not console session or third-party IdP acceptance.
func TestOtterIOSTSIntegration(t *testing.T) {
	binary := os.Getenv("OC_OIDC_TEST_SERVER")
	if binary == "" {
		t.Skip("set OC_OIDC_TEST_SERVER to a built OtterIO binary")
	}
	source := os.Getenv("OC_OIDC_TEST_SERVER_SOURCE")
	if len(source) != 40 {
		t.Fatal("supply the server's 40-character source commit for the evidence record")
	}
	binaryBytes, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(binaryBytes)
	t.Logf("server source (operator supplied)=%s binary SHA256=%x", source, sum)
	f := newFixture(t)
	f.mutate = func(m map[string]any) { m["policy"] = "readonly" }
	root := t.TempDir()
	certs := filepath.Join(root, "certs")
	if err := os.MkdirAll(filepath.Join(certs, "CAs"), 0700); err != nil {
		t.Fatal(err)
	}
	pair := f.server.TLS.Certificates[0]
	cert := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: pair.Certificate[0]})
	key, err := x509.MarshalPKCS8PrivateKey(pair.PrivateKey)
	if err != nil {
		t.Fatal(err)
	}
	for path, data := range map[string][]byte{"public.crt": cert, "private.key": pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: key}), "CAs/idp.crt": cert} {
		if err := os.WriteFile(filepath.Join(certs, path), data, 0600); err != nil {
			t.Fatal(err)
		}
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	address := listener.Addr().String()
	_ = listener.Close()
	access, _ := random()
	secret, _ := random()
	ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, binary, "server", "--address", address, "--certs-dir", certs, filepath.Join(root, "data"))
	for _, v := range os.Environ() {
		if !strings.HasPrefix(v, "OTTERIO_") && !strings.HasPrefix(v, "MC_") && !strings.HasPrefix(v, "OC_") {
			command.Env = append(command.Env, v)
		}
	}
	command.Env = append(command.Env, "OTTERIO_ROOT_USER="+access, "OTTERIO_ROOT_PASSWORD="+secret, "OTTERIO_BROWSER=off", "NO_PROXY=*", "no_proxy=*", "OTTERIO_IDENTITY_OPENID_CONFIG_URL="+f.server.URL+"/.well-known/openid-configuration", "OTTERIO_IDENTITY_OPENID_CLIENT_ID=console")
	log, err := os.OpenFile(filepath.Join(root, "server.log"), os.O_CREATE|os.O_WRONLY, 0600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	command.Stdout = log
	command.Stderr = log
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = command.Process.Kill(); _ = command.Wait() }()
	endpoint := "https://" + address
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodGet, endpoint+"/otterio/health/ready", nil)
		res, err := f.server.Client().Do(req)
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == 200 {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("disposable server did not become ready; inspect protected local log")
		case <-time.After(100 * time.Millisecond):
		}
	}
	// Storage health becomes ready before asynchronous IAM/STS initialization.
	for {
		req, _ := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader("Action=AssumeRoleWithWebIdentity&Version=2011-06-15&WebIdentityToken=invalid"))
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		res, err := f.server.Client().Do(req)
		if err == nil {
			_ = res.Body.Close()
			if res.StatusCode == 400 {
				break
			}
		}
		select {
		case <-ctx.Done():
			t.Fatal("STS initialization timeout")
		case <-time.After(100 * time.Millisecond):
		}
	}
	admin, err := minio.New(address, &minio.Options{Creds: credentials.NewStaticV4(access, secret, ""), Secure: true, Transport: f.server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	bucket := "oidc-acceptance"
	if err := admin.MakeBucket(ctx, bucket, minio.MakeBucketOptions{}); err != nil {
		t.Fatal("fixture bucket", err)
	}
	for _, key := range []string{"allowed", "denied"} {
		if _, err := admin.PutObject(ctx, bucket, key, bytes.NewReader([]byte("fixture")), 7, minio.PutObjectOptions{}); err != nil {
			t.Fatal("fixture object", err)
		}
	}
	policy := fmt.Sprintf(`{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:GetObject"],"Resource":["arn:aws:s3:::%s/allowed"]}]}`, bucket)
	observed := &observingTransport{base: f.server.Client().Transport, t: t}
	c, err := New(ctx, Config{Issuer: f.server.URL, ClientID: "console", RedirectURI: "https://console.example/callback", STSEndpoint: endpoint, SessionPolicy: policy, Transport: observed})
	if err != nil {
		t.Fatal(err)
	}
	a := f.begin(t, c)
	g, err := c.Complete(ctx, binding, a.State, "fixture-code")
	if err != nil {
		t.Fatal("STS exchange", err)
	}
	defer g.Close()
	creds, err := g.Credentials()
	if err != nil {
		t.Fatal(err)
	}
	user, err := minio.New(address, &minio.Options{Creds: credentials.NewStaticV4(creds.AccessKey, creds.SecretKey, creds.SessionToken), Secure: true, Transport: f.server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	read := func(key string) error {
		o, err := user.GetObject(ctx, bucket, key, minio.GetObjectOptions{})
		if err != nil {
			return err
		}
		defer o.Close()
		_, err = io.ReadAll(o)
		return err
	}
	if err := read("allowed"); err != nil {
		t.Fatal("authorized read", err)
	}
	if err := read("denied"); minio.ToErrorResponse(err).Code != "AccessDenied" {
		t.Fatal("session policy did not narrow read access", err)
	}
	// A maximally permissive session policy cannot expand the upstream readonly role.
	widePolicy := `{"Version":"2012-10-17","Statement":[{"Effect":"Allow","Action":["s3:*"],"Resource":["arn:aws:s3:::*"]}]}`
	wideClient, err := New(ctx, Config{Issuer: f.server.URL, ClientID: "console", RedirectURI: "https://console.example/callback", STSEndpoint: endpoint, SessionPolicy: widePolicy, Transport: observed})
	if err != nil {
		t.Fatal(err)
	}
	a = f.begin(t, wideClient)
	wide, err := wideClient.Complete(ctx, binding, a.State, "second-fixture-code")
	if err != nil {
		t.Fatal(err)
	}
	defer wide.Close()
	wc, _ := wide.Credentials()
	writer, err := minio.New(address, &minio.Options{Creds: credentials.NewStaticV4(wc.AccessKey, wc.SecretKey, wc.SessionToken), Secure: true, Transport: f.server.Client().Transport})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := writer.PutObject(ctx, bucket, "forbidden-write", bytes.NewReader([]byte("x")), 1, minio.PutObjectOptions{}); minio.ToErrorResponse(err).Code != "AccessDenied" {
		t.Fatal("session policy expanded readonly role", err)
	}
	f.mu.Lock()
	f.nonce = g.nonce
	f.mu.Unlock()
	if _, err := g.Refresh(ctx); err != nil {
		t.Fatal("refresh with real STS", err)
	}
	t.Log("TLS controlled IdP + real OtterIO: allowed read, narrowed read denial, readonly write denial, same-subject refresh passed")
}

// Reports only structural diagnostics, never raw response bodies or credentials.
type observingTransport struct {
	base http.RoundTripper
	t    *testing.T
}

func (o *observingTransport) RoundTrip(r *http.Request) (*http.Response, error) {
	res, err := o.base.RoundTrip(r)
	if err != nil {
		return res, err
	}
	if r.Method == http.MethodPost && r.URL.Path == "" {
		b, e := io.ReadAll(res.Body)
		_ = res.Body.Close()
		if e != nil {
			return nil, e
		}
		res.Body = io.NopCloser(bytes.NewReader(b))
		var x struct {
			XMLName xml.Name
			Error   struct {
				Code    string `xml:"Code"`
				Message string `xml:"Message"`
			} `xml:"Error"`
		}
		_ = xml.Unmarshal(b, &x)
		o.t.Logf("STS response status=%d root=%s namespace=%s code=%s errorClass=%s", res.StatusCode, x.XMLName.Local, x.XMLName.Space, x.Error.Code, classifySTSErrorMessage(x.Error.Message))
	}
	return res, nil
}

// Classify a fixed set of server errors without exporting raw error messages.
func classifySTSErrorMessage(message string) string {
	switch {
	case message == "":
		return "none"
	case strings.Contains(message, "before issued"):
		return "issued-in-future"
	case strings.Contains(message, "expired"):
		return "expired"
	case strings.Contains(message, "policy claim missing"):
		return "policy-missing-or-not-loaded"
	case strings.Contains(message, "verification error"):
		return "signature-verification"
	case strings.Contains(message, "invalid kid"):
		return "invalid-kid"
	case strings.Contains(message, "Invalid kid"):
		return "invalid-kid"
	case strings.Contains(message, "x509:"):
		return "jwks-certificate"
	case strings.Contains(message, "invalid number of segments"):
		return "malformed-jwt"
	case strings.Contains(message, "Session policy"):
		return "session-policy"
	case strings.Contains(message, "duration"):
		return "duration"
	case strings.Contains(message, "key is of invalid type"):
		return "signing-key-type"
	default:
		return "unclassified"
	}
}
func TestSTSErrorClassificationNeverExportsMessage(t *testing.T) {
	for _, test := range []struct{ message, class string }{{"policy claim missing from the JWT token, credentials will not be generated", "policy-missing-or-not-loaded"}, {"Token used before issued", "issued-in-future"}, {"Get https://private-token.example: x509: certificate signed by unknown authority", "jwks-certificate"}, {"private-token secret-value", "unclassified"}} {
		if classifySTSErrorMessage(test.message) != test.class {
			t.Fatal("incorrect structural classification")
		}
	}
}
