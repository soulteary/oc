// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package main

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/soulteary/mc/internal/consoleapi"
)

// lifecycleOutput allows run's startup writer and the test to observe the real
// printed URL without polling a buffer concurrently or choosing a fixed port.
type lifecycleOutput struct {
	mu      sync.Mutex
	buffer  bytes.Buffer
	ready   chan struct{}
	started sync.Once
}

func (w *lifecycleOutput) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	n, err := w.buffer.Write(data)
	if strings.Contains(w.buffer.String(), "\nSessions expire after") {
		w.started.Do(func() { close(w.ready) })
	}
	return n, err
}

func (w *lifecycleOutput) String() string {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.buffer.String()
}

func TestRunLocalConsoleLifecycle(t *testing.T) {
	// A user's environment must not replace this test's explicit local alias.
	for _, name := range []string{"OC_HOST_lifecycle", "MC_HOST_lifecycle", "OC_ADMIN_URL", "OC_ADMIN_URL_lifecycle", "OC_ADMIN_CA", "OC_ADMIN_CA_lifecycle"} {
		t.Setenv(name, "")
	}
	const accessKey = "lifecycle-private-access"
	const secretKey = "lifecycle-private-secret"
	const sessionToken = "lifecycle-private-session"
	var requests atomic.Int32
	upstreamPeers := make(chan string, 4)
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		requests.Add(1)
		if r.Method != http.MethodGet || r.URL.Path != "/" || r.URL.RawQuery != "" {
			t.Error("console issued an unexpected storage operation")
			w.WriteHeader(http.StatusBadRequest)
			return
		}
		if !strings.Contains(r.Header.Get("Authorization"), "Credential="+accessKey+"/") || r.Header.Get("X-Amz-Security-Token") != sessionToken {
			t.Error("console did not use the startup-selected S3 identity")
			w.WriteHeader(http.StatusForbidden)
			return
		}
		upstreamPeers <- r.RemoteAddr
		w.Header().Set("Content-Type", "application/xml")
		fmt.Fprint(w, `<ListAllMyBucketsResult xmlns="http://s3.amazonaws.com/doc/2006-03-01/"><Owner><ID>operator</ID><DisplayName>operator</DisplayName></Owner><Buckets><Bucket><Name>lifecycle-bucket</Name><CreationDate>2026-10-08T00:00:00Z</CreationDate></Bucket></Buckets></ListAllMyBucketsResult>`)
	}))
	t.Cleanup(upstream.Close)
	configDir := t.TempDir()
	original, err := json.Marshal(map[string]any{
		"version": "10", "aliases": map[string]aliasConfig{
			"lifecycle": {URL: upstream.URL, AccessKey: accessKey, SecretKey: secretKey, SessionToken: sessionToken, API: "S3v4", Path: "on"},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	configPath := filepath.Join(configDir, "config.json")
	if err := os.WriteFile(configPath, original, 0600); err != nil {
		t.Fatal(err)
	}
	before, err := os.Stat(configPath)
	if err != nil {
		t.Fatal(err)
	}
	output := &lifecycleOutput{ready: make(chan struct{})}
	errorOutput := &lifecycleOutput{ready: make(chan struct{})}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	var runError error
	go func() {
		runError = run(ctx, []string{"--config-dir", configDir, "--data-dir", t.TempDir(), "--alias", "lifecycle", "--address", "127.0.0.1:0"}, output, errorOutput)
		close(done)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(5 * time.Second):
			t.Error("console process did not stop during cleanup")
		}
	})
	select {
	case <-output.ready:
	case <-done:
		t.Fatalf("console stopped before starting: %v", runError)
	case <-time.After(5 * time.Second):
		t.Fatal("console did not print its startup URL")
	}
	var baseURL, code string
	for _, line := range strings.Split(output.String(), "\n") {
		if strings.HasPrefix(line, "URL: ") {
			baseURL = strings.TrimPrefix(line, "URL: ")
		}
		if strings.HasPrefix(line, "Login code: ") {
			code = strings.TrimPrefix(line, "Login code: ")
		}
	}
	u, err := url.Parse(baseURL)
	if err != nil || u == nil || u.Scheme != "http" || u.Hostname() != "127.0.0.1" || u.Port() == "" || u.Port() == "0" || len(code) < 24 || u.RawQuery != "" || u.Fragment != "" {
		t.Fatal("startup did not provide a safe local URL and login code")
	}
	jar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	transport := &http.Transport{Proxy: nil}
	t.Cleanup(transport.CloseIdleConnections)
	client := &http.Client{Transport: transport, Jar: jar, Timeout: 3 * time.Second}
	response, err := client.Get(baseURL + "/api/buckets")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusUnauthorized || requests.Load() != 0 {
		t.Fatal("anonymous browser accessed the upstream storage API")
	}
	body, err := json.Marshal(map[string]string{"code": code})
	if err != nil {
		t.Fatal(err)
	}
	request, err := http.NewRequest(http.MethodPost, baseURL+"/api/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", baseURL)
	request.Header.Set("Content-Type", "application/json")
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	var session struct {
		Alias     string `json:"alias"`
		ReadOnly  bool   `json:"readOnly"`
		CSRFToken string `json:"csrfToken"`
	}
	decodeError := json.NewDecoder(response.Body).Decode(&session)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeError != nil || session.Alias != "lifecycle" || !session.ReadOnly || session.CSRFToken == "" || len(jar.Cookies(u)) != 1 {
		t.Fatal("console login did not establish the selected read-only session")
	}
	response, err = client.Get(baseURL + "/api/buckets")
	if err != nil {
		t.Fatal(err)
	}
	var listing struct {
		Buckets []consoleapi.Bucket `json:"buckets"`
	}
	decodeError = json.NewDecoder(response.Body).Decode(&listing)
	_ = response.Body.Close()
	if response.StatusCode != http.StatusOK || decodeError != nil || len(listing.Buckets) != 1 || listing.Buckets[0].Name != "lifecycle-bucket" || requests.Load() != 1 {
		t.Fatal("authenticated console did not return the real S3 bucket response")
	}
	firstPeer := <-upstreamPeers
	secondJar, err := cookiejar.New(nil)
	if err != nil {
		t.Fatal(err)
	}
	secondClient := &http.Client{Transport: transport, Jar: secondJar, Timeout: 3 * time.Second}
	request, err = http.NewRequest(http.MethodPost, baseURL+"/api/login", bytes.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", baseURL)
	request.Header.Set("Content-Type", "application/json")
	response, err = secondClient.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 || len(secondJar.Cookies(u)) != 1 {
		t.Fatal("second browser failed to sign in")
	}
	response, err = secondClient.Get(baseURL + "/api/buckets")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("second browser failed to read storage")
	}
	secondPeer := <-upstreamPeers
	if firstPeer == secondPeer {
		t.Fatal("browser sessions shared an upstream transport connection")
	}
	request, err = http.NewRequest(http.MethodPost, baseURL+"/api/logout", nil)
	if err != nil {
		t.Fatal(err)
	}
	request.Header.Set("Origin", baseURL)
	request.Header.Set("X-CSRF-Token", session.CSRFToken)
	response, err = client.Do(request)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 204 {
		t.Fatal("first browser failed to log out")
	}
	response, err = secondClient.Get(baseURL + "/api/buckets")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.Copy(io.Discard, response.Body)
	_ = response.Body.Close()
	if response.StatusCode != 200 {
		t.Fatal("logging out the first browser retired the second browser's client")
	}
	if peer := <-upstreamPeers; peer != secondPeer {
		t.Fatal("logout closed another session's upstream transport pool")
	}
	shutdownStarted := time.Now()
	cancel()
	select {
	case <-done:
		if runError != nil {
			t.Fatalf("console shutdown failed: %v", runError)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("console did not shut down within five seconds")
	}
	if time.Since(shutdownStarted) > 5*time.Second {
		t.Fatal("console shutdown exceeded its budget")
	}
	connection, err := net.DialTimeout("tcp", u.Host, time.Second)
	if err == nil {
		_ = connection.Close()
		t.Fatal("console listener remained open after cancellation")
	}
	afterBytes, err := os.ReadFile(configPath)
	if err != nil || !bytes.Equal(afterBytes, original) {
		t.Fatal("console modified the OC configuration")
	}
	after, err := os.Stat(configPath)
	if err != nil || after.Mode() != before.Mode() || !after.ModTime().Equal(before.ModTime()) {
		t.Fatal("console rewrote the configuration file or changed its permissions")
	}
	entries, err := os.ReadDir(configDir)
	if err != nil || len(entries) != 1 || entries[0].Name() != "config.json" {
		t.Fatal("console created persistent configuration or session files")
	}
	printed := output.String() + errorOutput.String()
	for _, credential := range []string{accessKey, secretKey, sessionToken} {
		if strings.Contains(printed, credential) {
			t.Fatal("startup output disclosed an upstream credential")
		}
	}
}
