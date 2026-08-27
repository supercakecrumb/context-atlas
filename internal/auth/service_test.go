package auth

import (
	"context"
	"errors"
	"strings"
	"testing"

	authkit "github.com/supercakecrumb/msgr-authkit"
)

func TestNewAuthServiceUsesOneTimeTenMinuteLinks(t *testing.T) {
	issuer, err := authkit.NewInMemorySessionIssuer(SessionTTL)
	if err != nil {
		t.Fatal(err)
	}
	service, err := newAuthService(authkit.NewInMemoryIntentStore(), issuer, "https://atlas.aurorass.art", []byte(strings.Repeat("k", 32)))
	if err != nil {
		t.Fatal(err)
	}

	link, err := service.CreateLoginLink(context.Background(), authkit.CreateLoginLinkInput{
		Messenger: authkit.NewMessenger("telegram"),
		SubjectID: "42",
	})
	if err != nil {
		t.Fatal(err)
	}
	if link.Intent.RedemptionMode != authkit.IntentOneTime {
		t.Fatalf("redemption mode = %q, want one_time", link.Intent.RedemptionMode)
	}
	if got := link.ExpiresAt.Sub(link.Intent.CreatedAt); got != LoginLinkTTL {
		t.Fatalf("link TTL = %s, want %s", got, LoginLinkTTL)
	}
	if !strings.HasPrefix(link.LoginURL, "https://atlas.aurorass.art/login?auth_token=") {
		t.Fatalf("unexpected login URL %q", link.LoginURL)
	}

	session, err := service.RedeemLoginLink(context.Background(), authkit.RedeemLoginLinkInput{LinkToken: link.LinkToken})
	if err != nil {
		t.Fatal(err)
	}
	if session.SubjectID != "42" {
		t.Fatalf("session subject = %q, want 42", session.SubjectID)
	}
	_, err = service.RedeemLoginLink(context.Background(), authkit.RedeemLoginLinkInput{LinkToken: link.LinkToken})
	if !errors.Is(err, authkit.ErrIntentAlreadyRedeemed) {
		t.Fatalf("second redemption error = %v, want ErrIntentAlreadyRedeemed", err)
	}
}

func TestLoginCallbackURL(t *testing.T) {
	got, err := loginCallbackURL("https://atlas.aurorass.art/app/?ignored=true#fragment")
	if err != nil {
		t.Fatal(err)
	}
	if want := "https://atlas.aurorass.art/app/login"; got != want {
		t.Fatalf("login callback = %q, want %q", got, want)
	}
	if _, err := loginCallbackURL("mailto:owner@example.test"); err == nil {
		t.Fatal("expected non-HTTP public URL to fail")
	}
}

func TestPublicBaseURLPolicyOnlyAllowsLoopbackHTTP(t *testing.T) {
	for _, tc := range []struct {
		name       string
		baseURL    string
		wantOrigin string
		wantSecure bool
		wantErr    bool
	}{
		{name: "production HTTPS", baseURL: "https://ATLAS.aurorass.art:443", wantOrigin: "https://atlas.aurorass.art", wantSecure: true},
		{name: "localhost HTTP", baseURL: "http://localhost:8080", wantOrigin: "http://localhost:8080", wantSecure: false},
		{name: "IPv4 loopback HTTP", baseURL: "http://127.0.0.1", wantOrigin: "http://127.0.0.1", wantSecure: false},
		{name: "IPv6 loopback HTTP", baseURL: "http://[::1]:8080", wantOrigin: "http://[::1]:8080", wantSecure: false},
		{name: "remote HTTP", baseURL: "http://atlas.aurorass.art", wantErr: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			policy, err := parsePublicBaseURL(tc.baseURL)
			if tc.wantErr {
				if err == nil {
					t.Fatal("expected HTTP non-loopback URL to fail")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			if policy.origin != tc.wantOrigin || policy.secureCookie != tc.wantSecure {
				t.Fatalf("policy = %+v", policy)
			}
		})
	}
}

func TestHashTokenNeverReturnsPlaintext(t *testing.T) {
	got := hashToken("opaque-session-token")
	if len(got) != 32 {
		t.Fatalf("hash length = %d, want 32", len(got))
	}
	if string(got) == "opaque-session-token" {
		t.Fatal("session token was not hashed")
	}
}
