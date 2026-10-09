// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package storageclient

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"net/url"
	"sort"
	"strings"

	"github.com/soulteary/mc/internal/clienttransport"
	"github.com/soulteary/mc/internal/consoleapi"
	"github.com/soulteary/otterio-sdk/v7/pkg/credentials"
	"github.com/soulteary/otterio-sdk/v7/pkg/signer"
	iampolicy "github.com/soulteary/otterio/pkg/iam/policy"
	"github.com/soulteary/otterio/pkg/madmin"
)

var _ consoleapi.IAMBackend = (*Client)(nil)

const maxIAMReply = 4 << 20

func iamUnsupported() *consoleapi.Error {
	return &consoleapi.Error{Status: 501, Code: "iam_unsupported", Message: "This identity or storage server does not support native IAM management."}
}

func bindingUnsupported() *consoleapi.Error {
	return &consoleapi.Error{Status: 501, Code: "policy_bindings_unsupported", Message: "This server cannot safely replace policy bindings. View bindings here and update them with an administrative client until conditional updates are supported."}
}

func iamConflict(code, message string) *consoleapi.Error {
	return &consoleapi.Error{Status: 409, Code: code, Message: message}
}

func iamError(err error) *consoleapi.Error {
	code := madmin.ToErrorResponse(err).Code
	switch code {
	case "XOtterioAdminNoSuchUser", "XOtterioAdminNoSuchGroup", "XOtterioAdminNoSuchPolicy", "XMinioAdminNoSuchUser", "XMinioAdminNoSuchGroup", "XMinioAdminNoSuchPolicy":
		return &consoleapi.Error{Status: 404, Code: "iam_not_found", Message: "The IAM user, group or policy could not be found. Reload the list."}
	case "XOtterioAdminGroupNotEmpty", "XMinioAdminGroupNotEmpty":
		return iamConflict("group_not_empty", "Remove every member before deleting this group.")
	case "XOtterioInvalidIAMCredentials", "XMinioInvalidIAMCredentials", "XOtterioAdminActionNotAllowed", "XMinioAdminActionNotAllowed":
		return &consoleapi.Error{Status: 403, Code: "iam_action_denied", Message: "This identity cannot manage the selected IAM target. Root, temporary and external identities have additional restrictions."}
	case "XOtterioAdminInvalidArgument", "XOtterioAdminInvalidAccessKey", "XOtterioAdminInvalidSecretKey", "XOtterioAdminConfigBadJSON", "XMinioAdminInvalidArgument", "XMinioAdminInvalidAccessKey", "XMinioAdminInvalidSecretKey", "XMinioAdminConfigBadJSON":
		return invalidRequest()
	}
	return normalizeError(err)
}

// A request owns this observer and SDK client. The SDK normally retries IAM
// writes, including credential creation. Send at most one mutation, and retain
// the first response to distinguish a conclusive rejection from a lost result.
type iamTransport struct {
	base       http.RoundTripper
	dispatched bool
	write      bool
	status     int
	data       []byte
	readErr    error
	allowBody  bool
}

func (t *iamTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.write && t.dispatched {
		return nil, context.Canceled
	}
	if err := req.Context().Err(); err != nil {
		return nil, err
	}
	t.dispatched = true
	resp, err := t.base.RoundTrip(clienttransport.NormalizeAdminRequest(req))
	if resp == nil || err != nil {
		return resp, err
	}
	t.status = resp.StatusCode
	if resp.Body == nil {
		t.readErr = errors.New("missing IAM response body")
		return nil, t.readErr
	}
	t.data, t.readErr = readBounded(resp.Body, maxIAMReply)
	if t.readErr != nil {
		return nil, t.readErr
	}
	resp.Body = io.NopCloser(bytes.NewReader(t.data))
	return clienttransport.NormalizeAdminResponse(resp, nil)
}

