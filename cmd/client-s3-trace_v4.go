/*
 * MinIO Client (C) 2015 MinIO, Inc.
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
	"net/http"
	"net/http/httputil"

	"github.com/soulteary/mc/pkg/httptracer"
	"github.com/soulteary/otterio/pkg/console"
)

// traceV4 - tracing structure for signature version '4'.
type traceV4 struct{}

// newTraceV4 - initialize Trace structure
func newTraceV4() httptracer.HTTPTracer {
	return traceV4{}
}

// Request - Trace HTTP Request
func (t traceV4) Request(req *http.Request) (err error) {
	reqTrace, err := httputil.DumpRequestOut(redactTraceRequest(req), false)
	if err == nil {
		console.Debug(string(reqTrace))
	}
	return err
}

// Response - Trace HTTP Response
func (t traceV4) Response(resp *http.Response) (err error) {
	// Bodies may include credentials or signed URLs echoed by a server.
	respTrace, err := httputil.DumpResponse(redactTraceResponse(resp), false)
	if err == nil {
		console.Debug(string(respTrace))
	}

	if resp.TLS != nil {
		printTLSCertInfo(resp.TLS)
	}

	return err
}
