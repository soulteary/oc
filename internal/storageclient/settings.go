// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"bytes"
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/xml"
	"errors"
	"io"
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/consoleapi"
	minio "github.com/soulteary/otterio-sdk/v7"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/s3utils"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	"github.com/soulteary/otterio/pkg/madmin"
)

const (
	configCapabilityHeader = "X-Otterio-Bucket-Config"
	configRevisionHeader   = "X-Otterio-Config-Revision"
	configConditionHeader  = "X-Otterio-Config-If-Match"
	configExistsHeader     = "X-Otterio-Config-Exists"
	selfCapabilityHeader   = "X-Otterio-Self-Credentials"
	maxSettingDocument     = 1024 * 1024
	maxManagementReply     = 32 * 1024
	maxSelfReply           = 1024
)

var _ consoleapi.SettingsBackend = (*Client)(nil)

func settingUnsupported() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusNotImplemented, Code: "settings_unsupported", Message: "This storage server cannot safely update this setting. Its current value can still be read."}
}

func settingConflict() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusConflict, Code: "setting_conflict", Message: "This setting changed since it was loaded. Reload it before editing again."}
}

func settingOutcomeUnknown() *consoleapi.Error {
	return &consoleapi.Error{Status: http.StatusBadGateway, Code: "outcome_unknown", Message: "The current configuration could not be confirmed. Reload it before editing again; the change may already be saved."}
}

func validRevision(revision string) bool {
	if len(revision) != 64 {
		return false
	}
	for _, ch := range revision {
		if !(ch >= '0' && ch <= '9') && !(ch >= 'a' && ch <= 'f') {
			return false
		}
	}
	return true
}

func singleProtocolHeader(header http.Header, name string) string {
	values := header.Values(name)
	if len(values) != 1 {
		return ""
	}
	return values[0]
}

func validSettingTarget(bucket, kind string) bool {
	return s3utils.CheckValidBucketName(bucket) == nil && (kind == "policy" || kind == "versioning" || kind == "lifecycle")
}

