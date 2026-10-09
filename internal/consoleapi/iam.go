// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package consoleapi

import (
	"context"
	"encoding/json"
	"strings"
	"unicode/utf8"
)

// IAMBackend is optional. It uses only the startup-selected identity. Named
// policy bindings remain read-only until the server supports atomic replacement.
type IAMBackend interface {
	IAMUsers(context.Context) (IAMUsers, error)
	IAMGroups(context.Context) (IAMGroups, error)
	IAMServiceAccounts(context.Context, string) (IAMServiceAccounts, error)
	IAMPolicies(context.Context) (IAMPolicies, error)
	IAMBindings(context.Context, string, string) (IAMBindings, error)
	IAMAction(context.Context, IAMActionRequest) (IAMActionResult, error)
}

type IAMUser struct {
	AccessKey string   `json:"accessKey"`
	Status    string   `json:"status"`
	Policies  []string `json:"policies"`
	MemberOf  []string `json:"memberOf"`
	// ListUsers may omit membership entirely. Unknown must not imply empty;
	// destructive preflight requires the authoritative single-user endpoint.
	MemberOfKnown bool `json:"memberOfKnown"`
}

type IAMUsers struct {
	Users                  []IAMUser `json:"users"`
	PolicyBindingsWritable bool      `json:"policyBindingsWritable"`
}

type IAMGroup struct {
	Name     string   `json:"name"`
	Status   string   `json:"status"`
	Members  []string `json:"members"`
	Policies []string `json:"policies"`
}

type IAMGroups struct {
	Groups                 []IAMGroup `json:"groups"`
	PolicyBindingsWritable bool       `json:"policyBindingsWritable"`
}

type IAMServiceAccount struct {
	AccessKey     string `json:"accessKey"`
	ParentUser    string `json:"parentUser"`
	Status        string `json:"status"`
	ImpliedPolicy bool   `json:"impliedPolicy"`
	Policy        string `json:"policy"`
}

type IAMServiceAccounts struct {
	User            string              `json:"user"`
	ServiceAccounts []IAMServiceAccount `json:"serviceAccounts"`
}

type IAMPolicy struct {
	Name     string `json:"name"`
	Document string `json:"document"`
}

type IAMPolicies struct {
	Policies               []IAMPolicy `json:"policies"`
	PolicyBindingsWritable bool        `json:"policyBindingsWritable"`
}

type IAMBindings struct {
	Kind        string   `json:"kind"`
	Target      string   `json:"target"`
	Policies    []string `json:"policies"`
	Revision    string   `json:"revision"`
	Conditional bool     `json:"conditional"`
}

type IAMActionRequest struct {
	Action        string   `json:"action"`
	User          string   `json:"user,omitempty"`
	Group         string   `json:"group,omitempty"`
	Members       []string `json:"members,omitempty"`
	AccessKey     string   `json:"accessKey,omitempty"`
	SecretKey     string   `json:"secretKey,omitempty"`
	Policy        string   `json:"policy,omitempty"`
	Policies      []string `json:"policies,omitempty"`
	Revision      string   `json:"revision,omitempty"`
	ConfirmTarget string   `json:"confirmTarget"`
}

// IAMCredentials is returned only once, when OC generated a new credential.
// Existing and caller-supplied secrets must never appear in a response.
type IAMCredentials struct {
	AccessKey string `json:"accessKey"`
	SecretKey string `json:"secretKey"`
}

type IAMActionResult struct {
	Outcome         string          `json:"outcome"`
	RestartRequired bool            `json:"restartRequired"`
	Credentials     *IAMCredentials `json:"credentials,omitempty"`
}

func ValidIAMName(value string) bool {
	return len(value) >= 1 && len(value) <= 128 && utf8.ValidString(value) &&
		strings.TrimSpace(value) == value && !strings.ContainsAny(value, "\x00\r\n/\\")
}

func ValidIAMAccessKey(value string) bool { return len(value) >= 3 && ValidIAMName(value) }