func (t *iamTransport) mutationError(err error) error {
	if err == nil && t.dispatched && t.status >= 200 && t.status < 300 && t.readErr == nil && (t.allowBody || len(t.data) == 0) {
		return nil
	}
	if !t.dispatched {
		return iamError(err)
	}
	if t.readErr != nil || t.status == 0 || (t.status >= 500 && t.status != 501) || (t.status >= 300 && t.status < 400) || t.status < 400 {
		return outcomeUnknown()
	}
	code, parseErr := managementErrorCode(t.data, true)
	if parseErr != nil {
		return outcomeUnknown()
	}
	if code == "CredentialConflict" {
		return outcomeUnknown()
	}
	responseErr := madmin.ErrorResponse{Code: code}
	normalized := iamError(responseErr)
	if normalized.Code == "UpstreamError" || (normalized.Status != t.status && !(normalized.Code == "AccessDenied" && t.status == 401) && !(normalized.Code == "group_not_empty" && t.status == 400)) {
		return outcomeUnknown()
	}
	return normalized
}

func (c *Client) iamClient(ctx context.Context, write bool) (*madmin.AdminClient, *iamTransport, credentials.Value, error) {
	if err := c.ready(ctx); err != nil {
		return nil, nil, credentials.Value{}, err
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return nil, nil, credential, iamUnsupported()
	}
	u, err := url.Parse(c.adminEndpoint)
	if err != nil {
		return nil, nil, credential, configError()
	}
	admin, err := madmin.NewWithOptions(u.Host, &madmin.Options{Creds: credentials.NewStaticV4(credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken), Secure: u.Scheme == "https"})
	if err != nil {
		return nil, nil, credential, configError()
	}
	transport := &iamTransport{base: c.adminTransport, write: write}
	admin.SetCustomTransport(transport)
	admin.SetAppInfo(c.appName, c.appVersion)
	return admin, transport, credential, nil
}

func names(values []string) []string {
	result := append([]string{}, values...)
	sort.Strings(result)
	return result
}

func policyNames(value string) []string {
	result := []string{}
	for _, name := range strings.Split(value, ",") {
		if name = strings.TrimSpace(name); name != "" {
			result = append(result, name)
		}
	}
	return names(result)
}

func (c *Client) IAMUsers(ctx context.Context) (consoleapi.IAMUsers, error) {
	result := consoleapi.IAMUsers{Users: []consoleapi.IAMUser{}}
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	admin, _, _, err := c.iamClient(ctx, false)
	if err != nil {
		return result, err
	}
	users, err := admin.ListUsers(ctx)
	if err != nil {
		return result, iamError(err)
	}
	for accessKey, info := range users {
		// UserInfo contains a secret field. Never serialize or retain it in
		// the browser DTO, including when a legacy server returns it.
		var memberships []string
		known := info.MemberOf != nil
		if known {
			memberships = names(info.MemberOf)
		}
		result.Users = append(result.Users, consoleapi.IAMUser{AccessKey: accessKey, Status: string(info.Status), Policies: policyNames(info.PolicyName), MemberOf: memberships, MemberOfKnown: known})
	}
	sort.Slice(result.Users, func(i, j int) bool { return result.Users[i].AccessKey < result.Users[j].AccessKey })
	return result, nil
}

func (c *Client) IAMGroups(ctx context.Context) (consoleapi.IAMGroups, error) {
	result := consoleapi.IAMGroups{Groups: []consoleapi.IAMGroup{}}
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	admin, _, _, err := c.iamClient(ctx, false)
	if err != nil {
		return result, err
	}
	groups, err := admin.ListGroups(ctx)
	if err != nil {
		return result, iamError(err)
	}
	if len(groups) > 1000 {
		return result, &consoleapi.Error{Status: 413, Code: "iam_limit", Message: "The server returned too many IAM groups for one console view."}
	}
	for _, name := range names(groups) {
		info, err := admin.GetGroupDescription(ctx, name)
		if err != nil {
			return result, iamError(err)
		}
		result.Groups = append(result.Groups, consoleapi.IAMGroup{Name: name, Status: info.Status, Members: names(info.Members), Policies: policyNames(info.Policy)})
	}
	return result, nil
}

