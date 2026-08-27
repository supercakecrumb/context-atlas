package main

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	authkit "github.com/supercakecrumb/msgr-authkit"

	"github.com/supercakecrumb/context-atlas/internal/auth"
)

func TestOpenAPICommandNeedsNoConfiguration(t *testing.T) {
	var output bytes.Buffer
	if err := run([]string{"openapi"}, &output); err != nil {
		t.Fatal(err)
	}
	var document map[string]any
	if err := json.Unmarshal(output.Bytes(), &document); err != nil {
		t.Fatal(err)
	}
	if document["openapi"] == "" || document["paths"] == nil {
		t.Fatalf("incomplete OpenAPI document: %#v", document)
	}
}

func TestHealthcheckUsesLoopbackHealthEndpoint(t *testing.T) {
	t.Setenv("PORT", "43123")
	if got := healthURL(); got != "http://127.0.0.1:43123/health" {
		t.Fatalf("health URL = %q", got)
	}
}

func TestHealthPortFallsBackToDefault(t *testing.T) {
	t.Setenv("PORT", "not-a-port")
	if got := healthPort(); got != 8080 {
		t.Fatalf("port = %d, want 8080", got)
	}
}

func TestAdminSessionAdapterUsesAuthenticatedContext(t *testing.T) {
	key := []byte("01234567890123456789012345678901")
	sessions := &testSessions{session: authkit.WebSession{
		SubjectID: "42", Token: "owner-token", ExpiresAt: time.Now().Add(time.Hour),
	}}
	owner, err := auth.NewHandler(testRedeemer{}, sessions, sessions, key, "http://localhost:8080", "42")
	if err != nil {
		t.Fatal(err)
	}
	adapter := adminSessionAdapter{sessions: sessions, csrfKey: key, ownerID: 42}
	request := httptest.NewRequest(http.MethodGet, "http://localhost:8080/api/v1/admin/session", nil)
	request.AddCookie(&http.Cookie{Name: auth.SessionCookieName, Value: "owner-token"})
	response := httptest.NewRecorder()
	owner.RequireAdmin(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		session, err := adapter.Session(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		if session.OwnerTelegramID != 42 || session.CSRFToken != auth.CSRFTokenFor(key, "owner-token") {
			t.Fatalf("unexpected admin session: %#v", session)
		}
		cookie, err := adapter.Logout(r.Context())
		if err != nil {
			t.Fatal(err)
		}
		http.SetCookie(w, &cookie)
	})).ServeHTTP(response, request)
	if response.Code != http.StatusOK || sessions.revoked != "owner-token" {
		t.Fatalf("status/revocation = %d/%q", response.Code, sessions.revoked)
	}
	if !strings.Contains(response.Header().Get("Set-Cookie"), "Max-Age=0") {
		t.Fatalf("logout cookie = %q", response.Header().Get("Set-Cookie"))
	}
}

type testRedeemer struct{}

func (testRedeemer) RedeemLoginLink(context.Context, authkit.RedeemLoginLinkInput) (authkit.WebSession, error) {
	return authkit.WebSession{}, nil
}

type testSessions struct {
	session authkit.WebSession
	revoked string
}

func (s *testSessions) Validate(context.Context, string) (authkit.WebSession, error) {
	return s.session, nil
}

func (s *testSessions) Revoke(_ context.Context, token string) error {
	s.revoked = token
	return nil
}
