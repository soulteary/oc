/*
 * MinIO Client (C) 2017-2020 MinIO, Inc.
 *
 * Licensed under the Apache License, Version 2.0 (the "License");
 * you may not use this file except in compliance with the License.
 * You may obtain a copy of the License at
 *
 *     http://www.apache.org/licenses/LICENSE-2.0
 *
 * Unless required by applicable law or agreed to in writing, software
 * distributed under the License is distributed on an "AS IS" BASIS,
 * WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
 * See the License for the specific language governing permissions and
 * limitations under the License.
 */

package cmd

import (
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"net"
	"net/http"
	"sync"
	"time"

	"github.com/mattn/go-ieproxy"
	"github.com/minio/minio-go/v7/pkg/credentials"
	"github.com/soulteary/mc/pkg/httptracer"
	"github.com/soulteary/mc/pkg/probe"
	"github.com/soulteary/otterio/pkg/madmin"
)

// Include every credential and transport setting; different identities, TLS
// policies and session tokens must never share a cached admin client.
type adminClientKey struct {
	Endpoint, AccessKey, SecretKey, SessionToken, AppName, AppVersion, CAFingerprint string
	Insecure, Debug                                                                  bool
	RootCAs                                                                          *x509.CertPool
}

// NewAdminFactory encloses New function with client cache.
func NewAdminFactory() func(config *Config) (*madmin.AdminClient, *probe.Error) {
	clientCache := make(map[adminClientKey]*madmin.AdminClient)
	mutex := &sync.Mutex{}

	// Return New function.
	return func(config *Config) (*madmin.AdminClient, *probe.Error) {
		// Creates a parsed URL.
		targetURL, e := validateAdminEndpoint(config.HostURL)
		if e != nil {
			return nil, probe.NewError(e)
		}
		useTLS := targetURL.Scheme == "https"
		hostName := targetURL.Host
		roots := globalRootCAs
		fingerprint := ""
		if config.AdminCAFile != "" {
			roots, fingerprint, e = loadAdminCAs(config.AdminCAFile)
			if e != nil {
				return nil, probe.NewError(e)
			}
		}
		key := adminClientKey{Endpoint: targetURL.String(), AccessKey: config.AccessKey,
			SecretKey: config.SecretKey, SessionToken: config.SessionToken,
			AppName: config.AppName, AppVersion: config.AppVersion,
			Insecure: config.Insecure, Debug: config.Debug, RootCAs: roots,
			CAFingerprint: fingerprint}
		// A custom CA pool is rebuilt on each call; its certificate digest is the key.
		if config.AdminCAFile != "" {
			key.RootCAs = nil
		}

		// Lookup previous cache by hash.
		mutex.Lock()
		defer mutex.Unlock()
		var api *madmin.AdminClient
		var found bool
		if api, found = clientCache[key]; !found {
			// Admin API only supports signature v4.
			creds := credentials.NewStaticV4(config.AccessKey, config.SecretKey, config.SessionToken)

			// Not found. Instantiate a new MinIO
			var e error
			api, e = madmin.NewWithOptions(hostName, &madmin.Options{
				Creds:  creds,
				Secure: useTLS,
			})
			if e != nil {
				return nil, probe.NewError(e)
			}

			// Keep TLS config.
			tlsConfig := &tls.Config{
				RootCAs: roots,
				// Can't use SSLv3 because of POODLE and BEAST
				// Can't use TLSv1.0 because of POODLE and BEAST using CBC cipher
				// Can't use TLSv1.1 because of RC4 cipher usage
				MinVersion: tls.VersionTLS12,
			}
			if config.Insecure {
				tlsConfig.InsecureSkipVerify = true
			}

			var transport http.RoundTripper = &http.Transport{
				Proxy: ieproxy.GetProxyFunc(),
				DialContext: (&net.Dialer{
					Timeout:   10 * time.Second,
					KeepAlive: 15 * time.Second,
				}).DialContext,
				MaxIdleConnsPerHost:   256,
				IdleConnTimeout:       90 * time.Second,
				TLSHandshakeTimeout:   10 * time.Second,
				ExpectContinueTimeout: 10 * time.Second,
				TLSClientConfig:       tlsConfig,
				// Set this value so that the underlying transport round-tripper
				// doesn't try to auto decode the body of objects with
				// content-encoding set to `gzip`.
				//
				// Refer:
				//    https://golang.org/src/net/http/transport.go?h=roundTrip#L1843
				DisableCompression: true,
			}

			if config.Debug {
				transport = httptracer.GetNewTraceTransport(newTraceV4(), transport)
			}

			// Set custom transport.
			api.SetCustomTransport(adminNoRedirectTransport{transport})

			// Set app info.
			api.SetAppInfo(config.AppName, config.AppVersion)

			// Cache the new MinIO Client with hash of config as key.
			clientCache[key] = api
		}

		// Store the new api object.
		return api, nil
	}
}

// newAdminClient gives a new client interface
func newAdminClient(aliasedURL string) (*madmin.AdminClient, *probe.Error) {
	alias, urlStrFull, aliasCfg, err := expandAlias(aliasedURL)
	if err != nil {
		return nil, err.Trace(aliasedURL)
	}
	// Verify if the aliasedURL is a real URL, fail in those cases
	// indicating the user to add alias.
	if aliasCfg == nil && urlRgx.MatchString(aliasedURL) {
		return nil, errInvalidAliasedURL(aliasedURL).Trace(aliasedURL)
	}

	if aliasCfg == nil {
		return nil, probe.NewError(fmt.Errorf("no valid configuration found for '%s' host alias", urlStrFull))
	}

	endpoint, caFile := resolveAdminSettings(alias, aliasCfg)
	s3Config := NewS3Config(endpoint, aliasCfg)
	s3Config.AdminCAFile = caFile

	s3Client, err := s3AdminNew(s3Config)
	if err != nil {
		return nil, err.Trace(alias, urlStrFull)
	}
	return s3Client, nil
}

// s3AdminNew returns an initialized minioAdmin structure. If debug is enabled,
// it also enables an internal trace transport.
var s3AdminNew = NewAdminFactory()