func (c *Client) IAMServiceAccounts(ctx context.Context, user string) (consoleapi.IAMServiceAccounts, error) {
	result := consoleapi.IAMServiceAccounts{User: user, ServiceAccounts: []consoleapi.IAMServiceAccount{}}
	if !consoleapi.ValidIAMAccessKey(user) {
		return result, invalidRequest()
	}
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	admin, _, _, err := c.iamClient(ctx, false)
	if err != nil {
		return result, err
	}
	accounts, err := admin.ListServiceAccounts(ctx, user)
	if err != nil {
		return result, iamError(err)
	}
	if len(accounts.Accounts) > 1000 {
		return result, &consoleapi.Error{Status: 413, Code: "iam_limit", Message: "The server returned too many service accounts for one console view."}
	}
	for _, accessKey := range names(accounts.Accounts) {
		info, err := admin.InfoServiceAccount(ctx, accessKey)
		if err != nil {
			return result, iamError(err)
		}
		if info.ParentUser != user {
			return result, iamError(errors.New("inconsistent IAM parent"))
		}
		result.ServiceAccounts = append(result.ServiceAccounts, consoleapi.IAMServiceAccount{AccessKey: accessKey, ParentUser: info.ParentUser, Status: info.AccountStatus, ImpliedPolicy: info.ImpliedPolicy, Policy: info.Policy})
	}
	return result, nil
}

func (c *Client) IAMPolicies(ctx context.Context) (consoleapi.IAMPolicies, error) {
	result := consoleapi.IAMPolicies{Policies: []consoleapi.IAMPolicy{}}
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	admin, _, _, err := c.iamClient(ctx, false)
	if err != nil {
		return result, err
	}
	policies, err := admin.ListCannedPolicies(ctx)
	if err != nil {
		return result, iamError(err)
	}
	for name, policy := range policies {
		data, err := json.MarshalIndent(policy, "", "  ")
		if err != nil {
			return result, iamError(err)
		}
		result.Policies = append(result.Policies, consoleapi.IAMPolicy{Name: name, Document: string(data)})
	}
	sort.Slice(result.Policies, func(i, j int) bool { return result.Policies[i].Name < result.Policies[j].Name })
	return result, nil
}

func randomIAMSecret() (string, error) {
	var value [20]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", err
	}
	return hex.EncodeToString(value[:]), nil
}