func readBounded(body io.ReadCloser, limit int64) ([]byte, error) {
	defer body.Close()
	data, err := io.ReadAll(io.LimitReader(body, limit+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > limit {
		return nil, errors.New("storage reply exceeds its limit")
	}
	return data, nil
}

// Each observation belongs to one SDK GET. Location discovery, other calls and
// parallel edits cannot supply its capability, revision or request address.
type settingObservation struct {
	request   *http.Request
	status    int
	header    http.Header
	document  []byte
	readError error
	base      http.RoundTripper
	kind      string
}

func (o *settingObservation) RoundTrip(req *http.Request) (*http.Response, error) {
	resp, err := o.base.RoundTrip(req)
	if req.Method != http.MethodGet || !req.URL.Query().Has(o.kind) || resp == nil {
		return resp, err
	}
	o.request, o.status, o.header = req.Clone(req.Context()), resp.StatusCode, resp.Header.Clone()
	if resp.Body == nil {
		return resp, err
	}
	o.document, o.readError = readBounded(resp.Body, maxSettingDocument)
	if o.readError != nil {
		return nil, o.readError
	}
	resp.Body = io.NopCloser(bytes.NewReader(o.document))
	return resp, err
}

// The SDK builds the URL, bucket lookup and regional signature. Capture the
// raw reply before its typed XML decoder can discard extensions. The clone has
// its own transport and lookup state; the shared startup client is not changed.
func (c *Client) bucketSetting(ctx context.Context, bucket, kind string) (consoleapi.BucketSetting, *settingObservation, error) {
	result := consoleapi.BucketSetting{Bucket: bucket, Kind: kind, Format: "xml"}
	if kind == "policy" {
		result.Format = "json"
	}
	if err := c.ready(ctx); err != nil {
		return result, nil, err
	}
	if !validSettingTarget(bucket, kind) {
		return result, nil, invalidRequest()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	observation := &settingObservation{base: c.s3Transport, kind: kind}
	opts := c.s3Options
	opts.Transport = observation
	client, err := minio.New(c.s3Endpoint, &opts)
	if err != nil {
		return result, nil, configError()
	}
	client.SetAppInfo(c.appName, c.appVersion)
	switch kind {
	case "policy":
		_, err = client.GetBucketPolicy(requestCtx, bucket)
	case "versioning":
		_, err = client.GetBucketVersioning(requestCtx, bucket)
	case "lifecycle":
		_, err = client.GetBucketLifecycle(requestCtx, bucket)
	}
	absent := kind == "lifecycle" && observation.status == http.StatusNotFound && minio.ToErrorResponse(err).Code == "NoSuchLifecycleConfiguration"
	// GetBucketPolicy translates NoSuchBucketPolicy to an empty success.
	if kind == "policy" && err == nil && observation.status == http.StatusNotFound {
		absent = true
	}
	// A supported legacy lifecycle root is not understood by the SDK's typed
	// DTO. Its HTTP request still supplies discovery, authentication and the
	// signature; a complete valid XML configuration is the read result. Never
	// reinterpret a failed HTTP reply, read failure or error document as data.
	rawLifecycle := kind == "lifecycle" && observation.status == http.StatusOK &&
		observation.readError == nil && observation.request != nil && requestCtx.Err() == nil &&
		validSettingDocument(kind, string(observation.document))
	if err != nil && !absent && !rawLifecycle {
		return result, nil, normalizeError(err)
	}
	if observation.readError != nil || observation.request == nil || (observation.status != http.StatusOK && !absent) || (kind == "lifecycle" && !absent && !rawLifecycle) {
		return result, nil, normalizeError(errors.New("invalid setting response"))
	}
	result.Exists = !absent
	if result.Exists {
		result.Document = string(observation.document)
	}
	result.Revision = singleProtocolHeader(observation.header, configRevisionHeader)
	existsValue := singleProtocolHeader(observation.header, configExistsHeader)
	protocolSupported := singleProtocolHeader(observation.header, configCapabilityHeader) == "v1" && validRevision(result.Revision) && (existsValue == "true" || existsValue == "false")
	if protocolSupported {
		// Policy and lifecycle absence is represented by HTTP 404. Only
		// versioning has a standard empty XML document with HTTP 200.
		if kind != "versioning" && result.Exists != (existsValue == "true") {
			return result, nil, normalizeError(errors.New("contradictory setting response"))
		}
		result.Exists = existsValue == "true"
	}
	_, isV4 := signatureRegion(observation.request)
	result.Conditional = protocolSupported && isV4
	if !result.Conditional {
		result.Revision = ""
	}
	return result, observation, nil
}

func (c *Client) BucketSetting(ctx context.Context, bucket, kind string) (consoleapi.BucketSetting, error) {
	setting, _, err := c.bucketSetting(ctx, bucket, kind)
	return setting, err
}

func validSettingDocument(kind, document string) bool {
	limit := maxSettingDocument
	if kind == "policy" {
		limit = 20 * 1024
	}
	if document == "" || len(document) > limit || !utf8.ValidString(document) || strings.ContainsRune(document, 0) {
		return false
	}
	if kind == "policy" {
		var value map[string]json.RawMessage
		return json.Unmarshal([]byte(document), &value) == nil && value != nil
	}
	wantRoot := "VersioningConfiguration"
	if kind == "lifecycle" {
		wantRoot = "LifecycleConfiguration"
	}
	decoder := xml.NewDecoder(strings.NewReader(document))
	depth, roots := 0, 0
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			return roots == 1 && depth == 0
		}
		if err != nil {
			return false
		}
		switch value := token.(type) {
		case xml.StartElement:
			if depth == 0 {
				roots++
				legacyLifecycle := kind == "lifecycle" && value.Name.Local == "BucketLifecycleConfiguration"
				if (value.Name.Local != wantRoot && !legacyLifecycle) || roots > 1 {
					return false
				}
			}
			depth++
		case xml.EndElement:
			depth--
		case xml.Directive:
			return false
		case xml.CharData:
			if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return false
			}
		}
	}
}

func signatureRegion(req *http.Request) (string, bool) {
	auth := req.Header.Get("Authorization")
	if !strings.HasPrefix(auth, "AWS4-HMAC-SHA256 ") {
		return "", false
	}
	_, credential, ok := strings.Cut(auth, "Credential=")
	if !ok {
		return "", false
	}
	credential, _, _ = strings.Cut(credential, ",")
	scope := strings.Split(credential, "/")
	if len(scope) != 5 || scope[3] != "s3" || scope[4] != "aws4_request" {
		return "", false
	}
	return scope[2], true
}

func (c *Client) selectedCredentials(ctx context.Context) (credentials.Value, error) {
	// New installs a static provider for this startup identity. Use the SDK's
	// owned HTTP context without consulting another alias or a global client.
	credentialContext := c.s3.CredContext()
	credentialContext.Context = ctx
	return c.s3Options.Creds.GetWithContext(credentialContext)
}