func validIAMSecret(value string) bool {
	return len(value) >= 8 && len(value) <= 128 && utf8.ValidString(value) && !strings.ContainsAny(value, "\x00\r\n")
}

// ValidIAMAction rejects unrelated fields rather than silently ignoring them.
// This prevents a form intended to change only status from also changing keys.
func ValidIAMAction(a IAMActionRequest) bool {
	if a.Action == "user.policies" || a.Action == "group.policies" {
		target := a.User
		if a.Action == "group.policies" {
			target = a.Group
		}
		if !ValidIAMName(target) || a.ConfirmTarget != target || a.AccessKey != "" || a.SecretKey != "" || a.Policy != "" || len(a.Members) != 0 || (a.Action == "user.policies" && a.Group != "") || (a.Action == "group.policies" && a.User != "") || len(a.Policies) > 100 || len(a.Revision) != 64 {
			return false
		}
		for _, char := range a.Revision {
			if !(char >= 'a' && char <= 'f') && !(char >= '0' && char <= '9') {
				return false
			}
		}
		seen := map[string]bool{}
		for _, policy := range a.Policies {
			if !ValidIAMName(policy) || strings.ContainsRune(policy, ',') || seen[policy] {
				return false
			}
			seen[policy] = true
		}
		return a.Policies != nil
	}
	if len(a.Policies) != 0 || a.Revision != "" {
		return false
	}
	if a.Policy != "" {
		var document map[string]json.RawMessage
		if len(a.Policy) > 20*1024 || !utf8.ValidString(a.Policy) || json.Unmarshal([]byte(a.Policy), &document) != nil || document == nil {
			return false
		}
	}
	if a.SecretKey != "" && !validIAMSecret(a.SecretKey) {
		return false
	}
	switch a.Action {
	case "user.create", "user.enable", "user.disable", "user.rotate", "user.delete":
		if !ValidIAMAccessKey(a.User) || a.ConfirmTarget != a.User || a.Group != "" || a.AccessKey != "" || a.Policy != "" || len(a.Members) != 0 {
			return false
		}
		return a.Action == "user.create" || (a.Action == "user.rotate" && a.SecretKey != "") || (a.Action != "user.rotate" && a.SecretKey == "")
	case "group.create", "group.add-members", "group.remove-members", "group.enable", "group.disable", "group.delete":
		if !ValidIAMName(a.Group) || a.ConfirmTarget != a.Group || a.User != "" || a.AccessKey != "" || a.SecretKey != "" || a.Policy != "" || len(a.Members) > 100 {
			return false
		}
		if (a.Action == "group.add-members" || a.Action == "group.remove-members") && len(a.Members) == 0 {
			return false
		}
		if a.Action != "group.create" && a.Action != "group.add-members" && a.Action != "group.remove-members" && len(a.Members) != 0 {
			return false
		}
		seen := make(map[string]bool, len(a.Members))
		for _, member := range a.Members {
			if !ValidIAMAccessKey(member) || seen[member] {
				return false
			}
			seen[member] = true
		}
		return true
	case "service-account.create":
		return ValidIAMAccessKey(a.User) && a.ConfirmTarget == a.User && a.Group == "" && len(a.Members) == 0 && (a.AccessKey == "" || ValidIAMAccessKey(a.AccessKey)) && (a.SecretKey == "" || a.AccessKey != "")
	case "service-account.enable", "service-account.disable", "service-account.rotate", "service-account.policy", "service-account.delete":
		if !ValidIAMAccessKey(a.AccessKey) || a.ConfirmTarget != a.AccessKey || a.User != "" || a.Group != "" || len(a.Members) != 0 {
			return false
		}
		if a.Action == "service-account.rotate" {
			return a.SecretKey != "" && a.Policy == ""
		}
		if a.Action == "service-account.policy" {
			return a.Policy != "" && a.SecretKey == ""
		}
		return a.SecretKey == "" && a.Policy == ""
	default:
		return false
	}
}