func (c *Client) IAMAction(ctx context.Context, args consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
	result := consoleapi.IAMActionResult{Outcome: "confirmed"}
	if args.Action == "user.policies" || args.Action == "group.policies" {
		return c.replaceIAMBindings(ctx, args)
	}
	if strings.Contains(args.Action, "policies") || len(args.Policies) != 0 {
		return result, bindingUnsupported()
	}
	if !consoleapi.ValidIAMAction(args) {
		return result, invalidRequest()
	}
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	admin, _, credential, err := c.iamClient(ctx, false)
	if err != nil {
		return result, err
	}
	account, err := c.SelfAccount(ctx)
	if err != nil {
		return result, err
	}
	if account.Kind != "root" && account.Kind != "iam" && account.Kind != "service" {
		return result, iamUnsupported()
	}
	if account.Kind == "root" && (args.User == credential.AccessKeyID || args.AccessKey == credential.AccessKeyID) {
		return result, &consoleapi.Error{Status: 403, Code: "immutable_root", Message: "Root credentials are configured by the storage server and cannot be changed here."}
	}
	var policy *iampolicy.Policy
	if args.Policy != "" {
		policy, err = iampolicy.ParseConfig(strings.NewReader(args.Policy))
		if err != nil {
			return result, invalidRequest()
		}
	}
	// Preflight checks are reads, never permission grants. Every mutation is
	// independently authorized by the storage server using the same identity.
	var userInfo madmin.UserInfo
	var groupInfo *madmin.GroupDesc
	if strings.HasPrefix(args.Action, "user.") {
		users, listErr := admin.ListUsers(ctx)
		if listErr != nil {
			return result, iamError(listErr)
		}
		var exists bool
		userInfo, exists = users[args.User]
		if args.Action == "user.create" && exists {
			return result, iamConflict("user_exists", "This user already exists. Choose another access key.")
		}
		if args.Action != "user.create" && !exists {
			return result, &consoleapi.Error{Status: 404, Code: "iam_not_found", Message: "Only a listed native IAM user can be changed here."}
		}
		result.RestartRequired = args.User == credential.AccessKeyID
		if args.Action == "user.delete" {
			// ListUsers omits MemberOf on current native servers. Deletion
			// must read the single-user endpoint rather than treating omission
			// as an empty group membership list.
			userInfo, err = admin.GetUserInfo(ctx, args.User)
			if err != nil {
				return result, iamError(err)
			}
			if len(userInfo.MemberOf) != 0 {
				return result, iamConflict("user_has_groups", "Remove this user from every group before deleting it. User deletion also revokes temporary credentials.")
			}
			accounts, listErr := admin.ListServiceAccounts(ctx, args.User)
			if listErr != nil {
				return result, iamError(listErr)
			}
			if len(accounts.Accounts) != 0 {
				return result, iamConflict("user_has_service_accounts", "Delete this user's service accounts first. User deletion also revokes temporary credentials and group memberships.")
			}
		}
	}
	if strings.HasPrefix(args.Action, "group.") {
		groups, listErr := admin.ListGroups(ctx)
		if listErr != nil {
			return result, iamError(listErr)
		}
		exists := false
		for _, group := range groups {
			exists = exists || group == args.Group
		}
		if args.Action == "group.create" && exists {
			return result, iamConflict("group_exists", "This group already exists. Choose another name.")
		}
		if args.Action != "group.create" && !exists {
			return result, &consoleapi.Error{Status: 404, Code: "iam_not_found", Message: "The selected group no longer exists."}
		}
		if exists {
			groupInfo, err = admin.GetGroupDescription(ctx, args.Group)
			if err != nil {
				return result, iamError(err)
			}
			for _, member := range groupInfo.Members {
				result.RestartRequired = result.RestartRequired || member == credential.AccessKeyID
			}
			if args.Action == "group.delete" && len(groupInfo.Members) != 0 {
				return result, iamConflict("group_not_empty", "Remove every member before deleting this group.")
			}
		}
		for _, member := range args.Members {
			result.RestartRequired = result.RestartRequired || member == credential.AccessKeyID
		}
	}
	if strings.HasPrefix(args.Action, "service-account.") && args.Action != "service-account.create" {
		info, infoErr := admin.InfoServiceAccount(ctx, args.AccessKey)
		if infoErr != nil {
			return result, iamError(infoErr)
		}
		if info.ParentUser == "" {
			return result, iamUnsupported()
		}
		result.RestartRequired = args.AccessKey == credential.AccessKeyID
	}
	// A service account inherits its parent's user and group permissions.
	// Retire its session conservatively after any successful user/group edit.
	if account.Kind == "service" && (strings.HasPrefix(args.Action, "user.") || strings.HasPrefix(args.Action, "group.")) {
		result.RestartRequired = true
	}
	generated := false
	if args.Action == "user.create" && args.SecretKey == "" {
		args.SecretKey, err = randomIAMSecret()
		if err != nil {
			return result, iamError(err)
		}
		generated = true
	}
	if args.Action == "user.create" || args.Action == "group.create" {
		return c.createNativeIAM(ctx, args, result, generated)
	}
	if args.Action == "user.rotate" {
		return c.rotateNativeIAMSecret(ctx, args, result)
	}
	mutationClient, observer, _, err := c.iamClient(ctx, true)
	if err != nil {
		return result, err
	}
	observer.allowBody = args.Action == "service-account.create"
	switch args.Action {
	case "user.enable", "user.disable":
		status := madmin.AccountEnabled
		if args.Action == "user.disable" {
			status = madmin.AccountDisabled
		}
		err = mutationClient.SetUserStatus(ctx, args.User, status)
	case "user.delete":
		err = mutationClient.RemoveUser(ctx, args.User)
	case "group.add-members", "group.remove-members", "group.delete":
		err = mutationClient.UpdateGroupMembers(ctx, madmin.GroupAddRemove{Group: args.Group, Members: args.Members, IsRemove: args.Action == "group.remove-members" || args.Action == "group.delete"})
	case "group.enable", "group.disable":
		status := madmin.GroupEnabled
		if args.Action == "group.disable" {
			status = madmin.GroupDisabled
		}
		err = mutationClient.SetGroupStatus(ctx, args.Group, status)
	case "service-account.create":
		created, createErr := mutationClient.AddServiceAccount(ctx, madmin.AddServiceAccountReq{TargetUser: args.User, AccessKey: args.AccessKey, SecretKey: args.SecretKey, Policy: policy})
		err = createErr
		if err == nil {
			if !consoleapi.ValidIAMAccessKey(created.AccessKey) || len(created.SecretKey) < 8 || (args.AccessKey != "" && args.AccessKey != created.AccessKey) || (args.SecretKey != "" && args.SecretKey != created.SecretKey) || (created.ParentUser != "" && created.ParentUser != args.User) {
				err = errors.New("invalid created IAM credential")
			} else if args.SecretKey == "" {
				result.Credentials = &consoleapi.IAMCredentials{AccessKey: created.AccessKey, SecretKey: created.SecretKey}
			}
		}
	case "service-account.enable", "service-account.disable", "service-account.rotate", "service-account.policy":
		opts := madmin.UpdateServiceAccountReq{NewSecretKey: args.SecretKey, NewPolicy: policy}
		if args.Action == "service-account.enable" {
			opts.NewStatus = "on"
		} else if args.Action == "service-account.disable" {
			opts.NewStatus = "off"
		}
		err = mutationClient.UpdateServiceAccount(ctx, args.AccessKey, opts)
	case "service-account.delete":
		err = mutationClient.DeleteServiceAccount(ctx, args.AccessKey)
	}
	args.SecretKey = ""
	if err = observer.mutationError(err); err != nil {
		result.Credentials = nil
		return result, err
	}
	return result, nil
}

