// Copyright 2026 soulteary. Licensed under the Apache License, Version 2.0.

package console

import (
	"net/http"
	"strings"
	"unicode/utf8"

	"github.com/soulteary/mc/internal/consoleapi"
)

func validNativeCredentials(c NativeCredentials) bool {
	return consoleapi.ValidIAMAccessKey(c.AccessKey) && len(c.SecretKey) >= 8 && len(c.SecretKey) <= 128 && utf8.ValidString(c.SecretKey) && !strings.ContainsAny(c.SecretKey, "\x00\r\n")
}
func (s *Server) cookieName() string {
	if s.nativeLogin != nil {
		return nativeCookieName
	}
	return cookieName
}
func (s *Server) sessionCookie(token string, maxAge int) *http.Cookie {
	cookie := &http.Cookie{Name: s.cookieName(), Value: token, Path: "/", MaxAge: maxAge, HttpOnly: true, SameSite: http.SameSiteStrictMode}
	if s.nativeLogin != nil {
		cookie.Secure = true
		cookie.SameSite = http.SameSiteLaxMode
	}
	return cookie
}

func (s *Server) loginPrompt() string {
	if s.nativeLogin != nil {
		return "Sign in with your IAM credentials."
	}
	return "Sign in with the code printed by OC."
}