// Save performs one conditional write, using the server-issued revision that
// the browser loaded. A preceding GET is only capability/address discovery;
// the atomic check happens in the server, including edits from another client.
func (c *Client) SaveBucketSetting(ctx context.Context, bucket, kind, document, revision string, remove bool) (consoleapi.BucketSetting, error) {
	result := consoleapi.BucketSetting{}
	if !validSettingTarget(bucket, kind) || !validRevision(revision) || (remove && (document != "" || kind == "versioning")) || (!remove && !validSettingDocument(kind, document)) {
		return result, invalidRequest()
	}
	setting, observation, err := c.bucketSetting(ctx, bucket, kind)
	if err != nil {
		return result, err
	}
	region, isV4 := signatureRegion(observation.request)
	if !setting.Conditional || !isV4 {
		return result, settingUnsupported()
	}
	if setting.Revision != revision {
		return result, settingConflict()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return result, settingUnsupported()
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	method := http.MethodPut
	if remove {
		method = http.MethodDelete
	}
	req, err := http.NewRequestWithContext(requestCtx, method, observation.request.URL.String(), strings.NewReader(document))
	if err != nil {
		return result, invalidRequest()
	}
	req.Header.Set("User-Agent", observation.request.Header.Get("User-Agent"))
	req.Header.Set(configConditionHeader, revision)
	req.Header.Set("Content-Type", "application/"+setting.Format)
	setPayloadHash(req, []byte(document))
	if kind != "policy" && !remove {
		checksum := md5.Sum([]byte(document))
		req.Header.Set("Content-Md5", base64.StdEncoding.EncodeToString(checksum[:]))
	}
	req = signer.SignV4(*req, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, region)
	resp, err := c.s3Transport.RoundTrip(req)
	if err != nil {
		return result, settingOutcomeUnknown()
	}
	data, readErr := readBounded(resp.Body, maxManagementReply)
	if readErr != nil {
		return result, settingOutcomeUnknown()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return result, settingResponseError(resp.StatusCode, data, false, true)
	}
	if (resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusNoContent) || len(data) != 0 {
		return result, settingOutcomeUnknown()
	}
	nextRevision := singleProtocolHeader(resp.Header, configRevisionHeader)
	expectedExists := "true"
	if remove {
		expectedExists = "false"
	}
	if singleProtocolHeader(resp.Header, configCapabilityHeader) != "v1" || !validRevision(nextRevision) || singleProtocolHeader(resp.Header, configExistsHeader) != expectedExists {
		return result, settingOutcomeUnknown()
	}
	// The server validates and may normalize a document before persistence.
	// Display the actual saved bytes, never a locally reconstructed policy or
	// the submitted text. This is a read-only verification, not a write retry.
	current, _, err := c.bucketSetting(ctx, bucket, kind)
	if err != nil || !current.Conditional {
		return result, settingOutcomeUnknown()
	}
	if current.Revision != nextRevision {
		return result, settingConflict()
	}
	if current.Exists == remove {
		return result, settingOutcomeUnknown()
	}
	return current, nil
}

func setPayloadHash(req *http.Request, data []byte) {
	checksum := sha256.Sum256(data)
	req.Header.Set("X-Amz-Content-Sha256", hex.EncodeToString(checksum[:]))
}

// Metadata must be one JSON object with unambiguous, exact field names.
func managementJSONObject(data []byte) (map[string]json.RawMessage, error) {
	invalid := errors.New("invalid storage error response")
	if !utf8.Valid(data) {
		return nil, invalid
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	start, err := decoder.Token()
	if err != nil || start != json.Delim('{') {
		return nil, invalid
	}
	fields := make(map[string]json.RawMessage)
	for decoder.More() {
		token, err := decoder.Token()
		key, ok := token.(string)
		if _, seen := fields[key]; err != nil || !ok || seen {
			return nil, invalid
		}
		var value json.RawMessage
		if decoder.Decode(&value) != nil {
			return nil, invalid
		}
		fields[key] = value
	}
	end, err := decoder.Token()
	if err != nil || end != json.Delim('}') || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return nil, invalid
	}
	return fields, nil
}

func managementErrorCode(data []byte, admin bool) (string, error) {
	invalid := errors.New("invalid storage error response")
	if !utf8.Valid(data) {
		return "", invalid
	}
	if admin {
		fields, err := managementJSONObject(data)
		var code string
		if err != nil || json.Unmarshal(fields["Code"], &code) != nil || code == "" {
			return "", invalid
		}
		return code, nil
	}
	decoder := xml.NewDecoder(bytes.NewReader(data))
	depth, roots, codes := 0, 0, 0
	inCode := false
	var code strings.Builder
	for {
		token, err := decoder.Token()
		if err == io.EOF {
			if roots == 1 && depth == 0 && codes == 1 && code.Len() != 0 {
				return code.String(), nil
			}
			return "", invalid
		}
		if err != nil {
			return "", invalid
		}
		switch value := token.(type) {
		case xml.StartElement:
			if inCode {
				return "", invalid
			}
			if depth == 0 {
				roots++
				if roots != 1 || value.Name.Local != "Error" {
					return "", invalid
				}
			} else if depth == 1 && value.Name.Local == "Code" {
				codes++
				if codes != 1 || len(value.Attr) != 0 {
					return "", invalid
				}
				inCode = true
			}
			depth++
		case xml.EndElement:
			if inCode {
				inCode = false
			}
			depth--
		case xml.CharData:
			if inCode {
				code.Write(value)
			} else if depth == 0 && strings.TrimSpace(string(value)) != "" {
				return "", invalid
			}
		case xml.Directive:
			return "", invalid
		}
	}
}

// Only protocol-consistent, known pre-commit errors establish failure. Once
// dispatched, redirects and unrecognized responses have uncertain outcome.
func settingResponseError(status int, data []byte, admin, dispatched bool) *consoleapi.Error {
	unknown := settingOutcomeUnknown
	if admin {
		unknown = outcomeUnknown
	}
	if status >= 300 && status < 400 {
		if dispatched {
			return unknown()
		}
		return &consoleapi.Error{Status: http.StatusBadGateway, Code: "RedirectDisabled", Message: "The storage endpoint redirected the request. Check its configured address."}
	}
	code, err := managementErrorCode(data, admin)
	if err != nil {
		return unknown()
	}
	if (admin && code == "CredentialConflict") || (status >= 500 && !(status == http.StatusNotImplemented && (code == "NotImplemented" || code == "XMinioAdminNotImplemented"))) {
		// Persistence can finish before the response or peer propagation
		// fails. A competing secret rotation also invalidates this selected
		// credential, even though this particular write did not commit.
		return unknown()
	}
	if (status == http.StatusPreconditionFailed && code == "PreconditionFailed") || (status == http.StatusConflict && code == "ConditionalRequestConflict") {
		return settingConflict()
	}
	switch code {
	case "MalformedPolicy", "MalformedXML", "InvalidPolicyDocument", "AdminConfigBadJSON", "AdminConfigTooLarge", "XOtterioAdminConfigBadJSON", "XOtterioAdminConfigTooLarge", "XMinioAdminConfigBadJSON", "XMinioAdminConfigTooLarge", "XAmzContentSHA256Mismatch", "BadDigest", "InvalidDigest", "IncompleteBody", "MissingContentMD5", "EntityTooLarge", "PolicyTooLarge", "AuthorizationHeaderMalformed":
		if status != http.StatusBadRequest {
			return unknown()
		}
		return invalidRequest()
	case "MissingContentLength":
		if status != http.StatusLengthRequired {
			return unknown()
		}
		return invalidRequest()
	case "InvalidBucketState":
		if status != http.StatusConflict {
			return unknown()
		}
		return &consoleapi.Error{Status: http.StatusConflict, Code: "InvalidBucketState", Message: "The bucket's current lock or replication settings do not allow this change."}
	case "XOtterioAdminRemoteTargetNotFoundError":
		if status != http.StatusNotFound {
			return unknown()
		}
		return &consoleapi.Error{Status: http.StatusNotFound, Code: code, Message: "The lifecycle storage target is not configured."}
	case "RemoteDestinationNotFoundError":
		if status != http.StatusNotFound {
			return unknown()
		}
		return &consoleapi.Error{Status: http.StatusNotFound, Code: code, Message: "The lifecycle destination bucket could not be found."}
	case "InvalidTokenId":
		code = "InvalidToken"
	}
	failure := normalizeError(minio.ErrorResponse{Code: code, StatusCode: status})
	if failure.Code == "UpstreamError" || (failure.Status != status && !(failure.Code == "AccessDenied" && status == http.StatusUnauthorized)) {
		return unknown()
	}
	return failure
}

func (c *Client) selfRequest(ctx context.Context, method string, body []byte) (*http.Response, error) {
	credential, err := c.selectedCredentials(ctx)
	if err != nil {
		return nil, err
	}
	req, err := http.NewRequestWithContext(ctx, method, c.adminEndpoint+"/otterio/admin/v3/self-credentials", bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", c.appName+"/"+c.appVersion)
	setPayloadHash(req, body)
	req = signer.SignV4(*req, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, "")
	return c.adminTransport.RoundTrip(req)
}

func (c *Client) SelfAccount(ctx context.Context) (consoleapi.SelfAccount, error) {
	account := consoleapi.SelfAccount{Kind: "unknown", Status: "unknown"}
	if err := c.ready(ctx); err != nil {
		return account, err
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	resp, err := c.selfRequest(requestCtx, http.MethodGet, nil)
	if err != nil {
		return account, normalizeError(err)
	}
	if resp.StatusCode == http.StatusUnauthorized || resp.StatusCode == http.StatusForbidden {
		// This read-only rejection is conclusive without parsing a proxy's
		// potentially large or stalled denial page.
		_ = resp.Body.Close()
		return account, normalizeError(minio.ErrorResponse{Code: "AccessDenied", StatusCode: resp.StatusCode})
	}
	if singleProtocolHeader(resp.Header, selfCapabilityHeader) != "v1" && (resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusBadRequest || resp.StatusCode == http.StatusNotFound || resp.StatusCode == http.StatusNotImplemented) {
		// Legacy endpoints can return a browser page or a large S3 error at
		// this unknown path (including InvalidBucketName/400). Their content
		// is neither needed nor trusted, and can never grant a write.
		_ = resp.Body.Close()
		return account, nil
	}
	data, err := readBounded(resp.Body, maxSelfReply)
	if err != nil {
		return account, normalizeError(err)
	}
	if resp.StatusCode != http.StatusOK {
		failure := settingResponseError(resp.StatusCode, data, true, false)
		if failure.Code == "outcome_unknown" {
			// A GET cannot rotate a secret. Do not retire a connection merely
			// because a legacy discovery reply was incomplete or malformed.
			failure = normalizeError(errors.New("invalid self account response"))
		}
		return account, failure
	}
	if singleProtocolHeader(resp.Header, selfCapabilityHeader) != "v1" {
		return account, nil
	}
	fields, err := managementJSONObject(data)
	if err != nil {
		return consoleapi.SelfAccount{}, normalizeError(err)
	}
	// Exact lowerCamel names are the public protocol. encoding/json's
	// case-insensitive struct matching would let aliases override its hint.
	for _, field := range []struct {
		name string
		out  any
	}{{"kind", &account.Kind}, {"status", &account.Status}, {"canRotateSecret", &account.CanRotateSecret}} {
		if value, found := fields[field.name]; found {
			if err := json.Unmarshal(value, field.out); err != nil {
				return consoleapi.SelfAccount{}, normalizeError(err)
			}
		}
	}
	// Values as well as fields are bounded to the public protocol. Unknown
	// future principal kinds never inherit the IAM user capability.
	switch account.Kind {
	case "root", "iam", "sts", "service", "directory":
	default:
		account.Kind = "unknown"
	}
	if account.Status != "enabled" && account.Status != "disabled" {
		account.Status = "unknown"
	}
	account.CanRotateSecret = account.CanRotateSecret && account.Kind == "iam" && account.Status == "enabled"
	return account, nil
}

func (c *Client) RotateOwnSecret(ctx context.Context, newSecret string) error {
	if len(newSecret) < 8 || len(newSecret) > 128 || !utf8.ValidString(newSecret) || strings.ContainsAny(newSecret, "\x00\r\n") {
		return invalidRequest()
	}
	account, err := c.SelfAccount(ctx)
	if err != nil {
		return err
	}
	if !account.CanRotateSecret {
		return &consoleapi.Error{Status: http.StatusNotImplemented, Code: "secret_rotation_unsupported", Message: "This identity or storage server does not support safe secret rotation."}
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil {
		return normalizeError(err)
	}
	data, err := json.Marshal(struct {
		NewSecretKey string `json:"newSecretKey"`
	}{newSecret})
	if err != nil {
		return invalidRequest()
	}
	encrypted, err := madmin.EncryptData(credential.SecretAccessKey, data)
	if err != nil {
		return normalizeError(err)
	}
	requestCtx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	resp, err := c.selfRequest(requestCtx, http.MethodPut, encrypted)
	if err != nil {
		return outcomeUnknown()
	}
	data, err = readBounded(resp.Body, maxManagementReply)
	if err != nil {
		return outcomeUnknown()
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return settingResponseError(resp.StatusCode, data, true, true)
	}
	if singleProtocolHeader(resp.Header, selfCapabilityHeader) != "v1" || (resp.StatusCode != http.StatusNoContent && resp.StatusCode != http.StatusOK) || len(data) != 0 {
		return outcomeUnknown()
	}
	return nil
}