func (c *Client) nativeIAMRequest(ctx context.Context, route, method, kind, target string, data []byte) (*http.Response, error) {
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return nil, iamUnsupported()
	}
	u, err := url.Parse(c.adminEndpoint + "/otterio/admin/v3/" + route)
	if err != nil {
		return nil, configError()
	}
	u.RawQuery = url.Values{"kind": {kind}, "target": {target}}.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, invalidRequest()
	}
	req.Header.Set("Content-Type", "application/octet-stream")
	req.Header.Set("User-Agent", c.appName+"/"+c.appVersion)
	setPayloadHash(req, data)
	req = signer.SignV4(*req, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, "")
	return c.adminTransport.RoundTrip(req)
}

func (c *Client) createNativeIAM(ctx context.Context, args consoleapi.IAMActionRequest, result consoleapi.IAMActionResult, generated bool) (consoleapi.IAMActionResult, error) {
	kind, target := "user", args.User
	if args.Action == "group.create" {
		kind, target = "group", args.Group
	}
	resp, err := c.nativeIAMRequest(ctx, "iam-create", http.MethodGet, kind, target, nil)
	if err != nil {
		return result, iamError(err)
	}
	if singleProtocolHeader(resp.Header, "X-Otterio-IAM-Create") != "v1" && (resp.StatusCode == 200 || resp.StatusCode == 400 || resp.StatusCode == 404 || resp.StatusCode == 501) {
		_ = resp.Body.Close()
		return result, &consoleapi.Error{Status: 501, Code: "iam_create_unsupported", Message: "This server cannot safely create a new user or group without overwriting an existing target. Upgrade its IAM create-only protocol before using this action."}
	}
	data, err := readBounded(resp.Body, maxSelfReply)
	if err != nil {
		return result, iamError(err)
	}
	if resp.StatusCode != 200 {
		code, err := managementErrorCode(data, true)
		if err != nil {
			return result, iamError(err)
		}
		return result, iamError(madmin.ErrorResponse{Code: code})
	}
	fields, err := managementJSONObject(data)
	var gotKind, gotTarget string
	var createOnly bool
	if err != nil || json.Unmarshal(fields["kind"], &gotKind) != nil || json.Unmarshal(fields["target"], &gotTarget) != nil || json.Unmarshal(fields["createOnly"], &createOnly) != nil || gotKind != kind || gotTarget != target || !createOnly || singleProtocolHeader(resp.Header, "X-Otterio-IAM-Create") != "v1" {
		return result, iamError(errors.New("invalid IAM create capability"))
	}
	data, err = json.Marshal(struct {
		SecretKey string   `json:"secretKey,omitempty"`
		Members   []string `json:"members,omitempty"`
	}{args.SecretKey, args.Members})
	if err != nil {
		return result, invalidRequest()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil {
		return result, iamError(err)
	}
	data, err = madmin.EncryptData(credential.SecretAccessKey, data)
	if err != nil {
		return result, iamError(err)
	}
	resp, err = c.nativeIAMRequest(ctx, "iam-create", http.MethodPut, kind, target, data)
	if err != nil {
		return result, outcomeUnknown()
	}
	data, err = readBounded(resp.Body, maxManagementReply)
	if err != nil {
		return result, outcomeUnknown()
	}
	if resp.StatusCode == 412 {
		code, err := managementErrorCode(data, true)
		if err == nil && code == "PreconditionFailed" {
			return result, iamConflict("iam_exists", "Another client created this IAM target. Reload the list before choosing another name.")
		}
	}
	observer := iamTransport{dispatched: true, status: resp.StatusCode, data: data}
	if err := observer.mutationError(nil); err != nil {
		return result, err
	}
	if singleProtocolHeader(resp.Header, "X-Otterio-IAM-Create") != "v1" || (resp.StatusCode != 200 && resp.StatusCode != 204) {
		return result, outcomeUnknown()
	}
	if generated {
		result.Credentials = &consoleapi.IAMCredentials{AccessKey: args.User, SecretKey: args.SecretKey}
	}
	args.SecretKey = ""
	return result, nil
}

func (c *Client) rotateNativeIAMSecret(ctx context.Context, args consoleapi.IAMActionRequest, result consoleapi.IAMActionResult) (consoleapi.IAMActionResult, error) {
	resp, err := c.nativeIAMRequest(ctx, "iam-user-secret", http.MethodGet, "user", args.User, nil)
	if err != nil {
		return result, iamError(err)
	}
	if singleProtocolHeader(resp.Header, "X-Otterio-IAM-User-Secret") != "v1" && (resp.StatusCode == 200 || resp.StatusCode == 400 || resp.StatusCode == 404 || resp.StatusCode == 501) {
		_ = resp.Body.Close()
		return result, &consoleapi.Error{Status: 501, Code: "iam_secret_unsupported", Message: "This server cannot safely rotate another user's secret without changing its status. Upgrade its secret-only IAM protocol before using this action."}
	}
	data, err := readBounded(resp.Body, maxSelfReply)
	if err != nil {
		return result, iamError(err)
	}
	if resp.StatusCode != 200 {
		code, err := managementErrorCode(data, true)
		if err != nil {
			return result, iamError(err)
		}
		return result, iamError(madmin.ErrorResponse{Code: code})
	}
	fields, err := managementJSONObject(data)
	var kind, target string
	var secretOnly bool
	if err != nil || json.Unmarshal(fields["kind"], &kind) != nil || json.Unmarshal(fields["target"], &target) != nil || json.Unmarshal(fields["secretOnly"], &secretOnly) != nil || kind != "user" || target != args.User || !secretOnly || singleProtocolHeader(resp.Header, "X-Otterio-IAM-User-Secret") != "v1" {
		return result, iamError(errors.New("invalid secret-only IAM capability"))
	}
	data, err = json.Marshal(struct {
		NewSecretKey string `json:"newSecretKey"`
	}{args.SecretKey})
	if err != nil {
		return result, invalidRequest()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil {
		return result, iamError(err)
	}
	data, err = madmin.EncryptData(credential.SecretAccessKey, data)
	if err != nil {
		return result, iamError(err)
	}
	resp, err = c.nativeIAMRequest(ctx, "iam-user-secret", http.MethodPut, "user", args.User, data)
	if err != nil {
		return result, outcomeUnknown()
	}
	data, err = readBounded(resp.Body, maxManagementReply)
	if err != nil {
		return result, outcomeUnknown()
	}
	observer := iamTransport{dispatched: true, status: resp.StatusCode, data: data}
	if err := observer.mutationError(nil); err != nil {
		return result, err
	}
	if singleProtocolHeader(resp.Header, "X-Otterio-IAM-User-Secret") != "v1" || (resp.StatusCode != 200 && resp.StatusCode != 204) {
		return result, outcomeUnknown()
	}
	args.SecretKey = ""
	return result, nil
}

const iamBindingsCapability = "X-Otterio-IAM-Bindings"
const iamBindingsRevision = "X-Otterio-IAM-Revision"
const iamBindingsCondition = "X-Otterio-IAM-If-Match"

func (c *Client) iamBindingsRequest(ctx context.Context, method, kind, target, revision string, data []byte) (*http.Response, error) {
	credential, err := c.selectedCredentials(ctx)
	if err != nil || credential.SignerType != credentials.SignatureV4 {
		return nil, iamUnsupported()
	}
	u, err := url.Parse(c.adminEndpoint + "/otterio/admin/v3/iam-policy-bindings")
	if err != nil {
		return nil, configError()
	}
	u.RawQuery = url.Values{"kind": {kind}, "target": {target}}.Encode()
	req, err := http.NewRequestWithContext(ctx, method, u.String(), bytes.NewReader(data))
	if err != nil {
		return nil, invalidRequest()
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", c.appName+"/"+c.appVersion)
	if revision != "" {
		req.Header.Set(iamBindingsCondition, revision)
	}
	setPayloadHash(req, data)
	req = signer.SignV4(*req, credential.AccessKeyID, credential.SecretAccessKey, credential.SessionToken, "")
	return c.adminTransport.RoundTrip(req)
}

func parseIAMBindings(resp *http.Response, data []byte, kind, target string) (consoleapi.IAMBindings, error) {
	result := consoleapi.IAMBindings{}
	fields, err := managementJSONObject(data)
	if err != nil {
		return result, err
	}
	for _, field := range []struct {
		name string
		out  any
	}{{"kind", &result.Kind}, {"target", &result.Target}, {"policies", &result.Policies}, {"revision", &result.Revision}, {"conditional", &result.Conditional}} {
		value, found := fields[field.name]
		if !found || json.Unmarshal(value, field.out) != nil {
			return consoleapi.IAMBindings{}, errors.New("invalid IAM binding field")
		}
	}
	if result.Kind != kind || result.Target != target || !result.Conditional || result.Policies == nil || !validRevision(result.Revision) || singleProtocolHeader(resp.Header, iamBindingsCapability) != "v1" || singleProtocolHeader(resp.Header, iamBindingsRevision) != result.Revision {
		return consoleapi.IAMBindings{}, errors.New("invalid IAM binding response")
	}
	seen := map[string]bool{}
	if len(result.Policies) > 100 {
		return consoleapi.IAMBindings{}, errors.New("too many IAM policies")
	}
	for _, policy := range result.Policies {
		if !consoleapi.ValidIAMName(policy) || strings.ContainsRune(policy, ',') || seen[policy] {
			return consoleapi.IAMBindings{}, errors.New("invalid IAM policy binding")
		}
		seen[policy] = true
	}
	result.Policies = names(result.Policies)
	return result, nil
}

func (c *Client) IAMBindings(ctx context.Context, kind, target string) (consoleapi.IAMBindings, error) {
	result := consoleapi.IAMBindings{Kind: kind, Target: target, Policies: []string{}}
	if (kind != "user" && kind != "group") || !consoleapi.ValidIAMName(target) {
		return result, invalidRequest()
	}
	if err := c.ready(ctx); err != nil {
		return result, err
	}
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	resp, err := c.iamBindingsRequest(ctx, http.MethodGet, kind, target, "", nil)
	if err != nil {
		return result, iamError(err)
	}
	// Legacy servers can route an unknown admin path to a browser/S3 error.
	// Never accept that reply as a write capability; fetch the native value.
	if singleProtocolHeader(resp.Header, iamBindingsCapability) != "v1" && (resp.StatusCode == 200 || resp.StatusCode == 400 || resp.StatusCode == 404 || resp.StatusCode == 501) {
		_ = resp.Body.Close()
		admin, _, _, err := c.iamClient(ctx, false)
		if err != nil {
			return result, err
		}
		if kind == "user" {
			info, readErr := admin.GetUserInfo(ctx, target)
			if readErr != nil {
				return result, iamError(readErr)
			}
			result.Policies = policyNames(info.PolicyName)
		} else {
			info, readErr := admin.GetGroupDescription(ctx, target)
			if readErr != nil {
				return result, iamError(readErr)
			}
			result.Policies = policyNames(info.Policy)
		}
		return result, nil
	}
	data, err := readBounded(resp.Body, maxManagementReply)
	if err != nil {
		return result, iamError(err)
	}
	if resp.StatusCode != 200 {
		code, parseErr := managementErrorCode(data, true)
		if parseErr != nil {
			return result, iamError(parseErr)
		}
		return result, iamError(madmin.ErrorResponse{Code: code})
	}
	parsed, err := parseIAMBindings(resp, data, kind, target)
	if err != nil {
		return result, iamError(err)
	}
	return parsed, nil
}

func (c *Client) replaceIAMBindings(ctx context.Context, args consoleapi.IAMActionRequest) (consoleapi.IAMActionResult, error) {
	result := consoleapi.IAMActionResult{Outcome: "confirmed"}
	if !consoleapi.ValidIAMAction(args) {
		return result, invalidRequest()
	}
	kind, target := "user", args.User
	if args.Action == "group.policies" {
		kind, target = "group", args.Group
	}
	current, err := c.IAMBindings(ctx, kind, target)
	if err != nil {
		return result, err
	}
	if !current.Conditional {
		return result, bindingUnsupported()
	}
	if current.Revision != args.Revision {
		return result, iamConflict("binding_conflict", "These direct policy bindings changed. Reload them before editing again.")
	}
	account, err := c.SelfAccount(ctx)
	if err != nil {
		return result, err
	}
	if account.Kind != "root" && account.Kind != "iam" && account.Kind != "service" {
		return result, iamUnsupported()
	}
	credential, err := c.selectedCredentials(ctx)
	if err != nil {
		return result, iamError(err)
	}
	if account.Kind == "root" && kind == "user" && target == credential.AccessKeyID {
		return result, &consoleapi.Error{Status: 403, Code: "immutable_root", Message: "Root policy bindings cannot be changed here."}
	}
	result.RestartRequired = kind == "user" && target == credential.AccessKeyID
	if account.Kind == "service" {
		result.RestartRequired = true
	}
	if kind == "group" && account.Kind == "iam" {
		admin, _, _, err := c.iamClient(ctx, false)
		if err != nil {
			return result, err
		}
		info, err := admin.GetGroupDescription(ctx, target)
		if err != nil {
			return result, iamError(err)
		}
		for _, member := range info.Members {
			result.RestartRequired = result.RestartRequired || member == credential.AccessKeyID
		}
	}
	data, err := json.Marshal(struct {
		Policies []string `json:"policies"`
	}{args.Policies})
	if err != nil {
		return result, invalidRequest()
	}
	ctx, cancel := context.WithTimeout(ctx, c.metadataTimeout)
	defer cancel()
	resp, err := c.iamBindingsRequest(ctx, http.MethodPut, kind, target, args.Revision, data)
	if err != nil {
		return result, outcomeUnknown()
	}
	data, err = readBounded(resp.Body, maxManagementReply)
	if err != nil {
		return result, outcomeUnknown()
	}
	if resp.StatusCode != 200 {
		if resp.StatusCode == 412 {
			code, err := managementErrorCode(data, true)
			if err == nil && code == "PreconditionFailed" {
				return result, iamConflict("binding_conflict", "These direct policy bindings changed. Reload them before editing again.")
			}
		}
		observer := iamTransport{dispatched: true, status: resp.StatusCode, data: data}
		return result, observer.mutationError(errors.New("IAM binding rejected"))
	}
	saved, err := parseIAMBindings(resp, data, kind, target)
	if err != nil {
		return result, outcomeUnknown()
	}
	want := names(args.Policies)
	if len(saved.Policies) != len(want) {
		return result, outcomeUnknown()
	}
	for i := range want {
		if saved.Policies[i] != want[i] {
			return result, outcomeUnknown()
		}
	}
	return result, nil
}
