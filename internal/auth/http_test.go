package auth

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authkit "github.com/supercakecrumb/msgr-authkit"
)

type fakeRedeemer struct {
	session authkit.WebSession
	err     error
	token   string
}

func (f *fakeRedeemer) RedeemLoginLink(_ context.Context, input authkit.RedeemLoginLinkInput) (authkit.WebSession, error) {
	f.token = input.LinkToken
	return f.session, f.err
}

type fakeSessions struct {
	session   authkit.WebSession
	err       error
	revoked   string
	revokeErr error
}

func (f *fakeSessions) Validate(_ context.Context, token string) (authkit.WebSession, error) {
	if f.err != nil || token != f.session.Token {
		if f.err != nil {
			return authkit.WebSession{}, f.err
		}
		return authkit.WebSession{}, authkit.ErrSessionNotFound
	}
	return f.session, nil
}

func (f *fakeSessions) Revoke(_ context.Context, token string) error {
	f.revoked = token
	return f.revokeErr
}

func newTestHandler(t *testing.T) (*Handler, *fakeRedeemer, *fakeSessions) {
	return newTestHandlerAt(t, "https://atlas.aurorass.art")
}

func newTestHandlerAt(t *testing.T, publicBaseURL string) (*Handler, *fakeRedeemer, *fakeSessions) {
	t.Helper()
	session := authkit.WebSession{
		SessionID: "session-id",
		SubjectID: "42",
		Token:     "session-token",
		IssuedAt:  time.Now().Add(-time.Minute),
		ExpiresAt: time.Now().Add(time.Hour),
	}
	redeemer := &fakeRedeemer{session: session}
	sessions := &fakeSessions{session: session}
	handler, err := NewHandler(redeemer, sessions, sessions, []byte(strings.Repeat("k", 32)), publicBaseURL, "42")
	if err != nil {
		t.Fatal(err)
	}
	return handler, redeemer, sessions
}

func TestLoginAllowsInsecureCookieOnlyOnLoopbackHTTP(t *testing.T) {
	handler, _, _ := newTestHandlerAt(t, "http://localhost:8080")
	res := httptest.NewRecorder()
	handler.RedeemLogin(res, httptest.NewRequest(http.MethodGet, "http://localhost:8080/login?auth_token=signed", nil))
	cookies := res.Result().Cookies()
	if len(cookies) != 1 || cookies[0].Secure {
		t.Fatalf("localhost cookie = %+v, want non-Secure development cookie", cookies)
	}
}

func TestLoginRedeemsTokenAndSetsSecureCookie(t *testing.T) {
	handler, redeemer, _ := newTestHandler(t)
	req := httptest.NewRequest(http.MethodGet, "https://atlas.aurorass.art/login?auth_token=signed", nil)
	res := httptest.NewRecorder()
	handler.RedeemLogin(res, req)

	if res.Code != http.StatusSeeOther || res.Header().Get("Location") != "/admin" {
		t.Fatalf("login response = %d %q", res.Code, res.Header().Get("Location"))
	}
	if got := res.Header().Get("Referrer-Policy"); got != "no-referrer" {
		t.Fatalf("referrer policy = %q", got)
	}
	if redeemer.token != "signed" {
		t.Fatalf("redeemed token = %q", redeemer.token)
	}
	cookies := res.Result().Cookies()
	if len(cookies) != 1 {
		t.Fatalf("cookie count = %d", len(cookies))
	}
	cookie := cookies[0]
	if cookie.Name != SessionCookieName || !cookie.Secure || !cookie.HttpOnly || cookie.SameSite != http.SameSiteLaxMode {
		t.Fatalf("unsafe session cookie: %+v", cookie)
	}
}

func TestLoginFallsBackWithoutToken(t *testing.T) {
	handler, _, _ := newTestHandler(t)
	fallback := http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusTeapot) })
	res := httptest.NewRecorder()
	handler.Login(fallback).ServeHTTP(res, httptest.NewRequest(http.MethodGet, "https://atlas.aurorass.art/login", nil))
	if res.Code != http.StatusTeapot {
		t.Fatalf("fallback status = %d, want %d", res.Code, http.StatusTeapot)
	}
}

func TestRequireAdminUsesExactOriginAndRefererFallback(t *testing.T) {
	handler, _, sessions := newTestHandler(t)
	called := 0
	protected := handler.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		called++
		if session, ok := SessionFromContext(r.Context()); !ok || session.SubjectID != "42" {
			t.Fatal("session missing from protected request context")
		}
		w.WriteHeader(http.StatusNoContent)
	}))

	for _, tc := range []struct {
		name       string
		method     string
		origin     string
		referer    string
		csrf       bool
		wantStatus int
	}{
		{name: "exact origin", method: http.MethodPost, origin: "https://atlas.aurorass.art", csrf: true, wantStatus: http.StatusNoContent},
		{name: "referer fallback", method: http.MethodPost, referer: "https://atlas.aurorass.art/admin", csrf: true, wantStatus: http.StatusNoContent},
		{name: "foreign origin", method: http.MethodPost, origin: "https://attacker.example", csrf: true, wantStatus: http.StatusForbidden},
		{name: "different port", method: http.MethodPost, origin: "https://atlas.aurorass.art:443", csrf: true, wantStatus: http.StatusForbidden},
		{name: "missing csrf", method: http.MethodPost, origin: "https://atlas.aurorass.art", wantStatus: http.StatusForbidden},
		{name: "safe request", method: http.MethodGet, wantStatus: http.StatusNoContent},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(tc.method, "https://atlas.aurorass.art/api/v1/admin/imports", nil)
			req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessions.session.Token})
			if tc.origin != "" {
				req.Header.Set("Origin", tc.origin)
			}
			if tc.referer != "" {
				req.Header.Set("Referer", tc.referer)
			}
			if tc.csrf {
				req.Header.Set(CSRFHeaderName, CSRFTokenFor(handler.csrfKey, sessions.session.Token))
			}
			res := httptest.NewRecorder()
			protected.ServeHTTP(res, req)
			if res.Code != tc.wantStatus {
				t.Fatalf("status = %d, want %d", res.Code, tc.wantStatus)
			}
		})
	}
	if called != 3 {
		t.Fatalf("protected handler calls = %d, want 3", called)
	}
}

func TestRequireSessionRejectsNonOwnerSubject(t *testing.T) {
	handler, _, sessions := newTestHandler(t)
	sessions.session.SubjectID = "7"
	protected := handler.RequireSession(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {
		t.Fatal("non-owner session reached the handler")
	}))
	req := httptest.NewRequest(http.MethodGet, "https://atlas.aurorass.art/api/v1/admin/import-runs", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessions.session.Token})
	res := httptest.NewRecorder()
	protected.ServeHTTP(res, req)
	if res.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", res.Code, http.StatusUnauthorized)
	}
}

func TestLogoutRequiresCSRFAndRevokesSession(t *testing.T) {
	handler, _, sessions := newTestHandler(t)
	request := func(csrf string) *http.Request {
		req := httptest.NewRequest(http.MethodDelete, "https://atlas.aurorass.art/api/v1/auth/logout", nil)
		req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessions.session.Token})
		req.Header.Set("Origin", "https://atlas.aurorass.art")
		req.Header.Set(CSRFHeaderName, csrf)
		return req
	}

	bad := httptest.NewRecorder()
	handler.Logout(bad, request("bad"))
	if bad.Code != http.StatusForbidden || sessions.revoked != "" {
		t.Fatalf("bad logout status=%d revoked=%q", bad.Code, sessions.revoked)
	}

	good := httptest.NewRecorder()
	handler.Logout(good, request(CSRFTokenFor(handler.csrfKey, sessions.session.Token)))
	if good.Code != http.StatusNoContent || sessions.revoked != sessions.session.Token {
		t.Fatalf("good logout status=%d revoked=%q", good.Code, sessions.revoked)
	}
	cookies := good.Result().Cookies()
	if len(cookies) != 1 || cookies[0].MaxAge != -1 || !cookies[0].Secure || !cookies[0].HttpOnly {
		t.Fatalf("logout cookie = %+v", cookies)
	}
}

func TestSessionReturnsAnonymousForInvalidCookie(t *testing.T) {
	handler, _, sessions := newTestHandler(t)
	sessions.err = errors.New("database unavailable")
	res := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "https://atlas.aurorass.art/api/v1/auth/session", nil)
	req.AddCookie(&http.Cookie{Name: SessionCookieName, Value: sessions.session.Token})
	handler.Session(res, req)
	if res.Code != http.StatusOK || strings.TrimSpace(res.Body.String()) != `{"authenticated":false}` {
		t.Fatalf("session response = %d %q", res.Code, res.Body.String())
	}
}
